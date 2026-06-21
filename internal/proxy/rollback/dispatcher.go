package rollback

import (
	"fmt"
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/mitigator"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/policy"
)

func (ctl *Controller) checkExpired() {
	records := ctl.pol.Ledger().ListExecutedExpired()
	if len(records) == 0 {
		return
	}

	ctl.logf("rollback: found %d expired executed records", len(records))

	details := make([]ReconciliationDetail, 0, len(records))

	for _, rec := range records {
		detail, err := ctl.dispatchRollback(rec)
		if err != nil {
			ctl.logf("rollback: dispatch error tx=%s pid=%d err=%v",
				rec.TransactionID, rec.TargetPID, err)
		} else {
			details = append(details, *detail)
		}
	}

	if len(details) == 0 {
		return
	}

	ctl.emitEvent(SyncEvent{
		SyncEventID: generateSyncEventID(),
		Timestamp:   time.Now().Unix(),
		ReconciliationSummary: ReconciliationSummary{
			ScannedActiveInterventions: len(records),
			StateDriftDetected:         false,
			ActionsResolvedCount:       len(details),
		},
		ReconciliationDetails: details,
		EngineHealth: EngineHealth{
			TotalActiveLocksHeld: 0,
			SyncLatencyMs:        0,
		},
	})
}

func (ctl *Controller) dispatchRollback(rec policy.MitigationRecord) (*ReconciliationDetail, error) {
	start := time.Now()
	var prevState LedgerState
	var runtimeState RuntimeState
	var remedy RemedyType

	switch rec.ActionType {
	case policy.ActionSIGSTOP:
		prevState = LedgerStatePaused
		if err := mitigator.UnfreezeProcess(int(rec.TargetPID)); err != nil {
			return nil, fmt.Errorf("unfreeze pid %d: %w", rec.TargetPID, err)
		}
		runtimeState = RuntimeStateRunning
		remedy = RemedyRollbackExecuted

	case policy.ActionContainerPause, policy.ActionCgroupFreeze:
		prevState = LedgerStatePaused
		if err := ctl.mit.RollbackPID(int(rec.TargetPID), rec.ContainerID); err != nil {
			return nil, fmt.Errorf("rollback container pid %d: %w", rec.TargetPID, err)
		}
		runtimeState = RuntimeStateRunning
		remedy = RemedyRollbackExecuted

	case policy.ActionAPIRouteSwap, policy.ActionTokenChop:
		prevState = LedgerStateThrottled
		ctl.rreg.Deactivate(rec.TargetPID)
		runtimeState = RuntimeStateRunning
		remedy = RemedyRollbackExecuted

	default:
		return nil, fmt.Errorf("unknown action type: %s", rec.ActionType)
	}

	ctl.pol.Complete(rec.TransactionID)
	elapsed := time.Since(start).Seconds() * 1000

	detail := &ReconciliationDetail{
		TargetPID:              rec.TargetPID,
		ContainerID:            rec.ContainerID,
		PreviousLedgerState:    prevState,
		ActualRuntimeState:     runtimeState,
		RemedyApplied:          remedy,
		FinalSynchronizedState: LedgerStateResumed,
	}

	ctl.logf("rollback: executed tx=%s pid=%d action=%s latency=%.2fms",
		rec.TransactionID, rec.TargetPID, rec.ActionType, elapsed)

	return detail, nil
}
