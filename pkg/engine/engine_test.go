package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/config"
	"github.com/anomalyco/ai-compute-profiler/pkg/model"
)

func TestNewEngine_DefaultConfig(t *testing.T) {
	e := New(nil, nil)
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	if e.cfg == nil {
		t.Fatal("expected default config")
	}
	if e.collector == nil {
		t.Error("expected collector")
	}
	if e.pipeline == nil {
		t.Error("expected pipeline")
	}
	if e.detector == nil {
		t.Error("expected detector (default enabled)")
	}
	if e.profiler == nil {
		t.Error("expected profiler (default enabled)")
	}
	if e.alerter == nil {
		t.Error("expected alerter")
	}
	if e.apiServer == nil {
		t.Error("expected API server")
	}
}

func TestNewEngine_ComponentsDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DetectorEnabled = false
	cfg.ProfilerEnabled = false

	e := New(cfg, t.Logf)
	if e.detector != nil {
		t.Error("expected nil detector")
	}
	if e.profiler != nil {
		t.Error("expected nil profiler")
	}
}

func TestEngine_StartStop(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DetectorEnabled = false
	cfg.ProfilerEnabled = false
	cfg.ProxyEnabled = false
	cfg.PollInterval = 50 * time.Millisecond
	cfg.DashboardAddr = "127.0.0.1:0"

	e := New(cfg, t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- e.Start(ctx)
	}()

	time.Sleep(200 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("engine did not stop within 5s")
	}
}

func TestEngine_SnapshotDataRouting(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DetectorEnabled = false
	cfg.ProfilerEnabled = false
	cfg.ProxyEnabled = false
	cfg.PollInterval = 1 * time.Second
	cfg.DashboardAddr = "127.0.0.1:0"

	e := New(cfg, t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- e.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	snap := model.Snapshot{
		Timestamp: time.Now().Unix(),
		MonitoredProcesses: []model.ProcessMetrics{
			{PID: 1001, ProcessName: "test-process", CPUUsagePct: 5.0, RSSBytes: 64 * 1024 * 1024},
			{PID: 1002, ProcessName: "another-process", CPUUsagePct: 2.5, RSSBytes: 32 * 1024 * 1024},
		},
	}
	data, _ := json.Marshal(snap)
	e.pipeline.SnapshotCh <- data

	time.Sleep(200 * time.Millisecond)

	metrics := e.loadMetrics()
	if metrics == nil {
		t.Fatal("expected non-nil metrics after snapshot")
	}
	if metrics.ActivePIDsCount != 2 {
		t.Errorf("expected 2 active PIDs, got %d", metrics.ActivePIDsCount)
	}
	if metrics.EngineStatus != "RUNNING" {
		t.Errorf("expected RUNNING status, got %s", metrics.EngineStatus)
	}
	if metrics.LocalNodeID != cfg.AgentID {
		t.Errorf("expected node ID %s, got %s", cfg.AgentID, metrics.LocalNodeID)
	}
	if metrics.SelfCheck.ProfilerMemoryRSSBytes == 0 {
		t.Error("expected non-zero memory RSS")
	}

	cancel()
	<-done
}

func TestEngine_MetricsAnomalies(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DetectorEnabled = false
	cfg.ProfilerEnabled = false
	cfg.ProxyEnabled = false
	cfg.PollInterval = 1 * time.Second
	cfg.DashboardAddr = "127.0.0.1:0"

	e := New(cfg, t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- e.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	e.metricsPtr.Store(&UnifiedMetrics{
		EngineStatus:  "RUNNING",
		UptimeSeconds: 42,
		LocalNodeID:   "test-node",
		ActivePIDsCount: 3,
		Anomalies: []AnomalyEntry{
			{
				PID:   1001,
				State: "active",
				ImpactMetrics: ImpactMetrics{
					VRAMLeakBytesPerSec: 1024,
					TokenWasteCount:     5,
				},
			},
		},
		SelfCheck: SelfCheckMetrics{
			ProfilerMemoryRSSBytes: 1024 * 1024,
			ProfilerCPUUtilPct:     0.5,
		},
	})

	metrics := e.loadMetrics()
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	if len(metrics.Anomalies) != 1 {
		t.Fatalf("expected 1 anomaly, got %d", len(metrics.Anomalies))
	}
	if metrics.Anomalies[0].State != "active" {
		t.Errorf("expected state active, got %s", metrics.Anomalies[0].State)
	}
	if metrics.Anomalies[0].ImpactMetrics.VRAMLeakBytesPerSec != 1024 {
		t.Errorf("expected vram leak 1024, got %f", metrics.Anomalies[0].ImpactMetrics.VRAMLeakBytesPerSec)
	}

	cancel()
	<-done
}

func TestAPI_EndpointReturnsMetrics(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DetectorEnabled = false
	cfg.ProfilerEnabled = false
	cfg.ProxyEnabled = false
	cfg.PollInterval = 1 * time.Second
	cfg.DashboardAddr = "127.0.0.1:0"

	e := New(cfg, t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- e.Start(ctx)
	}()

	time.Sleep(200 * time.Millisecond)

	resp, err := http.Get("http://" + e.apiServer.addr + "/api/v1/metrics")
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result UnifiedMetrics
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("JSON decode failed: %v", err)
	}
	if result.EngineStatus != "STARTING" && result.EngineStatus != "RUNNING" {
		t.Errorf("unexpected engine status: %s", result.EngineStatus)
	}

	cancel()
	<-done
}

func TestEngine_MultipleStop(t *testing.T) {
	e := New(nil, t.Logf)

	e.Stop()
	e.Stop()
}

func TestEngine_UptimeIncreases(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DetectorEnabled = false
	cfg.ProfilerEnabled = false
	cfg.ProxyEnabled = false
	cfg.PollInterval = 1 * time.Second
	cfg.DashboardAddr = "127.0.0.1:0"

	e := New(cfg, t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- e.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	snap := model.Snapshot{
		Timestamp:          time.Now().Unix(),
		MonitoredProcesses: []model.ProcessMetrics{{PID: 1, ProcessName: "init"}},
	}
	data, _ := json.Marshal(snap)
	e.pipeline.SnapshotCh <- data

	time.Sleep(100 * time.Millisecond)
	m1 := e.loadMetrics()

	time.Sleep(500 * time.Millisecond)
	e.pipeline.SnapshotCh <- data
	time.Sleep(100 * time.Millisecond)
	m2 := e.loadMetrics()

	if m1 != nil && m2 != nil && m2.UptimeSeconds < m1.UptimeSeconds {
		t.Errorf("uptime should not decrease: before=%d after=%d", m1.UptimeSeconds, m2.UptimeSeconds)
	}

	cancel()
	<-done
}

func TestEngine_StopWithoutStart(t *testing.T) {
	e := New(nil, t.Logf)
	e.Stop()
}

func TestNew_NilLogger(t *testing.T) {
	e := New(nil, nil)
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	e.Stop()
}
