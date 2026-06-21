package profiler

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/model"
)

type Profiler struct {
	cfg       Config
	mu        sync.RWMutex
	histories map[ResourceKey]*ResourceHistory
	alerts    chan AnomalyAlert
	wg        sync.WaitGroup
	cancel    context.CancelFunc
	logf      func(string, ...any)
}

func NewProfiler(cfg Config, logf func(string, ...any)) *Profiler {
	return &Profiler{
		cfg:       cfg,
		histories: make(map[ResourceKey]*ResourceHistory),
		alerts:    make(chan AnomalyAlert, 64),
		logf:      logf,
	}
}

func (p *Profiler) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.wg.Add(2)
	go p.analysisLoop(ctx)
	go p.gcLoop(ctx)
}

func (p *Profiler) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}

func (p *Profiler) AlertCh() <-chan AnomalyAlert {
	return p.alerts
}

func (p *Profiler) Ingest(snap *model.Snapshot) {
	if snap == nil {
		return
	}
	now := snap.Timestamp
	if now == 0 {
		now = time.Now().Unix()
	}

	procMap := make(map[int]model.ProcessMetrics, len(snap.MonitoredProcesses))
	for _, pm := range snap.MonitoredProcesses {
		procMap[pm.PID] = pm
	}

	for _, gpu := range snap.GPUDevices {
		for _, gp := range gpu.RunningProcesses {
			pid := int64(gp.PID)
			key := ResourceKey{PID: pid, GPUUID: gpu.UUID}
			rh := p.getOrCreate(key)

			rh.mu.Lock()
			rh.Append(TimeSeriesPoint{
				Timestamp:   now,
				SMUtilPct:   gpu.SMUtilizationPct,
				VRAMUsed:    gp.VRAMUsedBytes,
				VRAMTotal:   gpu.MemoryTotalBytes,
				RSSBytes:    0,
				CPUUsagePct: 0,
			})

			pm, tracked := procMap[int(gp.PID)]
			if tracked {
				rh.points[len(rh.points)-1].RSSBytes = pm.RSSBytes
				rh.points[len(rh.points)-1].CPUUsagePct = pm.CPUUsagePct
			}
			rh.mu.Unlock()

			p.checkGPUIdle(pid, gpu.UUID, rh)
		}
	}

	for _, pm := range snap.MonitoredProcesses {
		pid := int64(pm.PID)
		key := ResourceKey{PID: pid, GPUUID: ""}
		rh := p.getOrCreate(key)

		rh.mu.Lock()
		rh.Append(TimeSeriesPoint{
			Timestamp:   now,
			SMUtilPct:   0,
			VRAMUsed:    0,
			VRAMTotal:   0,
			RSSBytes:    pm.RSSBytes,
			CPUUsagePct: pm.CPUUsagePct,
		})
		rh.mu.Unlock()
	}
}

func (p *Profiler) getOrCreate(key ResourceKey) *ResourceHistory {
	p.mu.RLock()
	rh, ok := p.histories[key]
	p.mu.RUnlock()
	if ok {
		return rh
	}

	p.mu.Lock()
	rh, ok = p.histories[key]
	if ok {
		p.mu.Unlock()
		return rh
	}

	rh = newResourceHistory(key, p.cfg.WindowSize, p.cfg.MaxAge)
	p.histories[key] = rh
	p.mu.Unlock()
	return rh
}

func gpuEfficiency(smUtilPct, vramUsed, vramTotal float64) float64 {
	vramPct := (vramUsed / math.Max(1, vramTotal)) * 100.0
	denom := math.Max(1.0, vramPct)
	return smUtilPct / denom
}

func (p *Profiler) checkGPUIdle(pid int64, gpuUUID string, rh *ResourceHistory) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	if rh.Len() < 2 {
		return
	}

	newest := rh.points[rh.Len()-1]
	if newest.VRAMUsed < p.cfg.IdleVRAMThreshold {
		return
	}

	avgSM := rh.RollingAvgSMUtil()
	if avgSM >= p.cfg.IdleSMThresholdPct {
		return
	}

	duration := float64(newest.Timestamp - rh.OldestTimestamp())
	if duration < p.cfg.IdleDuration.Seconds() {
		return
	}

	if !rh.lastAlertAt.IsZero() && time.Since(rh.lastAlertAt) < p.cfg.AlertCooldown {
		return
	}
	rh.lastAlertAt = time.Now()

	eta := gpuEfficiency(newest.SMUtilPct, float64(newest.VRAMUsed), float64(newest.VRAMTotal))

	alert := AnomalyAlert{
		Timestamp: newest.Timestamp,
		Alert: AnomalyAlertBody{
			TargetPID:   pid,
			GPUUID:      gpuUUID,
			AnomalyType: AnomalyIdleGPU,
			Severity:    SeverityCritical,
			MetricsSummary: MetricsSummary{
				DurationSeconds:                duration,
				CurrentVRAMAllocationBytes:     newest.VRAMUsed,
				RollingAvgSMUtilizationPct:     avgSM,
				CalculatedEfficiencyCoefficient: eta,
			},
			MemoryTrend: MemoryTrendAnalysis{
				OLSSlopeBytesPerSec: 0,
				LinearityRSquared:   0,
			},
		},
	}

	select {
	case p.alerts <- alert:
	default:
		p.logf("profiler: alert channel full, dropping idle GPU alert for PID %d GPU %s", pid, gpuUUID)
	}
}

func (p *Profiler) analysisLoop(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.cfg.AnalysisInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runMemoryLeakAnalysis()
		}
	}
}

func (p *Profiler) runMemoryLeakAnalysis() {
	p.mu.RLock()
	keys := make([]ResourceKey, 0, len(p.histories))
	for k := range p.histories {
		keys = append(keys, k)
	}
	p.mu.RUnlock()

	for _, key := range keys {
		if key.GPUUID != "" {
			continue
		}

		p.mu.RLock()
		rh, ok := p.histories[key]
		p.mu.RUnlock()
		if !ok {
			continue
		}

		p.analyzeMemoryLeak(key.PID, rh)
	}
}

func (p *Profiler) analyzeMemoryLeak(pid int64, rh *ResourceHistory) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	n := rh.Len()
	if n < 10 {
		return
	}

	points := rh.points
	x := make([]float64, n)
	y := make([]float64, n)
	firstTS := float64(points[0].Timestamp)

	for i, pt := range points {
		x[i] = float64(pt.Timestamp) - firstTS
		y[i] = float64(pt.RSSBytes)
	}

	slope, _, rSquared := OLSSlope(x, y)

	if slope < p.cfg.LeakSlopeThreshold {
		return
	}

	if rSquared < p.cfg.LeakRSquaredThreshold {
		return
	}

	cpuIncreasing := false
	if n >= 3 {
		half := n / 2
		var firstHalfCPU, secondHalfCPU float64
		for i := 0; i < half; i++ {
			firstHalfCPU += points[i].CPUUsagePct
		}
		for i := half; i < n; i++ {
			secondHalfCPU += points[i].CPUUsagePct
		}
		firstHalfCPU /= float64(half)
		secondHalfCPU /= float64(n - half)
		if secondHalfCPU > firstHalfCPU*1.1 {
			cpuIncreasing = true
		}
	}

	if cpuIncreasing {
		return
	}

	if !rh.lastAlertAt.IsZero() && time.Since(rh.lastAlertAt) < p.cfg.AlertCooldown {
		return
	}
	rh.lastAlertAt = time.Now()

	newest := points[n-1]
	duration := float64(newest.Timestamp - points[0].Timestamp)

	alert := AnomalyAlert{
		Timestamp: newest.Timestamp,
		Alert: AnomalyAlertBody{
			TargetPID:   pid,
			GPUUID:      "",
			AnomalyType: AnomalyMemoryLeak,
			Severity:    SeverityWarning,
			MetricsSummary: MetricsSummary{
				DurationSeconds:                duration,
				CurrentVRAMAllocationBytes:     0,
				RollingAvgSMUtilizationPct:     0,
				CalculatedEfficiencyCoefficient: 0,
			},
			MemoryTrend: MemoryTrendAnalysis{
				OLSSlopeBytesPerSec: slope,
				LinearityRSquared:   rSquared,
			},
		},
	}

	select {
	case p.alerts <- alert:
	default:
		p.logf("profiler: alert channel full, dropping memory leak alert for PID %d", pid)
	}
}

func (p *Profiler) gcLoop(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.cfg.GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.runGC()
			return
		case <-ticker.C:
			p.runGC()
		}
	}
}

func (p *Profiler) runGC() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for key, rh := range p.histories {
		rh.mu.Lock()
		stale := time.Since(rh.lastAccess) > p.cfg.MaxAge
		rh.mu.Unlock()
		if stale {
			delete(p.histories, key)
			p.logf("profiler: GC removed key PID=%d GPU=%s", key.PID, key.GPUUID)
		}
	}
}
