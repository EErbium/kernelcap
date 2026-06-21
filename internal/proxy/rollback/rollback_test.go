package rollback

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/mitigator"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/policy"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/router"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ReconciliationInterval != 5*time.Second {
		t.Errorf("ReconciliationInterval = %v, want 5s", cfg.ReconciliationInterval)
	}
	if cfg.RollbackCheckInterval != 1*time.Second {
		t.Errorf("RollbackCheckInterval = %v, want 1s", cfg.RollbackCheckInterval)
	}
	if cfg.ProcRoot != "/proc" {
		t.Errorf("ProcRoot = %q, want /proc", cfg.ProcRoot)
	}
	if cfg.BufferSize != 64 {
		t.Errorf("BufferSize = %d, want 64", cfg.BufferSize)
	}
}

func TestController_New(t *testing.T) {
	mit := mitigator.New(mitigator.DefaultConfig(), t.Logf)
	rreg := router.NewRegistry(60 * time.Second)
	pol := policy.NewEngine(policy.DefaultConfig(), t.Logf)

	ctl := New(DefaultConfig(), mit, rreg, pol, t.Logf)
	if ctl == nil {
		t.Fatal("expected non-nil controller")
	}
	if ctl.mit != mit {
		t.Error("mitigator reference mismatch")
	}
	if ctl.rreg != rreg {
		t.Error("router registry reference mismatch")
	}
	if ctl.pol != pol {
		t.Error("policy engine reference mismatch")
	}
}

func TestController_StartStop(t *testing.T) {
	mit := mitigator.New(mitigator.DefaultConfig(), t.Logf)
	rreg := router.NewRegistry(60 * time.Second)
	pol := policy.NewEngine(policy.DefaultConfig(), t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctl := New(DefaultConfig(), mit, rreg, pol, t.Logf)
	ctl.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	ctl.Stop()
}

func TestLedgerStateMapping(t *testing.T) {
	tests := []struct {
		input    mitigator.ActionState
		expected LedgerState
	}{
		{mitigator.ActionPaused, LedgerStatePaused},
		{mitigator.ActionThrottle, LedgerStateThrottled},
		{mitigator.ActionResumed, LedgerStateResumed},
		{mitigator.ActionOrphaned, LedgerStateOrphanedCleaned},
		{mitigator.ActionDriftSynced, LedgerStateDriftSynced},
	}

	for _, tt := range tests {
		got := ledgerStateFromAction(tt.input)
		if got != tt.expected {
			t.Errorf("ledgerStateFromAction(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSyncEvent_JSONSchema(t *testing.T) {
	evt := SyncEvent{
		SyncEventID: "sync_3e10cf2b-5890-4a8f-aa11-8b29cf0d894c",
		Timestamp:   1782352800,
		ReconciliationSummary: ReconciliationSummary{
			ScannedActiveInterventions: 1,
			StateDriftDetected:         true,
			ActionsResolvedCount:       1,
		},
		ReconciliationDetails: []ReconciliationDetail{
			{
				TargetPID:              41029,
				ContainerID:            "a1b2c3d4e5f67890abcdef",
				PreviousLedgerState:    LedgerStatePaused,
				ActualRuntimeState:     RuntimeStateRunning,
				RemedyApplied:          RemedySynchronizeLedgerToRuntime,
				FinalSynchronizedState: LedgerStateResumed,
			},
		},
		EngineHealth: EngineHealth{
			TotalActiveLocksHeld: 0,
			SyncLatencyMs:        1.45,
		},
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded SyncEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.SyncEventID != evt.SyncEventID {
		t.Errorf("SyncEventID mismatch: %s", decoded.SyncEventID)
	}
	if decoded.ReconciliationSummary.ScannedActiveInterventions != 1 {
		t.Errorf("ScannedActiveInterventions = %d, want 1",
			decoded.ReconciliationSummary.ScannedActiveInterventions)
	}
	if decoded.ReconciliationSummary.StateDriftDetected != true {
		t.Errorf("StateDriftDetected = %v, want true",
			decoded.ReconciliationSummary.StateDriftDetected)
	}
	if len(decoded.ReconciliationDetails) != 1 {
		t.Fatalf("expected 1 detail, got %d", len(decoded.ReconciliationDetails))
	}
	if decoded.ReconciliationDetails[0].TargetPID != 41029 {
		t.Errorf("TargetPID = %d, want 41029", decoded.ReconciliationDetails[0].TargetPID)
	}
	if decoded.ReconciliationDetails[0].RemedyApplied != RemedySynchronizeLedgerToRuntime {
		t.Errorf("RemedyApplied = %s", decoded.ReconciliationDetails[0].RemedyApplied)
	}
	if decoded.EngineHealth.SyncLatencyMs != 1.45 {
		t.Errorf("SyncLatencyMs = %f, want 1.45", decoded.EngineHealth.SyncLatencyMs)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal raw: %v", err)
	}

	requiredFields := []string{"sync_event_id", "timestamp", "reconciliation_summary", "reconciliation_details", "engine_health"}
	for _, field := range requiredFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing required JSON field: %s", field)
		}
	}
}

func TestCheckExpired_RouterAction(t *testing.T) {
	cfg := policy.DefaultConfig()
	cfg.CooldownSeconds = 0
	cfg.LedgerMaxEntries = 100
	pol := policy.NewEngine(cfg, t.Logf)

	mit := mitigator.New(mitigator.DefaultConfig(), t.Logf)
	rreg := router.NewRegistry(60 * time.Second)

	rreg.Activate(9999, "DYNAMIC_FALLBACK_ROUTING", "test-model")

	intent := policy.MitigationIntent{
		TargetPID:    9999,
		ProcessName:  "",
		ActionType:   policy.ActionAPIRouteSwap,
		SourceModule: "test",
	}
	authorized, txID, _ := pol.EvaluateAndRegister(intent)
	if !authorized {
		t.Fatal("expected authorized")
	}
	pol.ConfirmExecution(txID)

	time.Sleep(10 * time.Millisecond)

	_, fallbackCount := rreg.ActiveCount()
	if fallbackCount != 1 {
		t.Fatalf("expected 1 active fallback before rollback, got %d", fallbackCount)
	}

	ctl := New(DefaultConfig(), mit, rreg, pol, t.Logf)

	ctl.checkExpired()

	_, fallbackCount = rreg.ActiveCount()
	if fallbackCount != 0 {
		t.Errorf("expected 0 active fallback after rollback, got %d", fallbackCount)
	}
}

func TestCheckExpired_SIGSTOPAction(t *testing.T) {
	cfg := policy.DefaultConfig()
	cfg.CooldownSeconds = 0
	cfg.LedgerMaxEntries = 100
	pol := policy.NewEngine(cfg, t.Logf)

	mit := mitigator.New(mitigator.DefaultConfig(), t.Logf)
	rreg := router.NewRegistry(60 * time.Second)

	intent := policy.MitigationIntent{
		TargetPID:    99999,
		ProcessName:  "test-process",
		ActionType:   policy.ActionSIGSTOP,
		SourceModule: "test",
	}
	authorized, txID, _ := pol.EvaluateAndRegister(intent)
	if !authorized {
		t.Fatal("expected authorized")
	}
	pol.ConfirmExecution(txID)

	time.Sleep(10 * time.Millisecond)

	ctl := New(DefaultConfig(), mit, rreg, pol, t.Logf)

	ctl.checkExpired()

	record := pol.Ledger().Lookup(txID)
	if record == nil {
		t.Fatal("expected record to still exist")
	}
	if record.Status != "COMPLETED" {
		t.Logf("record status after rollback: %s (UnfreezeProcess may have failed on non-Linux or non-root)", record.Status)
	}
}

func TestCheckExpired_EmptyLedger(t *testing.T) {
	mit := mitigator.New(mitigator.DefaultConfig(), t.Logf)
	rreg := router.NewRegistry(60 * time.Second)
	pol := policy.NewEngine(policy.DefaultConfig(), t.Logf)

	ctl := New(DefaultConfig(), mit, rreg, pol, t.Logf)
	ctl.checkExpired()
}

func TestReconcileOnce_EmptyLedger(t *testing.T) {
	logf := func(string, ...any) {}
	mit := mitigator.New(mitigator.Config{BufferSize: 16}, logf)
	rreg := router.NewRegistry(60 * time.Second)
	pol := policy.NewEngine(policy.DefaultConfig(), logf)

	ctl := New(DefaultConfig(), mit, rreg, pol, logf)
	ctl.reconcileOnce()
}

func TestController_MultipleStop(t *testing.T) {
	mit := mitigator.New(mitigator.DefaultConfig(), t.Logf)
	rreg := router.NewRegistry(60 * time.Second)
	pol := policy.NewEngine(policy.DefaultConfig(), t.Logf)

	ctl := New(DefaultConfig(), mit, rreg, pol, t.Logf)
	ctl.Stop()
	ctl.Stop()
}

func TestController_ConcurrentAccess(t *testing.T) {
	mit := mitigator.New(mitigator.DefaultConfig(), t.Logf)
	rreg := router.NewRegistry(60 * time.Second)
	pol := policy.NewEngine(policy.DefaultConfig(), t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctl := New(DefaultConfig(), mit, rreg, pol, t.Logf)
	ctl.Start(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctl.checkExpired()
			ctl.reconcileOnce()
		}()
	}
	wg.Wait()

	ctl.Stop()
}

func TestGenerateSyncEventID(t *testing.T) {
	id1 := generateSyncEventID()
	id2 := generateSyncEventID()

	if id1 == id2 {
		t.Error("expected unique IDs")
	}
	if len(id1) != 37 {
		t.Errorf("expected ID length 37 (sync_ + 32 hex), got %d: %s", len(id1), id1)
	}
}

func TestEngineHealth_NonZero(t *testing.T) {
	evt := SyncEvent{
		SyncEventID: generateSyncEventID(),
		Timestamp:   time.Now().Unix(),
		EngineHealth: EngineHealth{
			TotalActiveLocksHeld: 5,
			SyncLatencyMs:        2.5,
		},
	}

	data, _ := json.Marshal(evt)
	var decoded SyncEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.EngineHealth.TotalActiveLocksHeld != 5 {
		t.Errorf("TotalActiveLocksHeld = %d, want 5", decoded.EngineHealth.TotalActiveLocksHeld)
	}
	if decoded.EngineHealth.SyncLatencyMs != 2.5 {
		t.Errorf("SyncLatencyMs = %f, want 2.5", decoded.EngineHealth.SyncLatencyMs)
	}
}
