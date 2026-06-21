package policy

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

type Engine struct {
	evaluator *RuleEvaluator
	ledger    *TransactionLedger
	events    chan PolicyEvent
	console   io.Writer
	cfg       Config

	logf   func(string, ...any)
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func NewEngine(cfg Config, logf func(string, ...any)) *Engine {
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 64
	}

	return &Engine{
		evaluator: NewRuleEvaluator(cfg),
		ledger:    NewTransactionLedger(cfg, logf),
		events:    make(chan PolicyEvent, cfg.BufferSize),
		console:   os.Stdout,
		cfg:       cfg,
		logf:      logf,
	}
}

func (e *Engine) SetConsoleOutput(w io.Writer) {
	if w != nil {
		e.console = w
	}
}

func (e *Engine) Events() <-chan PolicyEvent {
	return e.events
}

func (e *Engine) Evaluator() *RuleEvaluator {
	return e.evaluator
}

func (e *Engine) Ledger() *TransactionLedger {
	return e.ledger
}

func (e *Engine) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	e.ledger.StartSweeper(ctx)
	e.logf("policy: engine started (maxActions=%d velocityWindow=%ds cooldown=%ds threshold=%d)",
		e.cfg.MaxActionsPerPID, int(e.cfg.VelocityWindow.Seconds()),
		e.cfg.CooldownSeconds, e.cfg.PIDThreshold)
}

func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
	e.logf("policy: engine stopped")
}

func (e *Engine) EvaluateAndRegister(intent MitigationIntent) (authorized bool, txID string, event *PolicyEvent) {
	historicalCount := e.ledger.HistoricalCount(intent.TargetPID)
	authorized, matchedRules, rejectionRule := e.evaluator.Evaluate(intent, historicalCount)
	riskScore := e.evaluator.RiskScore(intent.ProcessName, historicalCount)

	matchedStr := make([]string, len(matchedRules))
	for i, r := range matchedRules {
		matchedStr[i] = string(r)
	}

	cooldown := e.ledger.CooldownSeconds()

	var status MitigationStatus
	var reason string
	if authorized {
		status = StatusAuthorizedPending
	} else {
		status = StatusRejected
		reason = string(rejectionRule)
	}

	record := e.ledger.Register(intent, status, matchedRules, riskScore, cooldown, reason)
	txID = record.TransactionID

	if !authorized {
		e.logf("policy: DENIED pid=%d name=%q rule=%s historical=%d",
			intent.TargetPID, intent.ProcessName, rejectionRule, historicalCount)
	}

	event = &PolicyEvent{
		LedgerTransactionID: record.TransactionID,
		Timestamp:           time.Now().Unix(),
		PolicyEvaluation: PolicyEvaluation{
			IsAuthorized:           authorized,
			EvaluatedRulesCount:    record.EvaluatedRules,
			MatchingPoliciesApplied: matchedStr,
			RejectionReason:        reason,
		},
		MitigationTarget: MitigationTargetMetadata{
			TargetPID:         intent.TargetPID,
			ContainerName:     intent.ContainerID,
			RiskScoreAssigned: riskScore,
		},
		LedgerState: LedgerStateSnapshot{
			CurrentActionStatus:                  status,
			HistoricalInterventionsOnTargetCount: record.HistoricalCount,
			EnforcedCooldownDurationSeconds:      cooldown,
		},
	}

	e.emitEvent(*event)

	return authorized, txID, event
}

func (e *Engine) ConfirmExecution(txID string) {
	e.ledger.UpdateStatus(txID, StatusExecuted)
}

func (e *Engine) Complete(txID string) {
	e.ledger.UpdateStatus(txID, StatusCompleted)
}

func (e *Engine) Reject(txID string, reason string) {
	e.ledger.UpdateStatus(txID, StatusRejected)
}

func (e *Engine) ActiveCount() int {
	return len(e.ledger.ListActive())
}

func (e *Engine) HistoricalForPID(pid int64) int {
	return e.ledger.HistoricalCount(pid)
}

func (e *Engine) TotalTrackedCount() int {
	return e.ledger.Count()
}

func (e *Engine) emitEvent(evt PolicyEvent) {
	select {
	case e.events <- evt:
	default:
		e.logf("policy: event channel full, dropping event")
	}

	if err := json.NewEncoder(e.console).Encode(evt); err != nil {
		e.logf("policy: encode console event: %v", err)
	}
}
