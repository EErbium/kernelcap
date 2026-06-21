package dbadapter

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

type flushRequest struct {
	tenantID string
	trigger  FlushTrigger
	records  []TimeseriesMetricRecord
}

type TenantMetricsBuffer struct {
	mu       sync.Mutex
	tenantID string
	records  []TimeseriesMetricRecord
}

type MetricsBatcher struct {
	cfg      Config
	buffers  sync.Map
	flushCh  chan flushRequest
	flushWg  sync.WaitGroup
	execFn   func(context.Context, string, FlushTrigger, []TimeseriesMetricRecord) error
	logf     func(string, ...any)
	ctx      context.Context
	cancel   context.CancelFunc
	dropped  atomic.Int64
	flushed  atomic.Int64
}

func NewMetricsBatcher(cfg Config, execFn func(context.Context, string, FlushTrigger, []TimeseriesMetricRecord) error, logf func(string, ...any)) *MetricsBatcher {
	return &MetricsBatcher{
		cfg:     cfg,
		flushCh: make(chan flushRequest, 1024),
		execFn:  execFn,
		logf:    logf,
	}
}

func (b *MetricsBatcher) Start(ctx context.Context) {
	b.ctx, b.cancel = context.WithCancel(ctx)

	b.flushWg.Add(1)
	go b.flushWorker()

	b.flushWg.Add(1)
	go b.tickerLoop()

	b.logf("batcher: started (interval=%v max_records=%d)", b.cfg.BatchFlushInterval, b.cfg.BatchMaxRecords)
}

func (b *MetricsBatcher) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	b.flushWg.Wait()
}

func (b *MetricsBatcher) Append(record TimeseriesMetricRecord) {
	raw, _ := b.buffers.LoadOrStore(record.TenantID, &TenantMetricsBuffer{
		tenantID: record.TenantID,
		records:  make([]TimeseriesMetricRecord, 0, b.cfg.BatchMaxRecords+64),
	})
	buf := raw.(*TenantMetricsBuffer)

	buf.mu.Lock()
	buf.records = append(buf.records, record)
	count := len(buf.records)
	shouldFlush := count >= b.cfg.BatchMaxRecords
	buf.mu.Unlock()

	if shouldFlush {
		b.triggerFlush(buf, FlushTriggerThreshold)
	}
}

func (b *MetricsBatcher) triggerFlush(buf *TenantMetricsBuffer, trigger FlushTrigger) {
	buf.mu.Lock()
	if len(buf.records) == 0 {
		buf.mu.Unlock()
		return
	}
	batch := buf.records
	buf.records = make([]TimeseriesMetricRecord, 0, b.cfg.BatchMaxRecords+64)
	buf.mu.Unlock()

	req := flushRequest{
		tenantID: buf.tenantID,
		trigger:  trigger,
		records:  batch,
	}

	select {
	case b.flushCh <- req:
	case <-b.ctx.Done():
		b.dropped.Add(int64(len(batch)))
		b.logf("batcher: dropping %d records for tenant=%s, context done", len(batch), buf.tenantID)
	}
}

func (b *MetricsBatcher) flushWorker() {
	defer b.flushWg.Done()

	for {
		select {
		case <-b.ctx.Done():
			b.drainRemaining()
			return
		case req := <-b.flushCh:
			if err := b.execFn(b.ctx, req.tenantID, req.trigger, req.records); err != nil {
				b.logf("batcher: flush error for tenant=%s: %v", req.tenantID, err)
			}
			b.flushed.Add(int64(len(req.records)))
		}
	}
}

func (b *MetricsBatcher) tickerLoop() {
	defer b.flushWg.Done()

	ticker := time.NewTicker(b.cfg.BatchFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.flushAll(FlushTriggerTimeHorizon)
		}
	}
}

func (b *MetricsBatcher) flushAll(trigger FlushTrigger) {
	b.buffers.Range(func(key, value any) bool {
		buf := value.(*TenantMetricsBuffer)
		buf.mu.Lock()
		if len(buf.records) > 0 {
			batch := buf.records
			buf.records = make([]TimeseriesMetricRecord, 0, b.cfg.BatchMaxRecords+64)
			buf.mu.Unlock()

			req := flushRequest{
				tenantID: buf.tenantID,
				trigger:  trigger,
				records:  batch,
			}

			select {
			case b.flushCh <- req:
			case <-b.ctx.Done():
				b.dropped.Add(int64(len(batch)))
				b.logf("batcher: dropping %d records on ticker flush for tenant=%s",
					len(batch), buf.tenantID)
			}
		} else {
			buf.mu.Unlock()
		}
		return true
	})
}

func (b *MetricsBatcher) drainRemaining() {
	b.buffers.Range(func(key, value any) bool {
		buf := value.(*TenantMetricsBuffer)
		buf.mu.Lock()
		if len(buf.records) > 0 {
			batch := buf.records
			buf.records = nil
			buf.mu.Unlock()

			if err := b.execFn(context.Background(), buf.tenantID, FlushTriggerTimeHorizon, batch); err != nil {
				b.logf("batcher: drain error for tenant=%s: %v", buf.tenantID, err)
			}
			b.flushed.Add(int64(len(batch)))
		} else {
			buf.mu.Unlock()
		}
		return true
	})
}

type batcherSnapshot struct {
	TenantID string `json:"tenant_id"`
	Buffered int    `json:"buffered_records"`
}

func (b *MetricsBatcher) Snapshot() string {
	var snap []batcherSnapshot
	b.buffers.Range(func(key, value any) bool {
		buf := value.(*TenantMetricsBuffer)
		buf.mu.Lock()
		snap = append(snap, batcherSnapshot{
			TenantID: buf.tenantID,
			Buffered: len(buf.records),
		})
		buf.mu.Unlock()
		return true
	})
	data, _ := json.Marshal(snap)
	return string(data)
}

func (b *MetricsBatcher) DroppedCount() int64 {
	return b.dropped.Load()
}

func (b *MetricsBatcher) FlushedCount() int64 {
	return b.flushed.Load()
}
