package mitigator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/alerter"
	"github.com/anomalyco/ai-compute-profiler/pkg/policy"
)

type Mitigator struct {
	cfg    Config
	ledger *Ledger
	policy *PolicyEvaluator
	cm     *containerManager

	events chan MitigationEvent
	console io.Writer

	logf   func(string, ...any)
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func New(cfg Config, logf func(string, ...any)) *Mitigator {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 64
	}

	var pe *PolicyEvaluator
	if cfg.Policy != nil {
		pe = NewPolicyEvaluator(cfg.Policy)
	} else {
		pe = NewPolicyEvaluator(nil)
	}

	m := &Mitigator{
		cfg:     cfg,
		ledger:  NewLedger(),
		policy:  pe,
		events:  make(chan MitigationEvent, cfg.BufferSize),
		console: os.Stdout,
		logf:    logf,
	}
	m.cm = newContainerManager(
		cfg.DockerSocketPath,
		cfg.DockerSocketPath != "",
		cfg.DockerTimeout,
		cfg.CgroupRoot,
		cfg.ProcRoot,
	)
	return m
}

func (m *Mitigator) SetConsoleOutput(w io.Writer) {
	if w != nil {
		m.console = w
	}
}

func (m *Mitigator) Events() <-chan MitigationEvent {
	return m.events
}

func (m *Mitigator) Start(ctx context.Context, alertCh <-chan alerter.ConsolidatedAlert) {
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	m.wg.Add(1)
	go m.alertListener(ctx, alertCh)
	m.logf("mitigator: started (docker=%v cgroup=%s)", m.cfg.DockerSocketPath != "", m.cfg.CgroupRoot)
}

func (m *Mitigator) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

func (m *Mitigator) alertListener(ctx context.Context, alertCh <-chan alerter.ConsolidatedAlert) {
	defer m.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case alert, ok := <-alertCh:
			if !ok {
				return
			}
			m.mitigateFromAlert(ctx, alert)
	}
}

func (m *Mitigator) mitigateFromAlert(ctx context.Context, alert alerter.ConsolidatedAlert) {
	pid := int(alert.Payload.TargetPID)
	anomalyType := alert.Payload.AnomalyType
	processName := m.resolveProcessName(pid)
	containerID := m.resolveContainerID(pid)

	target := TargetIdentifier{
		PID:         pid,
		ContainerID: containerID,
		ProcessName: processName,
	}

	if m.cfg.Policy != nil {
		actionType := policy.ActionSIGSTOP
		if containerID != "" {
			actionType = policy.ActionContainerPause
		}

		authorized, txID, polEvt := m.cfg.Policy.EvaluateAndRegister(policy.MitigationIntent{
			TargetPID:    int64(pid),
			ContainerID:  containerID,
			ProcessName:  processName,
			ActionType:   actionType,
			AnomalyType:  anomalyType,
			SourceModule: "mitigator",
		})

		if !authorized {
			isWhitelisted := m.policy.IsWhitelisted(pid, containerID, processName)
			m.emitEvent(ctx, MitigationEvent{
				MitigationEventID: generateEventID(),
				Timestamp:         time.Now().Unix(),
				Target:            target,
				Action: ActionExecuted{
					Mechanism:           "",
					SignalSent:          "",
					Status:              ActionStatusSkipped,
					ExecutionDurationMs: 0,
				},
				Policy: PolicyContext{
					IsWhitelistedEntity: isWhitelisted,
					ReversibilityToken:  polEvt.LedgerTransactionID,
				},
			})
			m.logf("mitigator: policy rejected pid=%d name=%s reason=%s", pid, processName, polEvt.PolicyEvaluation.RejectionReason)
			return
		}

		start := time.Now()
		var mech MechanismType
		var signalSent string
		var actionErr error

		if containerID != "" {
			mech = MechanismContainerPause
			signalSent = "SIGSTOP"
			actionErr = m.cm.Pause(ctx, containerID)
			if actionErr != nil {
				mech = MechanismSignalStop
				actionErr = FreezeProcess(pid)
			}
		} else {
			mech = MechanismSignalStop
			signalSent = "SIGSTOP"
			actionErr = FreezeProcess(pid)
		}

		elapsed := time.Since(start).Seconds() * 1000
		status := ActionStatusSuccess
		if actionErr != nil {
			status = ActionStatusFailed
			m.logf("mitigator: FAILED pid=%d mech=%s err=%v", pid, mech, actionErr)
		}

		if status == ActionStatusSuccess {
			m.cfg.Policy.ConfirmExecution(txID)
		} else {
			m.cfg.Policy.Reject(txID, actionErr.Error())
		}

		var revToken string
		if status == ActionStatusSuccess {
			revToken = m.ledger.Record(LedgerEntry{
				PID:          pid,
				ContainerID:  containerID,
				ProcessName:  processName,
				AnomalyType:  anomalyType,
				State:        ActionPaused,
				Mechanism:    mech,
			})
		}

		evt := MitigationEvent{
			MitigationEventID: generateEventID(),
			Timestamp:         time.Now().Unix(),
			Target:            target,
			Action: ActionExecuted{
				Mechanism:           mech,
				SignalSent:          signalSent,
				Status:              status,
				ExecutionDurationMs: elapsed,
			},
			Policy: PolicyContext{
				IsWhitelistedEntity: false,
				ReversibilityToken:  revToken,
			},
		}
		m.emitEvent(ctx, evt)
		return
	}

	start := time.Now()
	var mech MechanismType
	var signalSent string
	var actionErr error

	if containerID != "" {
		mech = MechanismContainerPause
		signalSent = "SIGSTOP"
		actionErr = m.cm.Pause(ctx, containerID)
		if actionErr != nil {
			mech = MechanismSignalStop
			actionErr = FreezeProcess(pid)
		}
	} else {
		mech = MechanismSignalStop
		signalSent = "SIGSTOP"
		actionErr = FreezeProcess(pid)
	}

	elapsed := time.Since(start).Seconds() * 1000

	status := ActionStatusSuccess
	if actionErr != nil {
		status = ActionStatusFailed
		m.logf("mitigator: FAILED pid=%d mech=%s err=%v", pid, mech, actionErr)
	}

	var revToken string
	if status == ActionStatusSuccess {
		revToken = m.ledger.Record(LedgerEntry{
			PID:          pid,
			ContainerID:  containerID,
			ProcessName:  processName,
			AnomalyType:  anomalyType,
			State:        ActionPaused,
			Mechanism:    mech,
		})
	}

	evt := MitigationEvent{
		MitigationEventID: generateEventID(),
		Timestamp:         time.Now().Unix(),
		Target:            target,
		Action: ActionExecuted{
			Mechanism:           mech,
			SignalSent:          signalSent,
			Status:              status,
			ExecutionDurationMs: elapsed,
		},
		Policy: PolicyContext{
			IsWhitelistedEntity: false,
			ReversibilityToken:  revToken,
		},
	}
	m.emitEvent(ctx, evt)
}

func (m *Mitigator) Resume(token string) (*MitigationEvent, error) {
	entry := m.ledger.ResolveByToken(token)
	if entry == nil {
		return nil, fmt.Errorf("resume: token %s not found", token)
	}

	start := time.Now()
	var mech MechanismType
	var actionErr error

	switch entry.Mechanism {
	case MechanismContainerPause:
		mech = MechanismContainerPause
		ctx, cancel := context.WithTimeout(context.Background(), m.cfg.DockerTimeout)
		defer cancel()
		actionErr = m.cm.Unpause(ctx, entry.ContainerID)
		if actionErr != nil {
			return nil, fmt.Errorf("resume container %s: %w", entry.ContainerID, actionErr)
		}
	case MechanismSignalStop, MechanismCgroupFreeze:
		mech = MechanismSignalStop
		actionErr = UnfreezeProcess(entry.PID)
		if actionErr != nil {
			return nil, fmt.Errorf("resume pid %d: %w", entry.PID, actionErr)
		}
	}

	elapsed := time.Since(start).Seconds() * 1000
	m.ledger.UpdateState(token, ActionResumed)

	if m.cfg.Policy != nil {
		m.cfg.Policy.Complete(token)
	}

	evt := MitigationEvent{
		MitigationEventID: generateEventID(),
		Timestamp:         time.Now().Unix(),
		Target: TargetIdentifier{
			PID:         entry.PID,
			ContainerID: entry.ContainerID,
			ProcessName: entry.ProcessName,
		},
		Action: ActionExecuted{
			Mechanism:           mech,
			SignalSent:          "SIGCONT",
			Status:              ActionStatusSuccess,
			ExecutionDurationMs: elapsed,
		},
		Policy: PolicyContext{
			IsWhitelistedEntity: false,
			ReversibilityToken:  token,
		},
	}

	select {
	case m.events <- evt:
	default:
		m.logf("mitigator: event channel full, dropping resume event for token %s", token)
	}

	return &evt, nil
}

func (m *Mitigator) ActiveMitigations() []LedgerEntry {
	return m.ledger.ListActive()
}

func (m *Mitigator) MitigationCount() int {
	return m.ledger.Count()
}

func (m *Mitigator) UpdateMitigationState(token string, state ActionState) bool {
	return m.ledger.UpdateState(token, state)
}

func (m *Mitigator) RollbackPID(pid int, containerID string) error {
	if containerID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), m.cfg.DockerTimeout)
		defer cancel()
		err := m.cm.Unpause(ctx, containerID)
		if err == nil {
			return nil
		}
		m.logf("mitigator: rollback container unpause failed %s: %v, trying SIGCONT", containerID, err)
	}
	return UnfreezeProcess(pid)
}

func (m *Mitigator) emitEvent(ctx context.Context, evt MitigationEvent) {
	select {
	case m.events <- evt:
	default:
		m.logf("mitigator: event channel full, dropping event %s", evt.MitigationEventID)
	}

	if err := json.NewEncoder(m.console).Encode(evt); err != nil {
		m.logf("mitigator: encode console event: %v", err)
	}
}

func (m *Mitigator) resolveProcessName(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("%s/%d/cmdline", m.cfg.ProcRoot, pid))
	if err != nil {
		return ""
	}
	return extractCmdline(data)
}

func (m *Mitigator) resolveContainerID(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("%s/%d/cgroup", m.cfg.ProcRoot, pid))
	if err != nil {
		return ""
	}
	return extractHexContainerID(data)
}

func extractCmdline(data []byte) string {
	end := len(data)
	for i, b := range data {
		if b == 0 {
			end = i
			break
		}
	}
	name := string(data[:end])
	if idx := stringsLastIndexByte(name, '/'); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}

func extractHexContainerID(data []byte) string {
	lines := splitLines(data)
	for _, line := range lines {
		if line == "" {
			continue
		}
		last := lastField(line)
		candidates := split(last, '/')
		for _, seg := range candidates {
			if id := hexFromSegment(seg); id != "" {
				return id
			}
		}
	}
	return ""
}

func hexFromSegment(seg string) string {
	if len(seg) >= 12 && isHex(seg) {
		return seg
	}
	if idx := stringsLastIndexByte(seg, '-'); idx >= 0 {
		candidate := seg[idx+1:]
		if dot := stringsLastIndexByte(candidate, '.'); dot >= 0 {
			candidate = candidate[:dot]
		}
		if len(candidate) >= 12 && isHex(candidate) {
			return candidate
		}
	}
	if dot := stringsLastIndexByte(seg, '.'); dot >= 0 {
		before := seg[:dot]
		if idx := stringsLastIndexByte(before, '-'); idx >= 0 {
			candidate := before[idx+1:]
			if len(candidate) >= 12 && isHex(candidate) {
				return candidate
			}
		}
		if len(before) >= 12 && isHex(before) {
			return before
		}
	}
	return ""
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, string(data[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}

func lastField(s string) string {
	idx := stringsLastIndexByte(s, ':')
	if idx < 0 {
		return s
	}
	return s[idx+1:]
}

func split(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			if i > start {
				parts = append(parts, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

func stringsLastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func isHex(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
