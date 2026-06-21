package profiler

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/model"
)

func TestGPU_Efficiency_Basic(t *testing.T) {
	eta := gpuEfficiency(0.45, 40*1024*1024*1024, 80*1024*1024*1024)
	expected := 0.009
	if math.Abs(eta-expected) > 1e-6 {
		t.Errorf("expected efficiency %.6f, got %.6f", expected, eta)
	}
}

func TestGPU_Efficiency_HighSM(t *testing.T) {
	eta := gpuEfficiency(85.0, 8*1024*1024*1024, 80*1024*1024*1024)
	vramPct := (8.0 / 80.0) * 100.0
	expected := 85.0 / vramPct
	if math.Abs(eta-expected) > 1e-6 {
		t.Errorf("expected efficiency %.6f, got %.6f", expected, eta)
	}
}

func TestGPU_Efficiency_ZeroSM(t *testing.T) {
	eta := gpuEfficiency(0, 40*1024*1024*1024, 80*1024*1024*1024)
	if eta != 0 {
		t.Errorf("expected 0 for zero SM util, got %.6f", eta)
	}
}

func TestGPU_Efficiency_TinyVRAM(t *testing.T) {
	eta := gpuEfficiency(50.0, 1*1024*1024, 80*1024*1024*1024)
	if eta < 49 || eta > 51 {
		t.Errorf("expected efficiency near 50 for tiny VRAM, got %.6f", eta)
	}
}

func TestGPU_Efficiency_ZeroTotalVRAM(t *testing.T) {
	eta := gpuEfficiency(50.0, 1024, 0)
	if eta < 0 || eta >= 1.0 {
		t.Errorf("expected efficiency in [0,1) for zero total VRAM, got %.6f", eta)
	}
}

func TestGPU_Efficiency_ZeroAllocatedVRAM(t *testing.T) {
	eta := gpuEfficiency(50.0, 0, 80*1024*1024*1024)
	if eta != 50.0 {
		t.Errorf("expected 50 for zero allocated VRAM, got %.6f", eta)
	}
}

func TestResourceHistory_Append(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 100, GPUUID: "GPU-abc"}, 10, 5*time.Minute)
	now := time.Now().Unix()

	for i := 0; i < 5; i++ {
		rh.Append(TimeSeriesPoint{
			Timestamp: now + int64(i),
			SMUtilPct: float64(i) * 10,
			VRAMUsed:  uint64(i) * 1024,
		})
	}

	if rh.Len() != 5 {
		t.Errorf("expected 5 points, got %d", rh.Len())
	}
	if rh.LatestVRAMUsed() != 4*1024 {
		t.Errorf("expected VRAM 4096, got %d", rh.LatestVRAMUsed())
	}
}

func TestResourceHistory_EvictsOverMax(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 100, GPUUID: "GPU-abc"}, 3, 5*time.Minute)
	now := time.Now().Unix()

	for i := 0; i < 10; i++ {
		rh.Append(TimeSeriesPoint{
			Timestamp: now + int64(i),
			SMUtilPct: float64(i),
		})
	}

	if rh.Len() != 3 {
		t.Errorf("expected 3 points after eviction, got %d", rh.Len())
	}
	if rh.OldestTimestamp() != now+7 {
		t.Errorf("expected oldest timestamp %d, got %d", now+7, rh.OldestTimestamp())
	}
}

func TestResourceHistory_MaxAgeEviction(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 100, GPUUID: "GPU-abc"}, 100, 10*time.Second)
	now := time.Now().Unix()

	for i := 0; i < 5; i++ {
		rh.Append(TimeSeriesPoint{Timestamp: now + int64(i)})
	}

	rh.Append(TimeSeriesPoint{Timestamp: now + 20})

	if rh.Len() != 1 {
		t.Errorf("expected 1 point after max-age eviction (all old ones expired), got %d", rh.Len())
	}
}

func TestResourceHistory_RollingAvgSMUtil(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 100, GPUUID: "GPU-abc"}, 10, 5*time.Minute)
	now := time.Now().Unix()

	for i := 0; i < 4; i++ {
		rh.Append(TimeSeriesPoint{
			Timestamp: now + int64(i),
			SMUtilPct: float64(i+1) * 10,
		})
	}

	avg := rh.RollingAvgSMUtil()
	expected := (10.0 + 20.0 + 30.0 + 40.0) / 4.0
	if math.Abs(avg-expected) > 1e-6 {
		t.Errorf("expected avg SM %.2f, got %.2f", expected, avg)
	}
}

func TestResourceHistory_EmptyRollingAvg(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 100, GPUUID: "GPU-abc"}, 10, 5*time.Minute)
	if avg := rh.RollingAvgSMUtil(); avg != 0 {
		t.Errorf("expected 0 for empty history, got %.2f", avg)
	}
}

func TestResourceHistory_MaxVRAMUsed(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 100, GPUUID: "GPU-abc"}, 10, 5*time.Minute)
	now := time.Now().Unix()

	rh.Append(TimeSeriesPoint{Timestamp: now, VRAMUsed: 100})
	rh.Append(TimeSeriesPoint{Timestamp: now + 1, VRAMUsed: 500})
	rh.Append(TimeSeriesPoint{Timestamp: now + 2, VRAMUsed: 200})

	if max := rh.MaxVRAMUsed(); max != 500 {
		t.Errorf("expected max VRAM 500, got %d", max)
	}
}

func TestOLSRegression_PerfectLine(t *testing.T) {
	x := []float64{0, 1, 2, 3, 4}
	y := []float64{1, 3, 5, 7, 9}

	slope, intercept, rSquared := OLSSlope(x, y)

	if math.Abs(slope-2.0) > 1e-10 {
		t.Errorf("expected slope 2.0, got %.10f", slope)
	}
	if math.Abs(intercept-1.0) > 1e-10 {
		t.Errorf("expected intercept 1.0, got %.10f", intercept)
	}
	if math.Abs(rSquared-1.0) > 1e-10 {
		t.Errorf("expected R² 1.0, got %.10f", rSquared)
	}
}

func TestOLSRegression_Flat(t *testing.T) {
	x := []float64{0, 1, 2, 3, 4, 5}
	y := []float64{100, 100, 100, 100, 100, 100}

	slope, intercept, rSquared := OLSSlope(x, y)

	if math.Abs(slope) > 1e-10 {
		t.Errorf("expected slope 0.0, got %.10f", slope)
	}
	if math.Abs(intercept-100.0) > 1e-10 {
		t.Errorf("expected intercept 100.0, got %.10f", intercept)
	}
	if rSquared != 0 {
		t.Errorf("expected R² 0.0, got %.10f", rSquared)
	}
}

func TestOLSRegression_NegativeSlope(t *testing.T) {
	x := []float64{0, 1, 2, 3, 4}
	y := []float64{10, 8, 6, 4, 2}

	slope, intercept, rSquared := OLSSlope(x, y)

	if math.Abs(slope+2.0) > 1e-10 {
		t.Errorf("expected slope -2.0, got %.10f", slope)
	}
	if math.Abs(intercept-10.0) > 1e-10 {
		t.Errorf("expected intercept 10.0, got %.10f", intercept)
	}
	if math.Abs(rSquared-1.0) > 1e-10 {
		t.Errorf("expected R² 1.0, got %.10f", rSquared)
	}
}

func TestOLSRegression_SinglePoint(t *testing.T) {
	x := []float64{42}
	y := []float64{100}

	slope, intercept, rSquared := OLSSlope(x, y)
	if slope != 0 || intercept != 0 || rSquared != 0 {
		t.Errorf("expected all zeros for single point, got slope=%.4f intercept=%.4f r2=%.4f", slope, intercept, rSquared)
	}
}

func TestOLSRegression_Empty(t *testing.T) {
	slope, intercept, rSquared := OLSSlope(nil, nil)
	if slope != 0 || intercept != 0 || rSquared != 0 {
		t.Errorf("expected all zeros for empty input")
	}
}

func TestProfiler_IdleGPU_Hog(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdleVRAMThreshold = 1
	cfg.IdleSMThresholdPct = 5.0
	cfg.IdleDuration = 1 * time.Second
	cfg.AlertCooldown = time.Hour

	p := NewProfiler(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	now := time.Now().Unix()

	for i := 0; i < 15; i++ {
		snap := &model.Snapshot{
			Timestamp: now + int64(i),
			GPUDevices: []model.GPUDeviceMetrics{
				{
					UUID:             "GPU-deadbeef",
					SMUtilizationPct: 0.5,
					MemoryTotalBytes: 80 * 1024 * 1024 * 1024,
					RunningProcesses: []model.GPUProcessMetrics{
						{PID: 41029, VRAMUsedBytes: 40 * 1024 * 1024 * 1024},
					},
				},
			},
		}
		p.Ingest(snap)
	}

	select {
	case alert := <-p.AlertCh():
		if alert.Alert.AnomalyType != AnomalyIdleGPU {
			t.Errorf("expected IDLE_GPU_HOG, got %s", alert.Alert.AnomalyType)
		}
		if alert.Alert.Severity != SeverityCritical {
			t.Errorf("expected CRITICAL severity, got %s", alert.Alert.Severity)
		}
		if alert.Alert.TargetPID != 41029 {
			t.Errorf("expected PID 41029, got %d", alert.Alert.TargetPID)
		}
		if alert.Alert.GPUUID != "GPU-deadbeef" {
			t.Errorf("expected GPU UUID GPU-deadbeef, got %s", alert.Alert.GPUUID)
		}
		if alert.Alert.MetricsSummary.CurrentVRAMAllocationBytes != 40*1024*1024*1024 {
			t.Errorf("expected VRAM 40GB, got %d", alert.Alert.MetricsSummary.CurrentVRAMAllocationBytes)
		}
		if alert.Alert.MetricsSummary.CalculatedEfficiencyCoefficient <= 0 {
			t.Errorf("expected positive efficiency coefficient, got %.6f", alert.Alert.MetricsSummary.CalculatedEfficiencyCoefficient)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected idle GPU alert but got none (timeout)")
	}
}

func TestProfiler_IdleGPU_NoAlert_HighSM(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdleVRAMThreshold = 1
	cfg.IdleSMThresholdPct = 5.0
	cfg.IdleDuration = 1 * time.Second

	p := NewProfiler(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	now := time.Now().Unix()

	for i := 0; i < 15; i++ {
		snap := &model.Snapshot{
			Timestamp: now + int64(i),
			GPUDevices: []model.GPUDeviceMetrics{
				{
					UUID:             "GPU-deadbeef",
					SMUtilizationPct: 85.0,
					MemoryTotalBytes: 80 * 1024 * 1024 * 1024,
					RunningProcesses: []model.GPUProcessMetrics{
						{PID: 41029, VRAMUsedBytes: 40 * 1024 * 1024 * 1024},
					},
				},
			},
		}
		p.Ingest(snap)
	}

	select {
	case alert := <-p.AlertCh():
		t.Errorf("unexpected alert (SM util is high): %+v", alert)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestProfiler_IdleGPU_NoAlert_LowVRAM(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdleVRAMThreshold = 100 * 1024 * 1024 * 1024
	cfg.IdleSMThresholdPct = 5.0
	cfg.IdleDuration = 1 * time.Second

	p := NewProfiler(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	now := time.Now().Unix()

	for i := 0; i < 15; i++ {
		snap := &model.Snapshot{
			Timestamp: now + int64(i),
			GPUDevices: []model.GPUDeviceMetrics{
				{
					UUID:             "GPU-deadbeef",
					SMUtilizationPct: 0.5,
					MemoryTotalBytes: 80 * 1024 * 1024 * 1024,
					RunningProcesses: []model.GPUProcessMetrics{
						{PID: 41029, VRAMUsedBytes: 1 * 1024 * 1024},
					},
				},
			},
		}
		p.Ingest(snap)
	}

	select {
	case alert := <-p.AlertCh():
		t.Errorf("unexpected alert (VRAM below threshold): %+v", alert)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestProfiler_MemoryLeak_Positive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LeakSlopeThreshold = 1
	cfg.LeakRSquaredThreshold = 0.80
	cfg.AnalysisInterval = 10 * time.Millisecond
	cfg.AlertCooldown = time.Hour

	p := NewProfiler(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	now := time.Now().Unix()
	baseRSS := uint64(100 * 1024 * 1024)

	for i := 0; i < 20; i++ {
		snap := &model.Snapshot{
			Timestamp: now + int64(i),
			MonitoredProcesses: []model.ProcessMetrics{
				{
					PID:         999,
					RSSBytes:    baseRSS + uint64(i)*10*1024*1024,
					CPUUsagePct: 5.0,
				},
			},
		}
		p.Ingest(snap)
	}

	time.Sleep(100 * time.Millisecond)

	select {
	case alert := <-p.AlertCh():
		if alert.Alert.AnomalyType != AnomalyMemoryLeak {
			t.Errorf("expected HOST_MEMORY_LEAK, got %s", alert.Alert.AnomalyType)
		}
		if alert.Alert.Severity != SeverityWarning {
			t.Errorf("expected WARNING severity, got %s", alert.Alert.Severity)
		}
		if alert.Alert.TargetPID != 999 {
			t.Errorf("expected PID 999, got %d", alert.Alert.TargetPID)
		}
		if alert.Alert.MemoryTrend.OLSSlopeBytesPerSec <= 0 {
			t.Errorf("expected positive slope, got %.2f", alert.Alert.MemoryTrend.OLSSlopeBytesPerSec)
		}
		if alert.Alert.MemoryTrend.LinearityRSquared < 0.80 {
			t.Errorf("expected R² >= 0.80, got %.4f", alert.Alert.MemoryTrend.LinearityRSquared)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("expected memory leak alert but got none (timeout)")
	}
}

func TestProfiler_MemoryLeak_Negative_Flat(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LeakSlopeThreshold = 1
	cfg.LeakRSquaredThreshold = 0.80
	cfg.AnalysisInterval = 10 * time.Millisecond

	p := NewProfiler(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	now := time.Now().Unix()

	for i := 0; i < 20; i++ {
		snap := &model.Snapshot{
			Timestamp: now + int64(i),
			MonitoredProcesses: []model.ProcessMetrics{
				{
					PID:         888,
					RSSBytes:    100 * 1024 * 1024,
					CPUUsagePct: 5.0,
				},
			},
		}
		p.Ingest(snap)
	}

	time.Sleep(100 * time.Millisecond)

	select {
	case alert := <-p.AlertCh():
		t.Errorf("unexpected alert for flat RSS: %+v", alert)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestProfiler_MemoryLeak_Negative_CPUIncreasing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LeakSlopeThreshold = 1
	cfg.LeakRSquaredThreshold = 0.80
	cfg.AnalysisInterval = 10 * time.Millisecond

	p := NewProfiler(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	now := time.Now().Unix()
	baseRSS := uint64(100 * 1024 * 1024)

	for i := 0; i < 20; i++ {
		snap := &model.Snapshot{
			Timestamp: now + int64(i),
			MonitoredProcesses: []model.ProcessMetrics{
				{
					PID:         777,
					RSSBytes:    baseRSS + uint64(i)*10*1024*1024,
					CPUUsagePct: 5.0 + float64(i),
				},
			},
		}
		p.Ingest(snap)
	}

	time.Sleep(100 * time.Millisecond)

	select {
	case alert := <-p.AlertCh():
		t.Errorf("unexpected alert (CPU is increasing): %+v", alert)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestProfiler_AlertCooldown(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdleVRAMThreshold = 1
	cfg.IdleSMThresholdPct = 5.0
	cfg.IdleDuration = 1 * time.Second
	cfg.AlertCooldown = time.Hour

	p := NewProfiler(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	now := time.Now().Unix()

	for i := 0; i < 15; i++ {
		snap := &model.Snapshot{
			Timestamp: now + int64(i),
			GPUDevices: []model.GPUDeviceMetrics{
				{
					UUID:             "GPU-abc",
					SMUtilizationPct: 0.5,
					MemoryTotalBytes: 80 * 1024 * 1024 * 1024,
					RunningProcesses: []model.GPUProcessMetrics{
						{PID: 41029, VRAMUsedBytes: 40 * 1024 * 1024 * 1024},
					},
				},
			},
		}
		p.Ingest(snap)
	}

	select {
	case <-p.AlertCh():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected first alert but got none")
	}

	for i := 15; i < 30; i++ {
		snap := &model.Snapshot{
			Timestamp: now + int64(i),
			GPUDevices: []model.GPUDeviceMetrics{
				{
					UUID:             "GPU-abc",
					SMUtilizationPct: 0.5,
					MemoryTotalBytes: 80 * 1024 * 1024 * 1024,
					RunningProcesses: []model.GPUProcessMetrics{
						{PID: 41029, VRAMUsedBytes: 40 * 1024 * 1024 * 1024},
					},
				},
			},
		}
		p.Ingest(snap)
	}

	select {
	case alert := <-p.AlertCh():
		t.Errorf("expected second alert to be suppressed by cooldown, got: %+v", alert)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestProfiler_ConcurrentIngest(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdleVRAMThreshold = 1
	cfg.IdleSMThresholdPct = 5.0
	cfg.IdleDuration = 1 * time.Second
	cfg.AlertCooldown = time.Hour

	p := NewProfiler(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	var wg sync.WaitGroup
	now := time.Now().Unix()

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pid := uint32(10000 + idx%5)
			snap := &model.Snapshot{
				Timestamp: now + int64(idx),
				GPUDevices: []model.GPUDeviceMetrics{
					{
						UUID:             "GPU-concurrent",
						SMUtilizationPct: 0.5,
						MemoryTotalBytes: 80 * 1024 * 1024 * 1024,
						RunningProcesses: []model.GPUProcessMetrics{
							{PID: pid, VRAMUsedBytes: 40 * 1024 * 1024 * 1024},
						},
					},
				},
				MonitoredProcesses: []model.ProcessMetrics{
					{PID: int(pid), RSSBytes: 100 * 1024 * 1024, CPUUsagePct: 10.0},
				},
			}
			p.Ingest(snap)
		}(i)
	}
	wg.Wait()

	p.mu.RLock()
	count := len(p.histories)
	p.mu.RUnlock()

	if count < 5 {
		t.Errorf("expected at least 5 histories, got %d", count)
	}
}

func TestProfiler_MultipleGPUs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdleVRAMThreshold = 1
	cfg.IdleSMThresholdPct = 5.0
	cfg.IdleDuration = 1 * time.Second
	cfg.AlertCooldown = time.Hour

	p := NewProfiler(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	now := time.Now().Unix()

	for i := 0; i < 15; i++ {
		snap := &model.Snapshot{
			Timestamp: now + int64(i),
			GPUDevices: []model.GPUDeviceMetrics{
				{
					UUID:             "GPU-aaa",
					SMUtilizationPct: 0.5,
					MemoryTotalBytes: 80 * 1024 * 1024 * 1024,
					RunningProcesses: []model.GPUProcessMetrics{
						{PID: 100, VRAMUsedBytes: 40 * 1024 * 1024 * 1024},
					},
				},
				{
					UUID:             "GPU-bbb",
					SMUtilizationPct: 0.3,
					MemoryTotalBytes: 80 * 1024 * 1024 * 1024,
					RunningProcesses: []model.GPUProcessMetrics{
						{PID: 200, VRAMUsedBytes: 50 * 1024 * 1024 * 1024},
					},
				},
			},
		}
		p.Ingest(snap)
	}

	alerts := make(map[int64]bool)
	for i := 0; i < 2; i++ {
		select {
		case alert := <-p.AlertCh():
			alerts[alert.Alert.TargetPID] = true
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("expected 2 alerts, got %d", len(alerts))
		}
	}

	if !alerts[100] {
		t.Error("expected alert for PID 100 on GPU-aaa")
	}
	if !alerts[200] {
		t.Error("expected alert for PID 200 on GPU-bbb")
	}
}

func TestProfiler_GCRemovesStaleHistories(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAge = 1 * time.Millisecond
	cfg.GCInterval = 10 * time.Millisecond

	p := NewProfiler(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	snap := &model.Snapshot{
		Timestamp: time.Now().Unix(),
		MonitoredProcesses: []model.ProcessMetrics{
			{PID: 666, RSSBytes: 100 * 1024 * 1024},
		},
	}
	p.Ingest(snap)

	key := ResourceKey{PID: 666, GPUUID: ""}
	p.mu.RLock()
	_, ok := p.histories[key]
	p.mu.RUnlock()
	if !ok {
		t.Fatal("expected history to exist before GC")
	}

	time.Sleep(50 * time.Millisecond)

	p.mu.RLock()
	_, ok = p.histories[key]
	p.mu.RUnlock()
	if ok {
		t.Error("expected stale history to be removed by GC")
	}
}

func TestProfiler_GCPreservesActiveHistories(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAge = 1 * time.Minute
	cfg.GCInterval = 10 * time.Millisecond

	p := NewProfiler(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	snap := &model.Snapshot{
		Timestamp: time.Now().Unix(),
		MonitoredProcesses: []model.ProcessMetrics{
			{PID: 555, RSSBytes: 100 * 1024 * 1024},
		},
	}
	p.Ingest(snap)

	time.Sleep(50 * time.Millisecond)

	key := ResourceKey{PID: 555, GPUUID: ""}
	p.mu.RLock()
	_, ok := p.histories[key]
	p.mu.RUnlock()
	if !ok {
		t.Error("expected active history to be preserved by GC")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.WindowSize != 60 {
		t.Errorf("expected WindowSize 60, got %d", cfg.WindowSize)
	}
	if cfg.MaxAge != 5*time.Minute {
		t.Errorf("expected MaxAge 5m, got %v", cfg.MaxAge)
	}
	if cfg.IdleVRAMThreshold != 4*1024*1024*1024 {
		t.Errorf("expected IdleVRAMThreshold 4GB, got %d", cfg.IdleVRAMThreshold)
	}
	if cfg.IdleSMThresholdPct != 2.0 {
		t.Errorf("expected IdleSMThresholdPct 2.0, got %.2f", cfg.IdleSMThresholdPct)
	}
	if cfg.IdleDuration != 3*time.Minute {
		t.Errorf("expected IdleDuration 3m, got %v", cfg.IdleDuration)
	}
	if cfg.LeakSlopeThreshold != 1024*1024 {
		t.Errorf("expected LeakSlopeThreshold 1MB/s, got %.0f", cfg.LeakSlopeThreshold)
	}
	if cfg.LeakRSquaredThreshold != 0.90 {
		t.Errorf("expected LeakRSquaredThreshold 0.90, got %.2f", cfg.LeakRSquaredThreshold)
	}
}

func TestProfiler_IngestNilSnapshot(t *testing.T) {
	p := NewProfiler(DefaultConfig(), nil)
	p.Ingest(nil)
	p.mu.RLock()
	count := len(p.histories)
	p.mu.RUnlock()
	if count != 0 {
		t.Errorf("expected 0 histories after nil ingest, got %d", count)
	}
}

func TestResourceHistory_PointsCopy(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 1, GPUUID: "gpu"}, 10, 5*time.Minute)
	now := time.Now().Unix()

	rh.Append(TimeSeriesPoint{Timestamp: now, VRAMUsed: 100})
	rh.Append(TimeSeriesPoint{Timestamp: now + 1, VRAMUsed: 200})

	copied := rh.Points()
	copied[0].VRAMUsed = 999

	if rh.points[0].VRAMUsed != 100 {
		t.Errorf("expected original unchanged (100), got %d", rh.points[0].VRAMUsed)
	}
}

func TestResourceHistory_EmptyPoints(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 1, GPUUID: "gpu"}, 10, 5*time.Minute)
	pts := rh.Points()
	if len(pts) != 0 {
		t.Errorf("expected empty points, got %d", len(pts))
	}
}

func TestResourceHistory_NewestTimestamp(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 1, GPUUID: "gpu"}, 10, 5*time.Minute)
	now := time.Now().Unix()

	rh.Append(TimeSeriesPoint{Timestamp: now})
	rh.Append(TimeSeriesPoint{Timestamp: now + 5})
	rh.Append(TimeSeriesPoint{Timestamp: now + 3})

	nt := rh.NewestTimestamp()
	if nt != now+5 {
		t.Errorf("expected newest timestamp %d, got %d", now+5, nt)
	}
}

func TestResourceHistory_EmptyNewest(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 1, GPUUID: "gpu"}, 10, 5*time.Minute)
	if nt := rh.NewestTimestamp(); nt != 0 {
		t.Errorf("expected 0 for empty, got %d", nt)
	}
}

func TestResourceHistory_EmptyLatestVRAM(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 1, GPUUID: "gpu"}, 10, 5*time.Minute)
	if v := rh.LatestVRAMUsed(); v != 0 {
		t.Errorf("expected 0 for empty, got %d", v)
	}
}

func TestResourceHistory_EmptyLatestCPU(t *testing.T) {
	rh := newResourceHistory(ResourceKey{PID: 1, GPUUID: "gpu"}, 10, 5*time.Minute)
	if c := rh.LatestCPUUsage(); c != 0 {
		t.Errorf("expected 0 for empty, got %.2f", c)
	}
}
