package detector

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSimHash_Consistent(t *testing.T) {
	h1 := SimHash("Error: Invalid JSON structure at line 1. Please regenerate schema")
	h2 := SimHash("Error: Invalid JSON structure at line 1. Please regenerate schema")
	if h1 != h2 {
		t.Errorf("expected identical fingerprints, got %016x vs %016x", h1, h2)
	}
}

func TestSimHash_Empty(t *testing.T) {
	h := SimHash("")
	if h != 0 {
		t.Errorf("expected 0 for empty input, got %016x", h)
	}
}

func TestSimHash_SmallHammingForNearDuplicates(t *testing.T) {
	a := "Error: Invalid JSON structure at line 1. Please regenerate schema"
	b := "Error: Invalid JSON structure at line 2. Please regenerate schema"
	ha := SimHash(a)
	hb := SimHash(b)
	d := HammingDistance(ha, hb)
	sim := NormalizedSimilarity(ha, hb)
	if d > 10 {
		t.Errorf("expected small Hamming distance for near-duplicates, got %d (sim=%.4f)", d, sim)
	}
	if sim < 0.8 {
		t.Errorf("expected similarity > 0.8 for near-duplicates, got %.4f", sim)
	}
}

func TestSimHash_LargeHammingForDifferentTexts(t *testing.T) {
	a := "The quick brown fox jumps over the lazy dog"
	b := "SELECT * FROM users WHERE id = 1; DROP TABLE users; --"
	ha := SimHash(a)
	hb := SimHash(b)
	d := HammingDistance(ha, hb)
	sim := NormalizedSimilarity(ha, hb)
	if d < 20 {
		t.Errorf("expected large Hamming distance for different texts, got %d (sim=%.4f)", d, sim)
	}
	if sim > 0.6 {
		t.Errorf("expected similarity < 0.6 for different texts, got %.4f", sim)
	}
}

func TestHammingDistance_Symmetric(t *testing.T) {
	a, b := uint64(0xFF00FF00FF00FF00), uint64(0x00FF00FF00FF00FF)
	d1 := HammingDistance(a, b)
	d2 := HammingDistance(b, a)
	if d1 != d2 {
		t.Errorf("Hamming distance should be symmetric: %d vs %d", d1, d2)
	}
}

func TestHammingDistance_Identical(t *testing.T) {
	a := uint64(0xDEADBEEF)
	d := HammingDistance(a, a)
	if d != 0 {
		t.Errorf("expected 0 for identical values, got %d", d)
	}
}

func TestNormalizedSimilarity_Range(t *testing.T) {
	a, b := uint64(0xFFFFFFFFFFF00000), uint64(0x00000000000FFFFF)
	sim := NormalizedSimilarity(a, b)
	if sim < 0 || sim > 1.0 {
		t.Errorf("similarity out of range [0,1]: %.4f", sim)
	}
}

func TestProcessWindow_AppendAndEvict(t *testing.T) {
	pw := &ProcessWindow{
		records: make([]Record, 0, 3),
		maxSize: 3,
	}
	now := time.Now().Unix()

	for i := 0; i < 5; i++ {
		pw.mu.Lock()
		pw.records = append(pw.records, Record{Timestamp: now + int64(i)})
		if len(pw.records) > pw.maxSize {
			pw.records = pw.records[len(pw.records)-pw.maxSize:]
		}
		pw.mu.Unlock()
	}

	pw.mu.Lock()
	if len(pw.records) != 3 {
		t.Errorf("expected 3 records after eviction, got %d", len(pw.records))
	}
	if pw.records[0].Timestamp != now+2 {
		t.Errorf("expected oldest record timestamp %d, got %d", now+2, pw.records[0].Timestamp)
	}
	pw.mu.Unlock()
}

func TestDetector_AlertOnDeadlock(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WindowSize = 10
	cfg.FreqThreshold = 3
	cfg.FreqWindow = 10 * time.Second
	cfg.SimilarityThreshold = 0.80
	cfg.AlertCooldown = time.Hour

	d := NewDetector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	payload := "Error: Invalid JSON structure at line 1. Please regenerate schema"
	now := time.Now().Unix()

	for i := 0; i < 6; i++ {
		d.Ingest(DetectorEvent{
			PID:       41029,
			Timestamp: now + int64(i),
			Provider:  "openai",
			Model:     "gpt-4o",
			Payload:   payload,
		})
	}

	select {
	case alert := <-d.AlertCh():
		if !alert.Summary.IsDeadlocked {
			t.Error("expected is_deadlocked = true")
		}
		if alert.Summary.TargetPID != 41029 {
			t.Errorf("expected PID 41029, got %d", alert.Summary.TargetPID)
		}
		if alert.Summary.LoopType != LoopSemanticRepetition {
			t.Errorf("expected loop type SEMANTIC_REPETITION_LOOP, got %s", alert.Summary.LoopType)
		}
		if alert.Summary.ConfidenceScore < cfg.SimilarityThreshold {
			t.Errorf("expected confidence >= %.2f, got %.4f", cfg.SimilarityThreshold, alert.Summary.ConfidenceScore)
		}
		if alert.Evidence.Provider != "openai" {
			t.Errorf("expected provider openai, got %s", alert.Evidence.Provider)
		}
		if alert.Evidence.Model != "gpt-4o" {
			t.Errorf("expected model gpt-4o, got %s", alert.Evidence.Model)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected alert but got none (timeout)")
	}
}

func TestDetector_NoAlertOnDissimilar(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WindowSize = 10
	cfg.FreqThreshold = 3
	cfg.FreqWindow = 10 * time.Second
	cfg.SimilarityThreshold = 0.88
	cfg.AlertCooldown = time.Hour

	d := NewDetector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	now := time.Now().Unix()
	payloads := []string{
		"Hello, how are you today?",
		"What is the capital of France?",
		"SELECT * FROM users WHERE id = 1",
		"The quick brown fox jumps over the lazy dog",
		"Error: Invalid JSON structure at line 1",
		"Please calculate the fibonacci sequence",
	}

	for i, p := range payloads {
		d.Ingest(DetectorEvent{
			PID:       99999,
			Timestamp: now + int64(i),
			Provider:  "openai",
			Model:     "gpt-4",
			Payload:   p,
		})
	}

	select {
	case alert := <-d.AlertCh():
		t.Errorf("unexpected alert for dissimilar payloads: %+v", alert)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDetector_AlertCooldown(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WindowSize = 10
	cfg.FreqThreshold = 3
	cfg.FreqWindow = 10 * time.Second
	cfg.SimilarityThreshold = 0.80
	cfg.AlertCooldown = 30 * time.Second

	d := NewDetector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	payload := "Repeat: This is a looping error message"
	now := time.Now().Unix()

	for i := 0; i < 6; i++ {
		d.Ingest(DetectorEvent{
			PID:       55555,
			Timestamp: now + int64(i),
			Provider:  "anthropic",
			Model:     "claude-3",
			Payload:   payload,
		})
	}

	select {
	case <-d.AlertCh():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected first alert but got none")
	}

	d.Ingest(DetectorEvent{
		PID:       55555,
		Timestamp: now + 7,
		Provider:  "anthropic",
		Model:     "claude-3",
		Payload:   payload,
	})

	select {
	case alert := <-d.AlertCh():
		t.Errorf("expected second alert to be suppressed by cooldown, got alert: %+v", alert)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDetector_NoAlertBelowFrequencyThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WindowSize = 10
	cfg.FreqThreshold = 5
	cfg.FreqWindow = 10 * time.Second
	cfg.SimilarityThreshold = 0.80

	d := NewDetector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	payload := "Error: Something went wrong"
	now := time.Now().Unix()

	for i := 0; i < 3; i++ {
		d.Ingest(DetectorEvent{
			PID:       33333,
			Timestamp: now + int64(i),
			Provider:  "openai",
			Model:     "gpt-4",
			Payload:   payload,
		})
	}

	select {
	case alert := <-d.AlertCh():
		t.Errorf("unexpected alert below frequency threshold: %+v", alert)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDetector_ConcurrentIngest(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WindowSize = 20
	cfg.FreqThreshold = 10
	cfg.FreqWindow = 10 * time.Second
	cfg.SimilarityThreshold = 0.99

	d := NewDetector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	var wg sync.WaitGroup
	now := time.Now().Unix()

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d.Ingest(DetectorEvent{
				PID:       int64(10000 + idx%5),
				Timestamp: now + int64(idx),
				Provider:  "openai",
				Model:     "gpt-4",
				Payload:   "Concurrent test payload",
			})
		}(i)
	}
	wg.Wait()

	d.mu.RLock()
	count := len(d.windows)
	d.mu.RUnlock()
	if count != 5 {
		t.Errorf("expected 5 process windows, got %d", count)
	}

	for _, pid := range []int64{10000, 10001, 10002, 10003, 10004} {
		d.mu.RLock()
		pw, ok := d.windows[pid]
		d.mu.RUnlock()
		if !ok {
			t.Errorf("expected window for PID %d", pid)
			continue
		}
		pw.mu.Lock()
		recCount := len(pw.records)
		pw.mu.Unlock()
		if recCount == 0 {
			t.Errorf("expected records for PID %d", pid)
		}
	}
}

func TestDetector_GCRemovesStaleWindows(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TTL = 1 * time.Millisecond
	cfg.GCInterval = 10 * time.Millisecond

	d := NewDetector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	d.Ingest(DetectorEvent{
		PID:       77777,
		Timestamp: time.Now().Unix(),
		Provider:  "openai",
		Model:     "gpt-4",
		Payload:   "Test payload",
	})

	d.mu.RLock()
	_, ok := d.windows[77777]
	d.mu.RUnlock()
	if !ok {
		t.Fatal("expected window to exist before GC")
	}

	time.Sleep(50 * time.Millisecond)

	d.mu.RLock()
	_, ok = d.windows[77777]
	d.mu.RUnlock()
	if ok {
		t.Error("expected window to be removed by GC")
	}
}

func TestDetector_GCPreservesActiveWindows(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TTL = 1 * time.Minute
	cfg.GCInterval = 10 * time.Millisecond

	d := NewDetector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	d.Ingest(DetectorEvent{
		PID:       88888,
		Timestamp: time.Now().Unix(),
		Provider:  "openai",
		Model:     "gpt-4",
		Payload:   "Active test payload",
	})

	time.Sleep(50 * time.Millisecond)

	d.mu.RLock()
	_, ok := d.windows[88888]
	d.mu.RUnlock()
	if !ok {
		t.Error("expected active window to be preserved by GC")
	}
}

func TestDetector_MultiplePIDs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WindowSize = 10
	cfg.FreqThreshold = 2
	cfg.FreqWindow = 10 * time.Second
	cfg.SimilarityThreshold = 0.50
	cfg.AlertCooldown = time.Hour

	d := NewDetector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	payload := "Error: looping error message"
	now := time.Now().Unix()

	for i := 0; i < 4; i++ {
		d.Ingest(DetectorEvent{
			PID:       11111,
			Timestamp: now + int64(i),
			Provider:  "openai",
			Model:     "gpt-4",
			Payload:   payload,
		})
		d.Ingest(DetectorEvent{
			PID:       22222,
			Timestamp: now + int64(i),
			Provider:  "anthropic",
			Model:     "claude-3",
			Payload:   payload,
		})
	}

	alerts := make(map[int64]bool)
	for i := 0; i < 2; i++ {
		select {
		case alert := <-d.AlertCh():
			alerts[alert.Summary.TargetPID] = true
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("expected 2 alerts, got %d", len(alerts))
		}
	}

	if !alerts[11111] {
		t.Error("expected alert for PID 11111")
	}
	if !alerts[22222] {
		t.Error("expected alert for PID 22222")
	}
}

func TestTruncatePayload(t *testing.T) {
	short := "short"
	if got := truncatePayload(short, 10); got != short {
		t.Errorf("expected %q, got %q", short, got)
	}

	long := "this is a very long string that should be truncated"
	got := truncatePayload(long, 20)
	if len(got) != 23 {
		t.Errorf("expected 23 chars (20 + '...'), got %d: %q", len(got), got)
	}
	if got != "this is a very long ..." {
		t.Errorf("expected truncated string, got %q", got)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.WindowSize != 20 {
		t.Errorf("expected WindowSize 20, got %d", cfg.WindowSize)
	}
	if cfg.TTL != 5*time.Minute {
		t.Errorf("expected TTL 5m, got %v", cfg.TTL)
	}
	if cfg.FreqThreshold != 5 {
		t.Errorf("expected FreqThreshold 5, got %d", cfg.FreqThreshold)
	}
	if cfg.FreqWindow != 10*time.Second {
		t.Errorf("expected FreqWindow 10s, got %v", cfg.FreqWindow)
	}
	if cfg.SimilarityThreshold != 0.88 {
		t.Errorf("expected SimilarityThreshold 0.88, got %.2f", cfg.SimilarityThreshold)
	}
	if cfg.AlertCooldown != 30*time.Second {
		t.Errorf("expected AlertCooldown 30s, got %v", cfg.AlertCooldown)
	}
	if cfg.GCInterval != 30*time.Second {
		t.Errorf("expected GCInterval 30s, got %v", cfg.GCInterval)
	}
}
