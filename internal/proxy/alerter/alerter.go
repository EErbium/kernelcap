package alerter

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"sync"

	"github.com/anomalyco/ai-compute-profiler/internal/analytics/detector"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/pipeline"
	"github.com/anomalyco/ai-compute-profiler/internal/analytics/profiler"
)

type AlertMultiplexer struct {
	cfg        Config
	detectorCh <-chan detector.Alert
	profilerCh <-chan profiler.AnomalyAlert
	ringBuf    *pipeline.RingBuffer
	console    io.Writer

	dedupMu sync.Mutex
	dedup   map[uint64]*DedupEntry

	internal chan ConsolidatedAlert

	subMu   sync.RWMutex
	subs    []chan<- ConsolidatedAlert

	logf   func(string, ...any)
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func NewAlertMultiplexer(cfg Config, ringBuf *pipeline.RingBuffer, logf func(string, ...any)) *AlertMultiplexer {
	if cfg.InternalBufferSize <= 0 {
		cfg.InternalBufferSize = 256
	}
	if cfg.FanOutCount <= 0 {
		cfg.FanOutCount = 2
	}
	if cfg.EventIDPrefix == "" {
		cfg.EventIDPrefix = "evt_"
	}
	return &AlertMultiplexer{
		cfg:      cfg,
		ringBuf:  ringBuf,
		console:  os.Stdout,
		dedup:    make(map[uint64]*DedupEntry),
		internal: make(chan ConsolidatedAlert, cfg.InternalBufferSize),
		logf:     logf,
	}
}

func (am *AlertMultiplexer) SetConsoleOutput(w io.Writer) {
	if w != nil {
		am.console = w
	}
}

func (am *AlertMultiplexer) AttachDetector(ch <-chan detector.Alert) {
	am.detectorCh = ch
}

func (am *AlertMultiplexer) AttachProfiler(ch <-chan profiler.AnomalyAlert) {
	am.profilerCh = ch
}

func (am *AlertMultiplexer) Subscribe() <-chan ConsolidatedAlert {
	ch := make(chan ConsolidatedAlert, am.cfg.InternalBufferSize)
	am.subMu.Lock()
	am.subs = append(am.subs, ch)
	am.subMu.Unlock()
	return ch
}

func (am *AlertMultiplexer) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	am.cancel = cancel

	if am.detectorCh == nil {
		am.detectorCh = make(chan detector.Alert)
	}
	if am.profilerCh == nil {
		am.profilerCh = make(chan profiler.AnomalyAlert)
	}

	am.wg.Add(1 + am.cfg.FanOutCount)
	go am.multiplexLoop(ctx)
	for i := 0; i < am.cfg.FanOutCount; i++ {
		go am.fanOutLoop(ctx)
	}
}

func (am *AlertMultiplexer) Stop() {
	if am.cancel != nil {
		am.cancel()
	}
	am.wg.Wait()
}

func (am *AlertMultiplexer) multiplexLoop(ctx context.Context) {
	defer am.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case a := <-am.detectorCh:
			alert := am.fromDetectorAlert(a)
			am.route(ctx, alert)
		case a := <-am.profilerCh:
			alert := am.fromProfilerAlert(a)
			am.route(ctx, alert)
		}
	}
}

func (am *AlertMultiplexer) route(ctx context.Context, alert ConsolidatedAlert) {
	key := dedupKey(alert.Payload.TargetPID, alert.Payload.AnomalyType, alert.Payload.GPUUID)

	am.dedupMu.Lock()
	entry, exists := am.dedup[key]
	now := alert.Timestamp
	windowSecs := int64(am.cfg.SuppressionWindow.Seconds())

	if exists && now-entry.FirstSeen < windowSecs {
		entry.OccurrenceCount++
		alert.Metadata = PropagationMetadata{
			IsDeduplicated:           true,
			CumulativeOccurrences:    entry.OccurrenceCount,
			SuppressionWindowSeconds: int(am.cfg.SuppressionWindow.Seconds()),
		}
		am.dedupMu.Unlock()
		return
	}

	am.dedup[key] = &DedupEntry{
		FirstSeen:       now,
		OccurrenceCount: 1,
	}
	am.dedupMu.Unlock()

	alert.Metadata = PropagationMetadata{
		IsDeduplicated:           false,
		CumulativeOccurrences:    1,
		SuppressionWindowSeconds: int(am.cfg.SuppressionWindow.Seconds()),
	}

	alert.EventID = generateEventID(am.cfg.EventIDPrefix)
	alert.Timestamp = now

	select {
	case am.internal <- alert:
	default:
		am.logf("alerter: internal buffer full, dropping alert for PID %d type %s",
			alert.Payload.TargetPID, alert.Payload.AnomalyType)
	}
}

func (am *AlertMultiplexer) fanOutLoop(ctx context.Context) {
	defer am.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case alert := <-am.internal:
			am.writeConsole(alert)
			am.writeRingBuf(alert)
			am.dispatchToSubscribers(alert)
		}
	}
}

func (am *AlertMultiplexer) dispatchToSubscribers(alert ConsolidatedAlert) {
	am.subMu.RLock()
	defer am.subMu.RUnlock()
	for _, ch := range am.subs {
		select {
		case ch <- alert:
		default:
		}
	}
}

func (am *AlertMultiplexer) writeConsole(alert ConsolidatedAlert) {
	if err := json.NewEncoder(am.console).Encode(alert); err != nil {
		am.logf("alerter: encode console alert: %v", err)
	}
}

func (am *AlertMultiplexer) writeRingBuf(alert ConsolidatedAlert) {
	if am.ringBuf == nil {
		return
	}
	data, err := json.Marshal(alert)
	if err != nil {
		am.logf("alerter: marshal ringbuf alert: %v", err)
		return
	}
	am.ringBuf.Push(data)
}

func dedupKey(pid int64, anomalyType, gpuUUID string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(fmt.Sprintf("%d|%s|%s", pid, anomalyType, gpuUUID)))
	return h.Sum64()
}

func (am *AlertMultiplexer) fromDetectorAlert(a detector.Alert) ConsolidatedAlert {
	return ConsolidatedAlert{
		EventID:   "",
		Timestamp: a.Timestamp,
		Payload: AlertPayload{
			TargetPID:   a.Summary.TargetPID,
			GPUUID:      "",
			AnomalyType: string(a.Summary.LoopType),
			Severity:    "CRITICAL",
			Telemetry:   TelemetrySnapshot{},
		},
	}
}

func (am *AlertMultiplexer) fromProfilerAlert(a profiler.AnomalyAlert) ConsolidatedAlert {
	payload := AlertPayload{
		TargetPID:   a.Alert.TargetPID,
		GPUUID:      a.Alert.GPUUID,
		AnomalyType: string(a.Alert.AnomalyType),
		Severity:    string(a.Alert.Severity),
		Telemetry: TelemetrySnapshot{
			SMUtilizationPct: a.Alert.MetricsSummary.RollingAvgSMUtilizationPct,
			VRAMUsedBytes:    a.Alert.MetricsSummary.CurrentVRAMAllocationBytes,
		},
	}
	return ConsolidatedAlert{
		EventID:   "",
		Timestamp: a.Timestamp,
		Payload:   payload,
	}
}
