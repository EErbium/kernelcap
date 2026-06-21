package rollback

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type RuntimeState string

const (
	RuntimeStateRunning    RuntimeState = "RUNNING"
	RuntimeStateStopped    RuntimeState = "STOPPED"
	RuntimeStateZombie     RuntimeState = "ZOMBIE"
	RuntimeStateNotFound   RuntimeState = "NOT_FOUND"
	RuntimeStatePaused     RuntimeState = "PAUSED"
)

type LedgerState string

const (
	LedgerStatePaused            LedgerState = "PAUSED"
	LedgerStateThrottled         LedgerState = "THROTTLED"
	LedgerStateResumed           LedgerState = "RESUMED"
	LedgerStateOrphanedCleaned   LedgerState = "ORPHANED_CLEANED"
	LedgerStateDriftSynced       LedgerState = "DRIFT_SYNCED"
)

type RemedyType string

const (
	RemedyRollbackExecuted         RemedyType = "ROLLBACK_EXECUTED"
	RemedySynchronizeLedgerToRuntime RemedyType = "SYNCHRONIZE_LEDGER_TO_RUNTIME"
)

type Config struct {
	ReconciliationInterval time.Duration
	RollbackCheckInterval  time.Duration
	BufferSize             int
	ProcRoot               string
}

func DefaultConfig() Config {
	return Config{
		ReconciliationInterval: 5 * time.Second,
		RollbackCheckInterval:  1 * time.Second,
		BufferSize:             64,
		ProcRoot:               "/proc",
	}
}

type SyncEvent struct {
	SyncEventID            string                 `json:"sync_event_id"`
	Timestamp              int64                  `json:"timestamp"`
	ReconciliationSummary  ReconciliationSummary  `json:"reconciliation_summary"`
	ReconciliationDetails  []ReconciliationDetail `json:"reconciliation_details"`
	EngineHealth           EngineHealth           `json:"engine_health"`
}

type ReconciliationSummary struct {
	ScannedActiveInterventions int  `json:"scanned_active_interventions"`
	StateDriftDetected         bool `json:"state_drift_detected"`
	ActionsResolvedCount       int  `json:"actions_resolved_count"`
}

type ReconciliationDetail struct {
	TargetPID              int64       `json:"target_pid"`
	ContainerID            string      `json:"container_id"`
	PreviousLedgerState    LedgerState `json:"previous_ledger_state"`
	ActualRuntimeState     RuntimeState `json:"actual_runtime_state"`
	RemedyApplied          RemedyType  `json:"remedy_applied"`
	FinalSynchronizedState LedgerState `json:"final_synchronized_state"`
}

type EngineHealth struct {
	TotalActiveLocksHeld int     `json:"total_active_locks_held"`
	SyncLatencyMs        float64 `json:"sync_latency_ms"`
}

func generateSyncEventID() string {
	var buf [16]byte
	rand.Read(buf[:])
	return "sync_" + hex.EncodeToString(buf[:])
}
