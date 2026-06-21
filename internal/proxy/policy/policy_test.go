package policy

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestEvaluate_SystemdRejected(t *testing.T) {
	re := NewRuleEvaluator(DefaultConfig())

	intent := MitigationIntent{TargetPID: 1, ProcessName: "systemd", ActionType: ActionSIGSTOP}
	authorized, _, rule := re.Evaluate(intent, 0)
	if authorized {
		t.Error("expected false for systemd")
	}
	if rule != RuleSystemBlacklist {
		t.Errorf("expected SYSTEM_BLACKLIST_CHECK, got %s", rule)
	}
}

func TestEvaluate_LowPIDRejected(t *testing.T) {
	re := NewRuleEvaluator(DefaultConfig())

	intent := MitigationIntent{TargetPID: 50, ProcessName: "python3", ActionType: ActionSIGSTOP}
	authorized, _, rule := re.Evaluate(intent, 0)
	if authorized {
		t.Error("expected false for PID < threshold")
	}
	if rule != RuleSystemBlacklist {
		t.Errorf("expected SYSTEM_BLACKLIST_CHECK, got %s", rule)
	}
}

func TestEvaluate_DockerdRejected(t *testing.T) {
	re := NewRuleEvaluator(DefaultConfig())

	intent := MitigationIntent{TargetPID: 1500, ProcessName: "dockerd", ActionType: ActionContainerPause}
	authorized, _, rule := re.Evaluate(intent, 0)
	if authorized {
		t.Error("expected false for dockerd")
	}
	if rule != RuleSystemBlacklist {
		t.Errorf("expected SYSTEM_BLACKLIST_CHECK, got %s", rule)
	}
}

func TestEvaluate_WhitelistBypass(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WhitelistNames = []string{"critical-pod"}
	re := NewRuleEvaluator(cfg)

	intent := MitigationIntent{TargetPID: 5000, ProcessName: "critical-pod", ActionType: ActionTokenChop}
	authorized, _, rule := re.Evaluate(intent, 0)
	if authorized {
		t.Error("expected false for whitelisted process")
	}
	if rule != RuleUserWhitelist {
		t.Errorf("expected USER_WHITELIST_CHECK, got %s", rule)
	}
}

func TestEvaluate_WhitelistContainerID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WhitelistIDs = []string{"prod-db-01"}
	re := NewRuleEvaluator(cfg)

	intent := MitigationIntent{TargetPID: 5000, ProcessName: "mysql", ContainerID: "prod-db-01", ActionType: ActionContainerPause}
	authorized, _, rule := re.Evaluate(intent, 0)
	if authorized {
		t.Error("expected false for whitelisted container")
	}
	if rule != RuleUserWhitelist {
		t.Errorf("expected USER_WHITELIST_CHECK, got %s", rule)
	}
}

func TestEvaluate_VelocityCapExceeded(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxActionsPerPID = 3
	re := NewRuleEvaluator(cfg)

	intent := MitigationIntent{TargetPID: 5000, ProcessName: "python3", ActionType: ActionSIGSTOP}
	authorized, _, rule := re.Evaluate(intent, 5)
	if authorized {
		t.Error("expected false when velocity cap exceeded")
	}
	if rule != RuleVelocityCap {
		t.Errorf("expected VELOCITY_CAP_CHECK, got %s", rule)
	}
}

func TestEvaluate_PIDBoundary(t *testing.T) {
	re := NewRuleEvaluator(DefaultConfig())

	intent := MitigationIntent{TargetPID: -1, ProcessName: "test", ActionType: ActionSIGSTOP}
	authorized, _, rule := re.Evaluate(intent, 0)
	if authorized {
		t.Error("expected false for negative PID")
	}
	if rule != RulePIDBoundary {
		t.Errorf("expected PID_BOUNDARY_CHECK, got %s", rule)
	}
}

func TestEvaluate_UnknownProcessAllowed(t *testing.T) {
	re := NewRuleEvaluator(DefaultConfig())

	intent := MitigationIntent{TargetPID: 5000, ProcessName: "python3", ActionType: ActionSIGSTOP}
	authorized, rules, rule := re.Evaluate(intent, 0)
	if !authorized {
		t.Errorf("expected true for unknown process, got false (rule=%s)", rule)
	}
	if len(rules) < 2 {
		t.Errorf("expected at least 2 matched rules, got %d", len(rules))
	}
}

func TestEvaluateAndRegister_Success(t *testing.T) {
	e := NewEngine(DefaultConfig(), func(string, ...any) {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.Start(ctx)
	defer e.Stop()

	intent := MitigationIntent{TargetPID: 5000, ProcessName: "python3", ActionType: ActionSIGSTOP, AnomalyType: "IDLE_GPU_HOG"}
	authorized, txID, event := e.EvaluateAndRegister(intent)

	if !authorized {
		t.Errorf("expected true, got false: %s", event.PolicyEvaluation.RejectionReason)
	}
	if txID == "" {
		t.Error("expected non-empty txID")
	}
	if event.LedgerTransactionID != txID {
		t.Error("event txID mismatch")
	}
	if event.PolicyEvaluation.IsAuthorized != true {
		t.Error("event should indicate authorized")
	}
	if event.LedgerState.CurrentActionStatus != StatusAuthorizedPending {
		t.Errorf("status = %s, want AUTHORIZED_PENDING", event.LedgerState.CurrentActionStatus)
	}
}

func TestEvaluateAndRegister_Denied(t *testing.T) {
	e := NewEngine(DefaultConfig(), func(string, ...any) {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.Start(ctx)
	defer e.Stop()

	intent := MitigationIntent{TargetPID: 1, ProcessName: "systemd", ActionType: ActionSIGSTOP}
	authorized, txID, event := e.EvaluateAndRegister(intent)

	if authorized {
		t.Error("expected false for systemd")
	}
	if txID == "" {
		t.Error("expected non-empty txID even for rejection")
	}
	if event.PolicyEvaluation.IsAuthorized != false {
		t.Error("event should indicate not authorized")
	}
	if event.LedgerState.CurrentActionStatus != StatusRejected {
		t.Errorf("status = %s, want REJECTED", event.LedgerState.CurrentActionStatus)
	}
}

func TestEvaluateAndRegister_Lifecycle(t *testing.T) {
	e := NewEngine(DefaultConfig(), func(string, ...any) {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.Start(ctx)
	defer e.Stop()

	intent := MitigationIntent{TargetPID: 5000, ProcessName: "test", ActionType: ActionSIGSTOP}
	_, txID, _ := e.EvaluateAndRegister(intent)

	if e.ActiveCount() != 1 {
		t.Errorf("ActiveCount = %d, want 1", e.ActiveCount())
	}

	e.ConfirmExecution(txID)
	rec := e.Ledger().Lookup(txID)
	if rec.Status != StatusExecuted {
		t.Errorf("status = %s, want EXECUTED", rec.Status)
	}

	e.Complete(txID)
	rec = e.Ledger().Lookup(txID)
	if rec.Status != StatusCompleted {
		t.Errorf("status = %s, want COMPLETED", rec.Status)
	}
}

func TestLedger_HistoricalCount(t *testing.T) {
	tl := NewTransactionLedger(DefaultConfig(), func(string, ...any) {})

	if tl.HistoricalCount(123) != 0 {
		t.Error("expected 0 for unknown PID")
	}

	for i := 0; i < 3; i++ {
		tl.Register(
			MitigationIntent{TargetPID: 123, ActionType: ActionSIGSTOP},
			StatusAuthorizedPending,
			[]RuleName{RuleSystemBlacklist},
			0.1, 3600, "",
		)
	}

	if tl.HistoricalCount(123) != 3 {
		t.Errorf("HistoricalCount = %d, want 3", tl.HistoricalCount(123))
	}
}

func TestLedger_RegisterAndLookup(t *testing.T) {
	tl := NewTransactionLedger(DefaultConfig(), func(string, ...any) {})

	rec := tl.Register(
		MitigationIntent{TargetPID: 42, ProcessName: "python3", ActionType: ActionAPIRouteSwap, AnomalyType: "HOST_MEMORY_LEAK"},
		StatusAuthorizedPending,
		[]RuleName{RuleSystemBlacklist, RulePIDBoundary},
		0.25, 60, "",
	)

	if rec.TransactionID == "" {
		t.Fatal("expected non-empty txID")
	}
	if !hasPrefix(rec.TransactionID, "tx_") {
		t.Errorf("txID should start with tx_, got %s", rec.TransactionID)
	}

	found := tl.Lookup(rec.TransactionID)
	if found == nil {
		t.Fatal("expected to find record")
	}
	if found.TargetPID != 42 {
		t.Errorf("PID = %d, want 42", found.TargetPID)
	}
	if found.Status != StatusAuthorizedPending {
		t.Errorf("status = %s", found.Status)
	}
	if found.EvaluatedRules != 2 {
		t.Errorf("EvaluatedRules = %d, want 2", found.EvaluatedRules)
	}
}

func TestLedger_UpdateStatus(t *testing.T) {
	tl := NewTransactionLedger(DefaultConfig(), func(string, ...any) {})

	rec := tl.Register(MitigationIntent{TargetPID: 100, ActionType: ActionSIGSTOP}, StatusAuthorizedPending, nil, 0, 60, "")
	if !tl.UpdateStatus(rec.TransactionID, StatusExecuted) {
		t.Fatal("UpdateStatus returned false")
	}
	if tl.Lookup(rec.TransactionID).Status != StatusExecuted {
		t.Error("status should be EXECUTED")
	}

	if tl.UpdateStatus("nonexistent", StatusCompleted) {
		t.Error("UpdateStatus on nonexistent should return false")
	}
}

func TestLedger_SweeperExpiry(t *testing.T) {
	tl := NewTransactionLedger(Config{LedgerMaxEntries: 1000, SweepInterval: 10 * time.Millisecond, CooldownSeconds: 0}, func(string, ...any) {})

	tl.Register(MitigationIntent{TargetPID: 999, ActionType: ActionSIGSTOP}, StatusAuthorizedPending, nil, 0, 0, "")

	ctx, cancel := context.WithCancel(context.Background())
	tl.StartSweeper(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()

	if tl.Count() != 0 {
		t.Errorf("expected 0 after sweep, got %d", tl.Count())
	}
}

func TestLedger_SweeperMaxEntries(t *testing.T) {
	tl := NewTransactionLedger(Config{
		LedgerMaxEntries: 2,
		SweepInterval:    24 * time.Hour,
		CooldownSeconds:  3600,
	}, func(string, ...any) {})

	tl.Register(MitigationIntent{TargetPID: 1, ActionType: ActionSIGSTOP}, StatusAuthorizedPending, nil, 0, 3600, "")
	tl.Register(MitigationIntent{TargetPID: 2, ActionType: ActionSIGSTOP}, StatusAuthorizedPending, nil, 0, 3600, "")
	tl.Register(MitigationIntent{TargetPID: 3, ActionType: ActionSIGSTOP}, StatusAuthorizedPending, nil, 0, 3600, "")

	tl.sweep()

	if tl.Count() > 2 {
		t.Errorf("expected max 2 entries after sweep, got %d", tl.Count())
	}
}

func TestLedger_ConcurrentAccess(t *testing.T) {
	tl := NewTransactionLedger(DefaultConfig(), func(string, ...any) {})
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pid := int64(1000 + i)
			rec := tl.Register(
				MitigationIntent{TargetPID: pid, ActionType: ActionSIGSTOP},
				StatusAuthorizedPending,
				[]RuleName{RuleSystemBlacklist},
				0.1, 3600, "",
			)
			tl.Lookup(rec.TransactionID)
			tl.UpdateStatus(rec.TransactionID, StatusExecuted)
			tl.HistoricalCount(pid)
			tl.ListActive()
		}(i)
	}
	wg.Wait()

	if tl.Count() != 50 {
		t.Errorf("Count = %d, want 50", tl.Count())
	}
}

func TestRiskScore(t *testing.T) {
	re := NewRuleEvaluator(DefaultConfig())

	tests := []struct {
		name           string
		processName    string
		historicalCount int
	}{
		{"unknown zero", "python3", 0},
		{"unknown mid", "python3", 3},
		{"unknown high", "python3", 10},
		{"blacklisted", "systemd", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := re.RiskScore(tt.processName, tt.historicalCount)
			if score < 0 || score > 1.0 {
				t.Errorf("risk score out of range: %f", score)
			}
		})
	}

	scoreBlacklisted := re.RiskScore("systemd", 0)
	scoreUnknown := re.RiskScore("python3", 0)
	if scoreBlacklisted <= scoreUnknown {
		t.Errorf("blacklisted score (%f) should be > unknown score (%f)", scoreBlacklisted, scoreUnknown)
	}
}

func TestPolicyEvent_JSONSchema(t *testing.T) {
	evt := PolicyEvent{
		LedgerTransactionID: "tx_fa92b10c-3490-4e2b-bb12-9a3e10cf4567",
		Timestamp:           1782352800,
		PolicyEvaluation: PolicyEvaluation{
			IsAuthorized:           true,
			EvaluatedRulesCount:    4,
			MatchingPoliciesApplied: []string{"SYSTEM_BLACKLIST_CHECK", "VELOCITY_CAP_CHECK"},
		},
		MitigationTarget: MitigationTargetMetadata{
			TargetPID:         41029,
			ContainerName:     "production-llm-agent-pod-04",
			RiskScoreAssigned: 0.12,
		},
		LedgerState: LedgerStateSnapshot{
			CurrentActionStatus:                  StatusAuthorizedPending,
			HistoricalInterventionsOnTargetCount: 2,
			EnforcedCooldownDurationSeconds:      60,
		},
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded PolicyEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.LedgerTransactionID != evt.LedgerTransactionID {
		t.Error("txID mismatch")
	}
	if decoded.PolicyEvaluation.IsAuthorized != true {
		t.Error("authorized mismatch")
	}
	if decoded.PolicyEvaluation.EvaluatedRulesCount != 4 {
		t.Error("rule count mismatch")
	}
	if decoded.MitigationTarget.TargetPID != 41029 {
		t.Error("PID mismatch")
	}
	if decoded.LedgerState.HistoricalInterventionsOnTargetCount != 2 {
		t.Error("historical count mismatch")
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	required := []string{"ledger_transaction_id", "timestamp", "policy_evaluation", "mitigation_target_metadata", "ledger_state_snapshot"}
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing required JSON field: %s", field)
		}
	}
}

func TestEngine_VelocityCapAcrossLifecycle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxActionsPerPID = 2
	e := NewEngine(cfg, func(string, ...any) {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.Start(ctx)
	defer e.Stop()

	intent := MitigationIntent{TargetPID: 7777, ProcessName: "worker", ActionType: ActionSIGSTOP}

	ok1, _, _ := e.EvaluateAndRegister(intent)
	if !ok1 {
		t.Error("first should be authorized")
	}

	ok2, _, _ := e.EvaluateAndRegister(intent)
	if !ok2 {
		t.Error("second should be authorized")
	}

	ok3, _, event := e.EvaluateAndRegister(intent)
	if ok3 {
		t.Error("third should be denied by velocity cap")
	}
	if event.PolicyEvaluation.RejectionReason != string(RuleVelocityCap) {
		t.Errorf("expected VELOCITY_CAP_CHECK, got %s", event.PolicyEvaluation.RejectionReason)
	}
}

func TestEngine_WhitelistUpdate(t *testing.T) {
	re := NewRuleEvaluator(DefaultConfig())

	intent := MitigationIntent{TargetPID: 5000, ProcessName: "my-service", ActionType: ActionTokenChop}
	authorized, _, _ := re.Evaluate(intent, 0)
	if !authorized {
		t.Error("expected true before whitelist update")
	}

	re.UpdateWhitelist([]string{"my-service"}, nil)

	authorized, _, _ = re.Evaluate(intent, 0)
	if authorized {
		t.Error("expected false after whitelist update")
	}
}

func hasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}
