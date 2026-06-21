package detector

import (
	"context"
	"sync"
	"time"
)

type Detector struct {
	cfg     Config
	mu      sync.RWMutex
	windows map[int64]*ProcessWindow
	alerts  chan Alert
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	logf    func(string, ...any)
}

func NewDetector(cfg Config, logf func(string, ...any)) *Detector {
	return &Detector{
		cfg:     cfg,
		windows: make(map[int64]*ProcessWindow),
		alerts:  make(chan Alert, 64),
		logf:    logf,
	}
}

func (d *Detector) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	d.wg.Add(1)
	go d.gcLoop(ctx)
}

func (d *Detector) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.wg.Wait()
}

func (d *Detector) AlertCh() <-chan Alert {
	return d.alerts
}

func (d *Detector) Ingest(ev DetectorEvent) {
	fp := SimHash(ev.Payload)

	pw := d.getOrCreateWindow(ev.PID)
	if pw == nil {
		return
	}

	pw.mu.Lock()
	defer pw.mu.Unlock()

	pw.records = append(pw.records, Record{
		Timestamp: ev.Timestamp,
		Provider:  ev.Provider,
		Model:     ev.Model,
		Payload:   ev.Payload,
		SimHash:   fp,
	})
	pw.lastAccess = time.Now()

	if len(pw.records) > pw.maxSize {
		excess := len(pw.records) - pw.maxSize
		pw.records = pw.records[excess:]
	}

	if d.isFrequentLocked(pw, ev.Timestamp) {
		d.analyzeAndAlert(ev.PID, pw, ev.Timestamp)
	}
}

func (d *Detector) getOrCreateWindow(pid int64) *ProcessWindow {
	d.mu.RLock()
	pw, ok := d.windows[pid]
	d.mu.RUnlock()
	if ok {
		return pw
	}

	d.mu.Lock()
	pw, ok = d.windows[pid]
	if ok {
		d.mu.Unlock()
		return pw
	}

	pw = &ProcessWindow{
		records: make([]Record, 0, d.cfg.WindowSize),
		maxSize: d.cfg.WindowSize,
	}
	d.windows[pid] = pw
	d.mu.Unlock()
	return pw
}

func (d *Detector) isFrequentLocked(pw *ProcessWindow, now int64) bool {
	if d.cfg.FreqThreshold <= 0 {
		return false
	}
	cutoff := now - int64(d.cfg.FreqWindow.Seconds())
	count := 0
	for i := len(pw.records) - 1; i >= 0; i-- {
		if pw.records[i].Timestamp >= cutoff {
			count++
		} else {
			break
		}
	}
	return count >= d.cfg.FreqThreshold
}

func (d *Detector) analyzeAndAlert(pid int64, pw *ProcessWindow, now int64) {
	n := len(pw.records)
	if n < 2 {
		return
	}

	newest := pw.records[n-1]

	var totalSimilarity float64
	var comparisons int
	oldestTime := newest.Timestamp

	for i := 0; i < n-1; i++ {
		r := pw.records[i]
		if r.Timestamp < oldestTime {
			oldestTime = r.Timestamp
		}
		sim := NormalizedSimilarity(newest.SimHash, r.SimHash)
		totalSimilarity += sim
		comparisons++
	}

	if comparisons == 0 {
		return
	}

	meanSim := totalSimilarity / float64(comparisons)

	if meanSim < d.cfg.SimilarityThreshold {
		return
	}

	if !pw.lastAlertAt.IsZero() && time.Since(pw.lastAlertAt) < d.cfg.AlertCooldown {
		return
	}
	pw.lastAlertAt = time.Now()

	timeWindow := float64(newest.Timestamp - oldestTime)
	if timeWindow < 1 {
		timeWindow = 1
	}

	alert := Alert{
		Timestamp: now,
		Summary: AlertSummary{
			TargetPID:       pid,
			IsDeadlocked:    true,
			ConfidenceScore: meanSim,
			LoopType:        LoopSemanticRepetition,
			Metrics: AlertMetrics{
				RequestsInWindow:    n,
				TimeWindowSeconds:   timeWindow,
				MeanSimilarityIndex: meanSim,
			},
		},
		Evidence: AlertEvidence{
			Provider:              newest.Provider,
			Model:                 newest.Model,
			RepeatedPayloadSnippet: truncatePayload(newest.Payload, 200),
		},
	}

	select {
	case d.alerts <- alert:
	default:
		d.logf("detector: alert channel full, dropping alert for PID %d", alert.Summary.TargetPID)
	}
}

func (d *Detector) gcLoop(ctx context.Context) {
	defer d.wg.Done()
	ticker := time.NewTicker(d.cfg.GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.runGC()
			return
		case <-ticker.C:
			d.runGC()
		}
	}
}

func (d *Detector) runGC() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for pid, pw := range d.windows {
		pw.mu.Lock()
		stale := time.Since(pw.lastAccess) > d.cfg.TTL
		pw.mu.Unlock()
		if stale {
			delete(d.windows, pid)
			d.logf("detector: GC removed PID %d", pid)
		}
	}
}

func truncatePayload(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
