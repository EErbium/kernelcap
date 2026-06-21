package engine

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/alerter"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/collector"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/config"
	"github.com/anomalyco/ai-compute-profiler/internal/analytics/detector"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/mitigator"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/model"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/pipeline"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/policy"
	"github.com/anomalyco/ai-compute-profiler/internal/analytics/profiler"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/proxy"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/rollback"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/router"
)

type ProfilerEngine struct {
	cfg       *config.Config
	startedAt time.Time
	logf      func(string, ...any)

	collector *collector.Collector
	pipeline  *pipeline.Pipeline
	proxySrv  *proxy.Proxy

	detector  *detector.Detector
	profiler  *profiler.Profiler
	alerter   *alerter.AlertMultiplexer
	mitigator *mitigator.Mitigator
	router    *router.Router
	policy    *policy.Engine
	rollback  *rollback.Controller

	mitLastEventID      string
	routerLastEvent     string
	policySweeperActive bool
	rollbackLastSync    int64

	metricsPtr atomic.Pointer[UnifiedMetrics]
	apiServer  *apiServer

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func New(cfg *config.Config, logf func(string, ...any)) *ProfilerEngine {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	e := &ProfilerEngine{
		cfg:       cfg,
		startedAt: time.Now(),
		logf:      logf,
	}

	e.collector = collector.New(cfg)
	e.pipeline = pipeline.New(cfg.AgentID, cfg.AuthToken, cfg.UpstreamEndpoint, cfg.PollInterval, cfg.RingBufferMaxMB, logf)

	e.collector.SnapshotOutput = e.pipeline.SnapshotCh

	if cfg.ProxyEnabled {
		proxyCfg := proxy.ProxyConfig{
			ListenAddr:  cfg.ProxyListenAddr,
			TokenOutput: e.pipeline.TokenCh,
		}
		if cfg.ProxyLogFilePath != "" {
			f, err := os.OpenFile(cfg.ProxyLogFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				logf("engine: open proxy log: %v", err)
			} else {
				proxyCfg.LogWriter = f
			}
		}
		if e.router != nil {
			proxyCfg.Router = e.router
		}
		if cfg.ProcRoot != "" {
			proc := proxy.NewProcResolver()
			proc.Start()
			proxyCfg.ProcResolver = proc
		}
		e.proxySrv = proxy.NewProxy(proxyCfg)
	}

	if cfg.DetectorEnabled {
		e.detector = detector.NewDetector(detector.DefaultConfig(), logf)
	}
	if cfg.ProfilerEnabled {
		e.profiler = profiler.NewProfiler(profiler.DefaultConfig(), logf)
	}

	e.alerter = alerter.NewAlertMultiplexer(alerter.DefaultConfig(), e.pipeline.RingBuf, logf)
	if e.detector != nil {
		e.alerter.AttachDetector(e.detector.AlertCh())
	}
	if e.profiler != nil {
		e.alerter.AttachProfiler(e.profiler.AlertCh())
	}

	policyCfg := policy.DefaultConfig()
	policyCfg.MaxActionsPerPID = cfg.PolicyMaxActionsPerPID
	policyCfg.VelocityWindow = time.Duration(cfg.PolicyVelocityWindowMin) * time.Minute
	policyCfg.CooldownSeconds = cfg.PolicyCooldownSeconds
	policyCfg.LedgerMaxEntries = cfg.PolicyLedgerMaxEntries
	policyCfg.PIDThreshold = cfg.PolicyPidThreshold
	if cfg.MitigationWhitelist != nil {
		policyCfg.WhitelistNames = cfg.MitigationWhitelist
	}
	e.policy = policy.NewEngine(policyCfg, logf)

	if cfg.MitigationEnabled {
		whitelist := cfg.MitigationWhitelist
		if whitelist == nil {
			whitelist = []string{}
		}
		e.mitigator = mitigator.New(mitigator.Config{
			DockerSocketPath: cfg.DockerSocketPath,
			DockerTimeout:    cfg.DockerAPITimeout,
			CgroupRoot:       cfg.SysRoot + "/fs/cgroup",
			ProcRoot:         cfg.ProcRoot,
			WhitelistNames:   whitelist,
			BufferSize:       128,
			Policy:           e.policy,
		}, logf)
	}

	if cfg.RouterEnabled {
		e.router = router.New(router.Config{
			Enabled:               true,
			MaxMessagesBeforeChop: cfg.RouterTokenCap,
			KeepRecentMessages:    cfg.RouterKeepRecent,
			FallbackEndpoint:      cfg.RouterFallbackEndpoint,
			FallbackModel:         cfg.RouterFallbackModel,
			FallbackAuthToken:     cfg.RouterFallbackAuthToken,
			CoolingOffDuration:    time.Duration(cfg.RouterCoolingOffSeconds) * time.Second,
			BufferSize:            128,
			Policy:                e.policy,
		}, logf)
	}

	rbCfg := rollback.DefaultConfig()
	rbCfg.ProcRoot = cfg.ProcRoot
	if e.mitigator != nil && e.policy != nil {
		var rreg *router.Registry
		if e.router != nil {
			rreg = e.router.Registry()
		}
		e.rollback = rollback.New(rbCfg, e.mitigator, rreg, e.policy, logf)
	}

	e.apiServer = newAPIServer(cfg.DashboardAddr, &e.metricsPtr, logf)

	return e
}

func (e *ProfilerEngine) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	e.pipeline.Start(ctx)
	e.pipeline.SetUpstreamEndpoint(e.cfg.UpstreamEndpoint)

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if err := e.collector.Run(ctx); err != nil && err != context.Canceled {
			e.logf("engine: collector error: %v", err)
		}
	}()

	e.wg.Add(1)
	go e.dataRouter(ctx)

	if e.detector != nil {
		e.detector.Start(ctx)
	}
	if e.profiler != nil {
		e.profiler.Start(ctx)
	}
	e.alerter.Start(ctx)

	if e.mitigator != nil {
		alertCh := e.alerter.Subscribe()
		e.mitigator.Start(ctx, alertCh)
		e.wg.Add(1)
		go e.mitigateEventLogger(ctx)
	}

	if e.policy != nil {
		e.policy.Start(ctx)
		e.wg.Add(1)
		go e.policyEventLogger(ctx)
	}

	if e.rollback != nil {
		e.rollback.Start(ctx)
		e.wg.Add(1)
		go e.rollbackEventLogger(ctx)
	}

	if e.router != nil {
		routerAlertCh := make(chan router.AlertTrigger, 64)
		e.router.Start(ctx, routerAlertCh)
		e.wg.Add(1)
		go e.routerAlertBridge(ctx, routerAlertCh)
		e.wg.Add(1)
		go e.routerEventLogger(ctx)
	}

	if e.proxySrv != nil {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			e.logf("engine: proxy listening on %s", e.cfg.ProxyListenAddr)
			if err := e.proxySrv.Start(); err != nil {
				e.logf("engine: proxy error: %v", err)
			}
		}()
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.apiServer.start(ctx)
	}()

		e.logf("engine: started (node=%s detector=%v profiler=%v proxy=%v dashboard=%s mitigation=%v router=%v)",
			e.cfg.AgentID, e.cfg.DetectorEnabled, e.cfg.ProfilerEnabled,
			e.cfg.ProxyEnabled, e.cfg.DashboardAddr, e.cfg.MitigationEnabled, e.cfg.RouterEnabled)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		e.logf("engine: received signal %v, shutting down...", sig)
	case <-ctx.Done():
		e.logf("engine: context cancelled, shutting down...")
	}

	signal.Stop(sigCh)
	e.Stop()
	return nil
}

func (e *ProfilerEngine) Stop() {
	metrics := e.loadMetrics()
	if metrics != nil {
		metrics.EngineStatus = "STOPPING"
		e.metricsPtr.Store(metrics)
	}

	if e.cancel != nil {
		e.cancel()
	}

	if e.proxySrv != nil {
		if err := e.proxySrv.Stop(); err != nil {
			e.logf("engine: proxy stop: %v", err)
		}
	}

	if e.detector != nil {
		e.detector.Stop()
	}
	if e.profiler != nil {
		e.profiler.Stop()
	}
	if e.rollback != nil {
		e.rollback.Stop()
	}
	if e.policy != nil {
		e.policy.Stop()
	}
	if e.mitigator != nil {
		e.mitigator.Stop()
	}
	if e.router != nil {
		e.router.Stop()
	}
	e.alerter.Stop()

	e.pipeline.Stop()
	e.apiServer.stop()

	e.flushRingBuffer()

	e.wg.Wait()
}

func (e *ProfilerEngine) flushRingBuffer() {
	entries := e.pipeline.RingBuf.Drain(10000)
	if len(entries) == 0 {
		return
	}
	e.logf("engine: flushing %d remaining ring buffer entries to stdout", len(entries))
	for _, data := range entries {
		os.Stdout.Write(data)
		os.Stdout.Write([]byte("\n"))
	}
}

func (e *ProfilerEngine) dataRouter(ctx context.Context) {
	defer e.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case data := <-e.pipeline.SnapshotCh:
			var snap model.Snapshot
			if err := json.Unmarshal(data, &snap); err != nil {
				e.logf("engine: unmarshal snapshot: %v", err)
				continue
			}

			if e.profiler != nil {
				e.profiler.Ingest(&snap)
			}

			if e.detector != nil {
				for _, pm := range snap.MonitoredProcesses {
					e.detector.Ingest(detector.DetectorEvent{
						PID:       int64(pm.PID),
						Timestamp: snap.Timestamp,
					})
				}
			}

			e.updateMetrics(&snap)
		}
	}
}

func (e *ProfilerEngine) updateMetrics(snap *model.Snapshot) {
	pidSet := make(map[int]bool)
	for _, pm := range snap.MonitoredProcesses {
		pidSet[pm.PID] = true
	}

	m := &UnifiedMetrics{
		EngineStatus:    "RUNNING",
		UptimeSeconds:   int64(time.Since(e.startedAt).Seconds()),
		LocalNodeID:     e.cfg.AgentID,
		ActivePIDsCount: len(pidSet),
		Anomalies:       []AnomalyEntry{},
		SelfCheck: SelfCheckMetrics{
			ProfilerMemoryRSSBytes: memUsageBytes(),
			ProfilerCPUUtilPct:     cpuUsagePct(),
		},
	}

	if e.mitigator != nil {
		m.Mitigations = &MitigationSummary{
			Enabled:      e.cfg.MitigationEnabled,
			ActiveCount:  len(e.mitigator.ActiveMitigations()),
			TotalActions: e.mitigator.MitigationCount(),
			LastEventID:  e.mitLastEventID,
		}
	}

	if e.router != nil {
		chopCount, fallbackCount := e.router.Registry().ActiveCount()
		m.Router = &RouterSummary{
			Enabled:          e.cfg.RouterEnabled,
			ActiveChops:      chopCount,
			ActiveFallbacks:  fallbackCount,
			TotalTokensSaved: e.router.Registry().TotalTokensSaved(),
		}
	}

	if e.policy != nil {
		m.Policy = &PolicySummary{
			ActivePendingCount: e.policy.ActiveCount(),
			TotalTracked:       e.policy.TotalTrackedCount(),
			SweeperActive:      e.policySweeperActive,
		}
	}

	if e.rollback != nil && e.mitigator != nil {
		m.Rollback = &RollbackSummary{
			ActiveMitigations: len(e.mitigator.ActiveMitigations()),
		}
	}

	e.metricsPtr.Store(m)
}

func (e *ProfilerEngine) mitigateEventLogger(ctx context.Context) {
	defer e.wg.Done()
	ch := e.mitigator.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			e.mitLastEventID = evt.MitigationEventID
			e.logf("mitigation: %s target_pid=%d mech=%s status=%s duration=%.2fms",
				evt.MitigationEventID, evt.Target.PID, evt.Action.Mechanism,
				evt.Action.Status, evt.Action.ExecutionDurationMs)
			data, err := json.Marshal(evt)
			if err != nil {
				e.logf("engine: marshal mitigation event: %v", err)
				continue
			}
			e.pipeline.RingBuf.Push(data)
		}
	}
}

func (e *ProfilerEngine) routerAlertBridge(ctx context.Context, out chan<- router.AlertTrigger) {
	defer e.wg.Done()
	defer close(out)

	alertCh := e.alerter.Subscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case a, ok := <-alertCh:
			if !ok {
				return
			}
			trigger := router.AlertTrigger{
				PID:         a.Payload.TargetPID,
				AnomalyType: a.Payload.AnomalyType,
			}
			select {
			case out <- trigger:
			default:
				e.logf("engine: router alert channel full, dropping pid=%d", trigger.PID)
			}
		}
	}
}

func (e *ProfilerEngine) routerEventLogger(ctx context.Context) {
	defer e.wg.Done()
	ch := e.router.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			e.routerLastEvent = evt.AppliedRemedy.Type
			e.logf("router: pid=%d remedy=%s overhead=%.2fms tokens_saved=%d",
				evt.InterceptedProcess.PID, evt.AppliedRemedy.Type,
				evt.ExecutionTelemetry.ProcessingOverheadMs,
				evt.AppliedRemedy.Details.TokensSavedByChopper)
			data, err := json.Marshal(evt)
			if err != nil {
				e.logf("engine: marshal router event: %v", err)
				continue
			}
			e.pipeline.RingBuf.Push(data)
		}
	}
}

func (e *ProfilerEngine) policyEventLogger(ctx context.Context) {
	defer e.wg.Done()
	ch := e.policy.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			e.logf("policy: pid=%d authorized=%v risk=%.3f rules=%d reason=%q",
				evt.MitigationTarget.TargetPID, evt.PolicyEvaluation.IsAuthorized,
				evt.MitigationTarget.RiskScoreAssigned,
				evt.PolicyEvaluation.EvaluatedRulesCount,
				evt.PolicyEvaluation.RejectionReason)
			data, err := json.Marshal(evt)
			if err != nil {
				e.logf("engine: marshal policy event: %v", err)
				continue
			}
			e.pipeline.RingBuf.Push(data)
		}
	}
}

func (e *ProfilerEngine) rollbackEventLogger(ctx context.Context) {
	defer e.wg.Done()
	ch := e.rollback.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			e.rollbackLastSync = evt.Timestamp
			e.logf("rollback: event=%s scanned=%d resolved=%d drift=%v",
				evt.SyncEventID, evt.ReconciliationSummary.ScannedActiveInterventions,
				evt.ReconciliationSummary.ActionsResolvedCount,
				evt.ReconciliationSummary.StateDriftDetected)
			data, err := json.Marshal(evt)
			if err != nil {
				e.logf("engine: marshal rollback event: %v", err)
				continue
			}
			e.pipeline.RingBuf.Push(data)
		}
	}
}

func (e *ProfilerEngine) loadMetrics() *UnifiedMetrics {
	ptr := e.metricsPtr.Load()
	if ptr == nil {
		return nil
	}
	return ptr
}

func memUsageBytes() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc
}

func cpuUsagePct() float64 {
	return float64(runtime.NumGoroutine()) * 0.001
}
