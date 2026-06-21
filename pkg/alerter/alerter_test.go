package alerter

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/detector"
	"github.com/anomalyco/ai-compute-profiler/pkg/pipeline"
	"github.com/anomalyco/ai-compute-profiler/pkg/profiler"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.SuppressionWindow != 30*time.Second {
		t.Errorf("expected SuppressionWindow 30s, got %v", cfg.SuppressionWindow)
	}
	if cfg.InternalBufferSize != 256 {
		t.Errorf("expected InternalBufferSize 256, got %d", cfg.InternalBufferSize)
	}
	if cfg.FanOutCount != 2 {
		t.Errorf("expected FanOutCount 2, got %d", cfg.FanOutCount)
	}
	if cfg.EventIDPrefix != "evt_" {
		t.Errorf("expected EventIDPrefix evt_, got %s", cfg.EventIDPrefix)
	}
}

func TestEventID_Format(t *testing.T) {
	id := generateEventID("evt_")
	if len(id) != 4+16 {
		t.Errorf("expected event ID len %d, got %d (%q)", 4+16, len(id), id)
	}
	if id[:4] != "evt_" {
		t.Errorf("expected prefix evt_, got %s", id[:4])
	}
}

func TestEventID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := generateEventID("")
		if seen[id] {
			t.Errorf("duplicate event ID: %s", id)
		}
		seen[id] = true
	}
}

func TestDedupKey_Consistency(t *testing.T) {
	k1 := dedupKey(41029, "IDLE_GPU_HOG", "GPU-abc")
	k2 := dedupKey(41029, "IDLE_GPU_HOG", "GPU-abc")
	if k1 != k2 {
		t.Errorf("expected identical keys for same inputs, got %016x vs %016x", k1, k2)
	}
}

func TestDedupKey_DifferentPID(t *testing.T) {
	k1 := dedupKey(41029, "IDLE_GPU_HOG", "GPU-abc")
	k2 := dedupKey(41030, "IDLE_GPU_HOG", "GPU-abc")
	if k1 == k2 {
		t.Error("expected different keys for different PIDs")
	}
}

func TestDedupKey_DifferentType(t *testing.T) {
	k1 := dedupKey(41029, "IDLE_GPU_HOG", "GPU-abc")
	k2 := dedupKey(41029, "HOST_MEMORY_LEAK", "GPU-abc")
	if k1 == k2 {
		t.Error("expected different keys for different anomaly types")
	}
}

func TestDedupKey_DifferentGPU(t *testing.T) {
	k1 := dedupKey(41029, "IDLE_GPU_HOG", "GPU-abc")
	k2 := dedupKey(41029, "IDLE_GPU_HOG", "GPU-xyz")
	if k1 == k2 {
		t.Error("expected different keys for different GPU UUIDs")
	}
}

func TestFromDetectorAlert(t *testing.T) {
	cfg := DefaultConfig()
	am := NewAlertMultiplexer(cfg, nil, nil)

	da := detector.Alert{
		Timestamp: 1000,
		Summary: detector.AlertSummary{
			TargetPID:    12345,
			IsDeadlocked: true,
			LoopType:     "SEMANTIC_REPETITION_LOOP",
		},
	}

	ca := am.fromDetectorAlert(da)
	if ca.Payload.TargetPID != 12345 {
		t.Errorf("expected PID 12345, got %d", ca.Payload.TargetPID)
	}
	if ca.Payload.AnomalyType != "SEMANTIC_REPETITION_LOOP" {
		t.Errorf("expected SEMANTIC_REPETITION_LOOP, got %s", ca.Payload.AnomalyType)
	}
	if ca.Payload.Severity != "CRITICAL" {
		t.Errorf("expected CRITICAL severity, got %s", ca.Payload.Severity)
	}
	if ca.Timestamp != 1000 {
		t.Errorf("expected timestamp 1000, got %d", ca.Timestamp)
	}
}

func TestFromProfilerAlert_IdleGPU(t *testing.T) {
	cfg := DefaultConfig()
	am := NewAlertMultiplexer(cfg, nil, nil)

	pa := profiler.AnomalyAlert{
		Timestamp: 2000,
		Alert: profiler.AnomalyAlertBody{
			TargetPID:   67890,
			GPUUID:      "GPU-deadbeef",
			AnomalyType: profiler.AnomalyIdleGPU,
			Severity:    profiler.SeverityCritical,
			MetricsSummary: profiler.MetricsSummary{
				RollingAvgSMUtilizationPct:   0.45,
				CurrentVRAMAllocationBytes:   42949672960,
			},
		},
	}

	ca := am.fromProfilerAlert(pa)
	if ca.Payload.TargetPID != 67890 {
		t.Errorf("expected PID 67890, got %d", ca.Payload.TargetPID)
	}
	if ca.Payload.AnomalyType != string(profiler.AnomalyIdleGPU) {
		t.Errorf("expected IDLE_GPU_HOG, got %s", ca.Payload.AnomalyType)
	}
	if ca.Payload.Severity != string(profiler.SeverityCritical) {
		t.Errorf("expected CRITICAL, got %s", ca.Payload.Severity)
	}
	if ca.Payload.GPUUID != "GPU-deadbeef" {
		t.Errorf("expected GPU-deadbeef, got %s", ca.Payload.GPUUID)
	}
	if ca.Payload.Telemetry.SMUtilizationPct != 0.45 {
		t.Errorf("expected SM util 0.45, got %.2f", ca.Payload.Telemetry.SMUtilizationPct)
	}
	if ca.Payload.Telemetry.VRAMUsedBytes != 42949672960 {
		t.Errorf("expected VRAM 42949672960, got %d", ca.Payload.Telemetry.VRAMUsedBytes)
	}
}

func TestFromProfilerAlert_MemoryLeak(t *testing.T) {
	cfg := DefaultConfig()
	am := NewAlertMultiplexer(cfg, nil, nil)

	pa := profiler.AnomalyAlert{
		Timestamp: 3000,
		Alert: profiler.AnomalyAlertBody{
			TargetPID:   54321,
			GPUUID:      "",
			AnomalyType: profiler.AnomalyMemoryLeak,
			Severity:    profiler.SeverityWarning,
		},
	}

	ca := am.fromProfilerAlert(pa)
	if ca.Payload.TargetPID != 54321 {
		t.Errorf("expected PID 54321, got %d", ca.Payload.TargetPID)
	}
	if ca.Payload.AnomalyType != string(profiler.AnomalyMemoryLeak) {
		t.Errorf("expected HOST_MEMORY_LEAK, got %s", ca.Payload.AnomalyType)
	}
	if ca.Payload.Severity != string(profiler.SeverityWarning) {
		t.Errorf("expected WARNING, got %s", ca.Payload.Severity)
	}
	if ca.Payload.GPUUID != "" {
		t.Errorf("expected empty GPUUID, got %s", ca.Payload.GPUUID)
	}
}

func TestDedup_FirstOccurrencePublished(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SuppressionWindow = time.Hour

	am := NewAlertMultiplexer(cfg, nil, nil)

	alert := am.fromDetectorAlert(detector.Alert{
		Timestamp: 100,
		Summary:   detector.AlertSummary{TargetPID: 1, LoopType: "SEMANTIC_REPETITION_LOOP"},
	})

	am.route(context.Background(), alert)

	am.dedupMu.Lock()
	entry, ok := am.dedup[dedupKey(1, "SEMANTIC_REPETITION_LOOP", "")]
	am.dedupMu.Unlock()

	if !ok {
		t.Fatal("expected dedup entry to exist")
	}
	if entry.OccurrenceCount != 1 {
		t.Errorf("expected OccurrenceCount 1, got %d", entry.OccurrenceCount)
	}
	select {
	case <-am.internal:
	default:
		t.Error("expected alert in internal channel")
	}
}

func TestDedup_SuppressDuplicate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SuppressionWindow = time.Hour

	am := NewAlertMultiplexer(cfg, nil, nil)

	alert := am.fromDetectorAlert(detector.Alert{
		Timestamp: 100,
		Summary:   detector.AlertSummary{TargetPID: 1, LoopType: "SEMANTIC_REPETITION_LOOP"},
	})
	am.route(context.Background(), alert)

	<-am.internal

	dup := am.fromDetectorAlert(detector.Alert{
		Timestamp: 101,
		Summary:   detector.AlertSummary{TargetPID: 1, LoopType: "SEMANTIC_REPETITION_LOOP"},
	})
	am.route(context.Background(), dup)

	am.dedupMu.Lock()
	entry, ok := am.dedup[dedupKey(1, "SEMANTIC_REPETITION_LOOP", "")]
	am.dedupMu.Unlock()

	if !ok {
		t.Fatal("expected dedup entry to exist")
	}
	if entry.OccurrenceCount != 2 {
		t.Errorf("expected OccurrenceCount 2, got %d", entry.OccurrenceCount)
	}

	select {
	case <-am.internal:
		t.Error("expected duplicate to be suppressed, but alert was in internal channel")
	default:
	}
}

func TestDedup_ExpiredWindow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SuppressionWindow = 1 * time.Millisecond

	am := NewAlertMultiplexer(cfg, nil, nil)

	a1 := am.fromDetectorAlert(detector.Alert{
		Timestamp: 100,
		Summary:   detector.AlertSummary{TargetPID: 2, LoopType: "SEMANTIC_REPETITION_LOOP"},
	})
	am.route(context.Background(), a1)
	<-am.internal

	time.Sleep(5 * time.Millisecond)

	a2 := am.fromDetectorAlert(detector.Alert{
		Timestamp: 200,
		Summary:   detector.AlertSummary{TargetPID: 2, LoopType: "SEMANTIC_REPETITION_LOOP"},
	})
	am.route(context.Background(), a2)

	select {
	case <-am.internal:
	case <-time.After(50 * time.Millisecond):
		t.Error("expected alert after window expiry, but internal channel was empty")
	}
}

func TestDedup_MultipleKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SuppressionWindow = time.Hour

	am := NewAlertMultiplexer(cfg, nil, nil)

	a1 := am.fromDetectorAlert(detector.Alert{
		Timestamp: 100,
		Summary:   detector.AlertSummary{TargetPID: 1, LoopType: "SEMANTIC_REPETITION_LOOP"},
	})
	am.route(context.Background(), a1)
	<-am.internal

	a2 := am.fromProfilerAlert(profiler.AnomalyAlert{
		Timestamp: 101,
		Alert: profiler.AnomalyAlertBody{
			TargetPID:   2,
			AnomalyType: profiler.AnomalyMemoryLeak,
			Severity:    profiler.SeverityWarning,
		},
	})
	am.route(context.Background(), a2)

	select {
	case <-am.internal:
	default:
		t.Error("expected second alert for different key in internal channel")
	}

	am.dedupMu.Lock()
	count := len(am.dedup)
	am.dedupMu.Unlock()

	if count != 2 {
		t.Errorf("expected 2 dedup entries, got %d", count)
	}
}

func TestFanOut_ConsoleOutput(t *testing.T) {
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.SuppressionWindow = time.Hour

	detCh := make(chan detector.Alert, 10)
	am := NewAlertMultiplexer(cfg, nil, nil)
	am.SetConsoleOutput(&buf)
	am.AttachDetector(detCh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	am.Start(ctx)

	detCh <- detector.Alert{
		Timestamp: 100,
		Summary: detector.AlertSummary{
			TargetPID: 42,
			LoopType:  "SEMANTIC_REPETITION_LOOP",
		},
	}

	time.Sleep(100 * time.Millisecond)

	if buf.Len() == 0 {
		t.Fatal("expected console output")
	}

	var alert ConsolidatedAlert
	if err := json.Unmarshal(buf.Bytes(), &alert); err != nil {
		t.Fatalf("unmarshal console output: %v", err)
	}
	if alert.Payload.TargetPID != 42 {
		t.Errorf("expected PID 42 in console output, got %d", alert.Payload.TargetPID)
	}
	if alert.Payload.AnomalyType != "SEMANTIC_REPETITION_LOOP" {
		t.Errorf("expected loop type in console output, got %s", alert.Payload.AnomalyType)
	}
	if alert.EventID == "" {
		t.Error("expected non-empty event ID")
	}

	cancel()
	am.Stop()
}

func TestFanOut_RingBuffer(t *testing.T) {
	rb := pipeline.NewRingBuffer(1024 * 1024)
	cfg := DefaultConfig()
	cfg.SuppressionWindow = time.Hour

	detCh := make(chan detector.Alert, 10)
	am := NewAlertMultiplexer(cfg, rb, nil)
	am.AttachDetector(detCh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	am.Start(ctx)

	detCh <- detector.Alert{
		Timestamp: 100,
		Summary: detector.AlertSummary{
			TargetPID: 77,
			LoopType:  "SEMANTIC_REPETITION_LOOP",
		},
	}

	time.Sleep(100 * time.Millisecond)

	if rb.Len() == 0 {
		t.Fatal("expected at least one entry in ring buffer")
	}

	entries := rb.Drain(10)
	if len(entries) == 0 {
		t.Fatal("expected drained entries")
	}

	var alert ConsolidatedAlert
	if err := json.Unmarshal(entries[0], &alert); err != nil {
		t.Fatalf("unmarshal ringbuf entry: %v", err)
	}
	if alert.Payload.TargetPID != 77 {
		t.Errorf("expected PID 77 in ringbuf, got %d", alert.Payload.TargetPID)
	}

	cancel()
	am.Stop()
}

func TestMultiplexer_ContextCancellation(t *testing.T) {
	cfg := DefaultConfig()
	am := NewAlertMultiplexer(cfg, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	am.Start(ctx)

	done := make(chan struct{})
	go func() {
		am.wg.Wait()
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for multiplexer to stop")
	}
}

func TestConcurrentDedup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SuppressionWindow = time.Hour

	am := NewAlertMultiplexer(cfg, nil, nil)

	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pid := int64(10000 + idx%5)
			alert := am.fromDetectorAlert(detector.Alert{
				Timestamp: int64(100 + idx),
				Summary:   detector.AlertSummary{TargetPID: pid, LoopType: "SEMANTIC_REPETITION_LOOP"},
			})
			am.route(ctx, alert)
		}(i)
	}
	wg.Wait()

	am.dedupMu.Lock()
	count := len(am.dedup)
	am.dedupMu.Unlock()

	if count < 1 || count > 5 {
		t.Errorf("expected 1-5 dedup entries, got %d", count)
	}
}

func TestJSONOutputSchema(t *testing.T) {
	alert := ConsolidatedAlert{
		EventID:   "evt_bc92a83f-4e01-419b-81f2",
		Timestamp: 1782352800,
		Metadata: PropagationMetadata{
			IsDeduplicated:           true,
			CumulativeOccurrences:    14,
			SuppressionWindowSeconds: 30,
		},
		Payload: AlertPayload{
			TargetPID:   41029,
			GPUUID:      "GPU-a63e8a12-61d2-a541-bbf2-89234012e12a",
			AnomalyType: "IDLE_GPU_HOG",
			Severity:    "CRITICAL",
			Telemetry: TelemetrySnapshot{
				SMUtilizationPct: 0.45,
				VRAMUsedBytes:    42949672960,
			},
		},
	}

	data, err := json.Marshal(alert)
	if err != nil {
		t.Fatalf("marshal alert: %v", err)
	}

	var decoded ConsolidatedAlert
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal alert: %v", err)
	}

	if decoded.EventID != "evt_bc92a83f-4e01-419b-81f2" {
		t.Errorf("EventID mismatch")
	}
	if decoded.Timestamp != 1782352800 {
		t.Errorf("Timestamp mismatch")
	}
	if decoded.Metadata.IsDeduplicated != true {
		t.Errorf("IsDeduplicated mismatch")
	}
	if decoded.Metadata.CumulativeOccurrences != 14 {
		t.Errorf("CumulativeOccurrences mismatch")
	}
	if decoded.Metadata.SuppressionWindowSeconds != 30 {
		t.Errorf("SuppressionWindowSeconds mismatch")
	}
	if decoded.Payload.TargetPID != 41029 {
		t.Errorf("TargetPID mismatch")
	}
	if decoded.Payload.GPUUID != "GPU-a63e8a12-61d2-a541-bbf2-89234012e12a" {
		t.Errorf("GPUUID mismatch")
	}
	if decoded.Payload.AnomalyType != "IDLE_GPU_HOG" {
		t.Errorf("AnomalyType mismatch")
	}
	if decoded.Payload.Severity != "CRITICAL" {
		t.Errorf("Severity mismatch")
	}
	if decoded.Payload.Telemetry.SMUtilizationPct != 0.45 {
		t.Errorf("SMUtilizationPct mismatch")
	}
	if decoded.Payload.Telemetry.VRAMUsedBytes != 42949672960 {
		t.Errorf("VRAMUsedBytes mismatch")
	}
}

func TestMultiplexer_DetectorAndProfiler(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SuppressionWindow = time.Hour

	detCh := make(chan detector.Alert, 10)
	profCh := make(chan profiler.AnomalyAlert, 10)

	var buf bytes.Buffer
	am := NewAlertMultiplexer(cfg, nil, nil)
	am.SetConsoleOutput(&buf)
	am.AttachDetector(detCh)
	am.AttachProfiler(profCh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	am.Start(ctx)

	detCh <- detector.Alert{
		Timestamp: 100,
		Summary: detector.AlertSummary{
			TargetPID: 11,
			LoopType:  "SEMANTIC_REPETITION_LOOP",
		},
	}

	profCh <- profiler.AnomalyAlert{
		Timestamp: 200,
		Alert: profiler.AnomalyAlertBody{
			TargetPID:   22,
			GPUUID:      "GPU-test",
			AnomalyType: profiler.AnomalyIdleGPU,
			Severity:    profiler.SeverityCritical,
		},
	}

	time.Sleep(200 * time.Millisecond)

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 console lines, got %d", len(lines))
	}

	var foundDetector, foundProfiler bool
	for _, line := range lines {
		var alert ConsolidatedAlert
		if err := json.Unmarshal(line, &alert); err != nil {
			continue
		}
		switch alert.Payload.TargetPID {
		case 11:
			foundDetector = true
		case 22:
			foundProfiler = true
		}
	}
	if !foundDetector {
		t.Error("expected detector alert (PID 11) in console output")
	}
	if !foundProfiler {
		t.Error("expected profiler alert (PID 22) in console output")
	}

	cancel()
	am.Stop()
}
