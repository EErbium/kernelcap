package rollback

import (
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/mitigator"
)

func (ctl *Controller) reconcileOnce() {
	entries := ctl.mit.ActiveMitigations()
	if len(entries) == 0 {
		return
	}

	ctl.logf("rollback: reconciling %d active mitigations", len(entries))

	details := make([]ReconciliationDetail, 0, len(entries))
	driftDetected := false

	for _, entry := range entries {
		detail := ctl.reconcileEntry(entry)
		if detail == nil {
			continue
		}
		if detail.ActualRuntimeState != RuntimeStateStopped &&
			detail.FinalSynchronizedState != ledgerStateFromAction(entry.State) {
			driftDetected = true
		}
		details = append(details, *detail)

		switch detail.FinalSynchronizedState {
		case LedgerStateOrphanedCleaned:
			ctl.mit.UpdateMitigationState(entry.ReversibilityToken, mitigator.ActionOrphaned)
		case LedgerStateDriftSynced:
			ctl.mit.UpdateMitigationState(entry.ReversibilityToken, mitigator.ActionDriftSynced)
		}
	}

	if len(details) == 0 {
		return
	}

	ctl.emitEvent(SyncEvent{
		SyncEventID: generateSyncEventID(),
		Timestamp:   time.Now().Unix(),
		ReconciliationSummary: ReconciliationSummary{
			ScannedActiveInterventions: len(entries),
			StateDriftDetected:         driftDetected,
			ActionsResolvedCount:       len(details),
		},
		ReconciliationDetails: details,
		EngineHealth: EngineHealth{
			TotalActiveLocksHeld: 0,
			SyncLatencyMs:        0,
		},
	})
}

func (ctl *Controller) reconcileEntry(entry mitigator.LedgerEntry) *ReconciliationDetail {
	runtime := probeProcessState(entry.PID, ctl.cfg.ProcRoot)

	detail := &ReconciliationDetail{
		TargetPID:           int64(entry.PID),
		ContainerID:         entry.ContainerID,
		PreviousLedgerState: ledgerStateFromAction(entry.State),
		ActualRuntimeState:  runtime,
	}

	switch runtime {
	case RuntimeStateNotFound:
		detail.RemedyApplied = RemedySynchronizeLedgerToRuntime
		detail.FinalSynchronizedState = LedgerStateOrphanedCleaned
		ctl.logf("rollback: orphaned pid=%d (process gone), marking ORPHANED_CLEANED", entry.PID)

	case RuntimeStateRunning:
		detail.RemedyApplied = RemedySynchronizeLedgerToRuntime
		detail.FinalSynchronizedState = LedgerStateDriftSynced
		ctl.logf("rollback: drift pid=%d (running but ledger says %s), marking DRIFT_SYNCED",
			entry.PID, entry.State)

	case RuntimeStateStopped, RuntimeStatePaused:
		detail.RemedyApplied = RemedySynchronizeLedgerToRuntime
		detail.FinalSynchronizedState = ledgerStateFromAction(entry.State)
		return nil

	default:
		detail.RemedyApplied = RemedySynchronizeLedgerToRuntime
		detail.FinalSynchronizedState = LedgerStateDriftSynced
	}

	return detail
}

func ledgerStateFromAction(a mitigator.ActionState) LedgerState {
	switch a {
	case mitigator.ActionPaused:
		return LedgerStatePaused
	case mitigator.ActionThrottle:
		return LedgerStateThrottled
	case mitigator.ActionResumed:
		return LedgerStateResumed
	case mitigator.ActionOrphaned:
		return LedgerStateOrphanedCleaned
	case mitigator.ActionDriftSynced:
		return LedgerStateDriftSynced
	default:
		return LedgerStatePaused
	}
}
