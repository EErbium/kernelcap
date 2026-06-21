package mitigator

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/alerter"
	"github.com/anomalyco/ai-compute-profiler/pkg/policy"
)

func TestPolicyEvaluator_Whitelist(t *testing.T) {
	cfg := policy.DefaultConfig()
	cfg.WhitelistNames = []string{"systemd", "dockerd", "kubelet"}
	eng := policy.NewEngine(cfg, t.Logf)
	p := NewPolicyEvaluator(eng)

	tests := []struct {
		name        string
		pid         int
		containerID string
		processName string
		want        bool
	}{
		{"systemd by name", 1, "", "systemd", true},
		{"dockerd by name", 1000, "", "dockerd", true},
		{"kubelet by name", 2000, "", "kubelet", true},
		{"unknown process", 3000, "", "python3", false},
		{"unknown container", 5000, "xyz789", "python3", false},
		{"empty name and id", 6000, "", "", false},
		{"empty container with known name", 1, "", "systemd", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.IsWhitelisted(tt.pid, tt.containerID, tt.processName)
			if got != tt.want {
				t.Errorf("IsWhitelisted(%d, %q, %q) = %v, want %v",
					tt.pid, tt.containerID, tt.processName, got, tt.want)
			}
		})
	}
}

func TestPolicyEvaluator_DefaultWhitelist(t *testing.T) {
	p := NewPolicyEvaluator(nil)
	if p.IsWhitelisted(0, "", "random-process") {
		t.Error("expected false for unknown process with empty whitelist")
	}
	if p.IsWhitelisted(0, "some-container", "") {
		t.Error("expected false for unknown container with empty whitelist")
	}
}

func TestLedger_RecordAndResolve(t *testing.T) {
	l := NewLedger()

	entry := LedgerEntry{
		PID:         12345,
		ContainerID: "abc123",
		ProcessName: "python3",
		AnomalyType: "IDLE_GPU_HOG",
		Mechanism:   MechanismSignalStop,
	}

	token := l.Record(entry)
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	resolved := l.ResolveByToken(token)
	if resolved == nil {
		t.Fatal("expected to resolve entry by token")
	}
	if resolved.PID != 12345 {
		t.Errorf("PID = %d, want 12345", resolved.PID)
	}
	if resolved.State != ActionPaused {
		t.Errorf("State = %s, want PAUSED", resolved.State)
	}
	if resolved.AnomalyType != "IDLE_GPU_HOG" {
		t.Errorf("AnomalyType = %s, want IDLE_GPU_HOG", resolved.AnomalyType)
	}

	notFound := l.ResolveByToken("nonexistent")
	if notFound != nil {
		t.Error("expected nil for nonexistent token")
	}
}

func TestLedger_UpdateState(t *testing.T) {
	l := NewLedger()

	token := l.Record(LedgerEntry{PID: 99, Mechanism: MechanismSignalStop})

	if !l.UpdateState(token, ActionResumed) {
		t.Fatal("UpdateState returned false for valid token")
	}
	entry := l.ResolveByToken(token)
	if entry.State != ActionResumed {
		t.Errorf("State = %s, want RESUMED", entry.State)
	}

	if l.UpdateState("badtoken", ActionResumed) {
		t.Error("UpdateState returned true for invalid token")
	}
}

func TestLedger_ListActive(t *testing.T) {
	l := NewLedger()

	l.Record(LedgerEntry{PID: 1, Mechanism: MechanismSignalStop, AnomalyType: "IDLE_GPU_HOG"})
	l.Record(LedgerEntry{PID: 2, Mechanism: MechanismContainerPause, AnomalyType: "HOST_MEMORY_LEAK"})

	active := l.ListActive()
	if len(active) != 2 {
		t.Errorf("ListActive returned %d entries, want 2", len(active))
	}

	l.UpdateState(active[0].ReversibilityToken, ActionResumed)
	active = l.ListActive()
	if len(active) != 1 {
		t.Errorf("ListActive after resume returned %d entries, want 1", len(active))
	}
}

func TestLedger_ConcurrentAccess(t *testing.T) {
	l := NewLedger()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token := l.Record(LedgerEntry{
				PID:         i,
				ProcessName: "test",
				Mechanism:   MechanismSignalStop,
			})
			l.ResolveByToken(token)
			l.UpdateState(token, ActionResumed)
			l.ListActive()
		}(i)
	}
	wg.Wait()

	if l.Count() != 50 {
		t.Errorf("Count = %d, want 50", l.Count())
	}
}

func TestMitigationEvent_JSONSchema(t *testing.T) {
	evt := MitigationEvent{
		MitigationEventID: "mit_7d291a4e-1280-49bf-9a3d-3a10cf0d921b",
		Timestamp:         1782352800,
		Target: TargetIdentifier{
			PID:         41029,
			ContainerID: "a1b2c3d4e5f67890abcdef",
			ProcessName: "python3-agent-node",
		},
		Action: ActionExecuted{
			Mechanism:           MechanismContainerPause,
			SignalSent:          "SIGSTOP",
			Status:              ActionStatusSuccess,
			ExecutionDurationMs: 0.85,
		},
		Policy: PolicyContext{
			IsWhitelistedEntity: false,
			ReversibilityToken:  "rev_token_01abcde429",
		},
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded MitigationEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.MitigationEventID != evt.MitigationEventID {
		t.Errorf("EventID mismatch: %s vs %s", decoded.MitigationEventID, evt.MitigationEventID)
	}
	if decoded.Target.PID != 41029 {
		t.Errorf("PID mismatch: %d", decoded.Target.PID)
	}
	if decoded.Action.Mechanism != MechanismContainerPause {
		t.Errorf("Mechanism mismatch: %s", decoded.Action.Mechanism)
	}
	if decoded.Policy.IsWhitelistedEntity != false {
		t.Errorf("IsWhitelistedEntity mismatch: %v", decoded.Policy.IsWhitelistedEntity)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal raw: %v", err)
	}

	requiredFields := []string{"mitigation_event_id", "timestamp", "target_identifier", "action_executed", "policy_context"}
	for _, field := range requiredFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing required JSON field: %s", field)
		}
	}
}

func TestMitigator_SkipsWhitelisted(t *testing.T) {
	logf := func(string, ...any) {}
	m := New(Config{
		WhitelistNames: []string{"allowed-process"},
		BufferSize:     16,
	}, logf)

	alert := alerter.ConsolidatedAlert{
		EventID:   "test-1",
		Timestamp: time.Now().Unix(),
		Payload: alerter.AlertPayload{
			TargetPID:   99999,
			AnomalyType: "TEST",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alertCh := make(chan alerter.ConsolidatedAlert, 4)
	m.Start(ctx, alertCh)

	alertCh <- alert
	time.Sleep(100 * time.Millisecond)

	active := m.ActiveMitigations()
	if len(active) != 0 {
		t.Errorf("expected 0 active mitigations for whitelisted, got %d", len(active))
	}

	close(alertCh)
	m.Stop()
}

func TestFreezeUnfreeze_LocalSubprocess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("FreezeProcess/UnfreezeProcess requires Linux")
	}
	if os.Geteuid() != 0 {
		t.Skip("FreezeProcess/UnfreezeProcess requires root")
	}

	proc := startSleepProcess(t)
	defer proc.Kill()

	pid := proc.Pid

	if err := FreezeProcess(pid); err != nil {
		t.Fatalf("FreezeProcess(%d): %v", pid, err)
	}

	if err := UnfreezeProcess(pid); err != nil {
		t.Fatalf("UnfreezeProcess(%d): %v", pid, err)
	}
}

func TestExtractCmdline(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"simple", []byte("python3\x00--arg\x00"), "python3"},
		{"full path", []byte("/usr/bin/python3\x00--arg\x00"), "python3"},
		{"empty", []byte{}, ""},
		{"no null terminator", []byte("bash"), "bash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCmdline(tt.data)
			if got != tt.want {
				t.Errorf("extractCmdline(%q) = %q, want %q", string(tt.data), got, tt.want)
			}
		})
	}
}

func TestExtractHexContainerID(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			"docker cgroup v2",
			[]byte("0::/system.slice/docker-abc123def456.scope\n"),
			"abc123def456",
		},
		{
			"docker cgroup v1",
			[]byte("1:name=systemd:/docker/abc123def456\n"),
			"abc123def456",
		},
		{
			"no container id",
			[]byte("0::/system.slice/init.scope\n"),
			"",
		},
		{
			"kubepods",
			[]byte("0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-poduid123.slice/crio-abc123def456.scope\n"),
			"abc123def456",
		},
		{
			"empty cgroup",
			[]byte{},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHexContainerID(tt.data)
			if got != tt.want {
				t.Errorf("extractHexContainerID(%q) = %q, want %q", string(tt.data), got, tt.want)
			}
		})
	}
}

func startSleepProcess(t *testing.T) *os.Process {
	t.Helper()
	attr := &os.ProcAttr{Files: []*os.File{nil, nil, nil}}
	proc, err := os.StartProcess("/bin/sleep", []string{"sleep", "60"}, attr)
	if err != nil {
		t.Fatalf("start sleep process: %v", err)
	}
	return proc
}
