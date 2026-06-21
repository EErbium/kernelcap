package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/cgroup"
	"github.com/anomalyco/ai-compute-profiler/pkg/config"
	"github.com/anomalyco/ai-compute-profiler/pkg/ebpf"
	"github.com/anomalyco/ai-compute-profiler/pkg/gpu"
	"github.com/anomalyco/ai-compute-profiler/pkg/model"
	"github.com/anomalyco/ai-compute-profiler/pkg/procfs"
)

const (
	clkTck       = 100.0
	nanosecPerSec = 1e9
)

type Collector struct {
	cfg        *config.Config
	cpucalc    *procfs.CPUUtilCalculator
	cg         *cgroup.Resolver
	tracker    *ebpf.Tracker
	gpuSampler *gpu.GPUSampler

	deltaMu     sync.Mutex
	procDeltas  map[int]*procfs.ProcDelta
	prevCPUPct  map[int]float64

	httpServer   *http.Server
	lastSnapshot []byte
	snapshotMu   sync.RWMutex

	SnapshotOutput chan<- []byte

	logf func(format string, args ...any)

	startedAt time.Time
	pollCount int64
}

func New(cfg *config.Config) *Collector {
	logf := log.Printf
	if !cfg.Verbose {
		logf = func(format string, args ...any) {}
	}
	return &Collector{
		cfg:        cfg,
		cpucalc:    procfs.NewCPUUtilCalculator(),
		cg:         cgroup.NewResolver(cfg.ProcRoot),
		tracker:    ebpf.NewTracker(logf),
		gpuSampler: gpu.NewGPUSampler(cfg, logf),
		procDeltas: make(map[int]*procfs.ProcDelta),
		prevCPUPct: make(map[int]float64),
		logf:       logf,
	}
}

func (c *Collector) Run(ctx context.Context) error {
	c.startedAt = time.Now()

	if c.cfg.EnableBPF {
		if err := c.tracker.Start(); err != nil {
			c.logf("collector: ebpf tracker start: %v (continuing with /proc-only)", err)
		}
	}
	if c.tracker.Fallback() {
		c.logf("collector: running in /proc-only mode (short-lived processes may be missed)")
	} else {
		c.logf("collector: running with eBPF process tracking")
	}

	if err := c.gpuSampler.Run(); err != nil {
		c.logf("collector: gpu sampler not available: %v", err)
	}

	c.startHTTPServer()

	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()

	if c.cfg.Verbose {
		c.logf("collector: started, poll interval %v", c.cfg.PollInterval)
		c.collectOnce()
	}

	for {
		select {
		case <-ctx.Done():
			c.logf("collector: shutting down")
			if c.httpServer != nil {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				c.httpServer.Shutdown(shutdownCtx)
			}
			c.tracker.Close()
			c.gpuSampler.Close()
			return ctx.Err()
		case <-ticker.C:
			c.collectOnce()
		}
	}
}

func (c *Collector) collectOnce() {
	c.pollCount++
	snap := &model.Snapshot{}
	procRoot := c.cfg.ProcRoot

	totalCPUPct, err := c.cpucalc.ReadAndCompute(procRoot)
	if err != nil {
		c.logf("collector: read cpu stats: %v", err)
	}

	memInfo, err := procfs.ReadMemoryInfo(procRoot)
	if err != nil {
		c.logf("collector: read memory info: %v", err)
	}

	snap.HostMetrics.CPUUtilizationPct = math.Round(totalCPUPct*100) / 100
	if memInfo != nil {
		snap.HostMetrics.MemoryTotalBytes = memInfo.TotalBytes
		snap.HostMetrics.MemoryUsedBytes = memInfo.UsedBytes
	}

	c.collectProcessMetrics(snap, procRoot)

	drainEvents := c.tracker.Drain(256)
	if len(drainEvents) > 0 && c.cfg.Verbose {
		c.logf("collector: drained %d ebpf events", len(drainEvents))
	}

	c.gpuSampler.Sample()
	snap.GPUDevices = c.gpuSampler.LastGPUDevices()

	snap.MonitoredProcesses = c.sortByCPU(snap.MonitoredProcesses)
	if len(snap.MonitoredProcesses) > 1000 {
		snap.MonitoredProcesses = snap.MonitoredProcesses[:1000]
	}

	data, err := snap.JSON()
	if err != nil {
		c.logf("collector: marshal error: %v", err)
		return
	}
	data = append(data, '\n')

	c.snapshotMu.Lock()
	c.lastSnapshot = data
	c.snapshotMu.Unlock()

	if c.SnapshotOutput != nil {
		select {
		case c.SnapshotOutput <- data:
		default:
		}
	}

	if c.cfg.Verbose {
		os.Stdout.Write(data)
	}
}

func (c *Collector) collectProcessMetrics(snap *model.Snapshot, procRoot string) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		c.logf("collector: read /proc: %v", err)
		return
	}

	seenPIDs := make(map[int]bool, len(entries)/2)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		seenPIDs[pid] = true

		pm, err := procfs.ReadProcStat(pid, procRoot)
		if err != nil {
			continue
		}

		containerID := c.cg.Resolve(pid)

		cpuPct := c.computeProcessCPU(pid, pm)

		snap.MonitoredProcesses = append(snap.MonitoredProcesses, model.ProcessMetrics{
			PID:         pid,
			ContainerID: containerID,
			ProcessName: pm.Comm,
			CPUUsagePct: cpuPct,
			RSSBytes:    pm.RSS,
			VSizeBytes:  pm.VSize,
		})
	}

	// Prune deltas and cgroup cache for dead processes.
	c.deltaMu.Lock()
	for pid := range c.procDeltas {
		if !seenPIDs[pid] {
			delete(c.procDeltas, pid)
			delete(c.prevCPUPct, pid)
			c.cg.Evict(pid)
		}
	}
	c.deltaMu.Unlock()
}

func (c *Collector) computeProcessCPU(pid int, pm *procfs.ProcMetrics) float64 {
	c.deltaMu.Lock()
	defer c.deltaMu.Unlock()

	prev, ok := c.procDeltas[pid]
	if !ok {
		c.procDeltas[pid] = &procfs.ProcDelta{
			PrevUTime: pm.UTime,
			PrevSTime: pm.STime,
		}
		return 0
	}

	utimeDelta := pm.UTime - prev.PrevUTime
	stimeDelta := pm.STime - prev.PrevSTime

	prev.PrevUTime = pm.UTime
	prev.PrevSTime = pm.STime

	intervalSec := c.cfg.PollInterval.Seconds()
	if intervalSec <= 0 {
		return 0
	}

	totalTicks := float64(utimeDelta + stimeDelta)
	cpuPct := totalTicks / (intervalSec * clkTck) * 100.0
	if cpuPct < 0 {
		cpuPct = 0
	}
	if cpuPct > 100*c.cfg.PollInterval.Seconds() {
		cpuPct = 0
	}
	cpuPct = math.Round(cpuPct*100) / 100

	c.prevCPUPct[pid] = cpuPct
	return cpuPct
}

func (c *Collector) sortByCPU(procs []model.ProcessMetrics) []model.ProcessMetrics {
	if len(procs) <= 1 {
		return procs
	}
	// Simple insertion sort for near-sorted data.
	for i := 1; i < len(procs); i++ {
		key := procs[i]
		j := i - 1
		for j >= 0 && procs[j].CPUUsagePct < key.CPUUsagePct {
			procs[j+1] = procs[j]
			j--
		}
		procs[j+1] = key
	}
	return procs
}

func (c *Collector) startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", c.handleMetrics)
	mux.HandleFunc("/health", c.handleHealth)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "AI Compute Profiler Agent\n\nEndpoints:\n  /metrics  - latest telemetry snapshot (JSON)\n  /health   - health check\n")
			return
		}
		http.NotFound(w, r)
	})

	c.httpServer = &http.Server{
		Addr:    c.cfg.HTTPListenAddr,
		Handler: mux,
	}

	go func() {
		c.logf("collector: HTTP server listening on %s", c.cfg.HTTPListenAddr)
		if err := c.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			c.logf("collector: HTTP server error: %v", err)
		}
	}()
}

func (c *Collector) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	c.snapshotMu.RLock()
	data := c.lastSnapshot
	c.snapshotMu.RUnlock()
	if data == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":"no data collected yet"}`)
		return
	}
	w.Write(data)
}

func (c *Collector) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := "ok"
	uptime := time.Since(c.startedAt).String()
	pollInterval := c.cfg.PollInterval.String()
	ebpfMode := "enabled"
	if c.tracker.Fallback() {
		ebpfMode = "fallback (/proc-only)"
	}

	gpuMode := "not available"
	if c.gpuSampler.Available() {
		gpuMode = "active"
	}
	resp := struct {
		Status       string `json:"status"`
		Uptime       string `json:"uptime"`
		PollInterval string `json:"poll_interval"`
		Polls        int64  `json:"polls_completed"`
		EBPFMode     string `json:"ebpf_mode"`
		GPUMode      string `json:"gpu_mode"`
		ProcRoot     string `json:"proc_root"`
	}{
		Status:       status,
		Uptime:       uptime,
		PollInterval: pollInterval,
		Polls:        c.pollCount,
		EBPFMode:     ebpfMode,
		GPUMode:      gpuMode,
		ProcRoot:     c.cfg.ProcRoot,
	}

	json.NewEncoder(w).Encode(resp)
}

// LastSnapshot returns the most recent telemetry snapshot.
func (c *Collector) LastSnapshot() []byte {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()
	return c.lastSnapshot
}

// Stats returns basic collector statistics.
func (c *Collector) Stats() string {
	ebpfStatus := "fallback (/proc-only)"
	if !c.tracker.Fallback() {
		ebpfStatus = "active"
	}
	gpuStatus := "not available"
	if c.gpuSampler.Available() {
		gpuStatus = "active"
	}
	return fmt.Sprintf("polls=%d uptime=%s ebpf=%s gpu=%s", c.pollCount, time.Since(c.startedAt).Round(time.Second), ebpfStatus, gpuStatus)
}
