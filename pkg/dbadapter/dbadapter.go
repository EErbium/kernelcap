package dbadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/ingestion"
)

type DBAdapter struct {
	cfg             Config
	poolMgr         *DBPoolManager
	timeseriesSink  *TimeseriesSink
	relationalSink  *RelationalSink
	batcher         *MetricsBatcher
	logf            func(string, ...any)
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	closed          atomic.Bool
	lastSyncDiag    atomic.Value
	diagMu          sync.Mutex
	ingestionCount  atomic.Int64
}

func NewDBAdapter(cfg Config, logf func(string, ...any)) *DBAdapter {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	a := &DBAdapter{
		cfg:  cfg,
		logf: logf,
	}

	tsExecFn := func(ctx context.Context, tenantID string, trigger FlushTrigger, records []TimeseriesMetricRecord) error {
		start := time.Now()
		tsSink := a.timeseriesSink
		if tsSink == nil {
			return fmt.Errorf("dbadapter: timeseries sink not initialized")
		}
		if err := tsSink.BulkInsert(ctx, tenantID, records); err != nil {
			return err
		}
		elapsed := time.Since(start).Seconds() * 1000

		a.emitFlushDiagnostic(tenantID, trigger, len(records), elapsed, 0)
		return nil
	}

	a.batcher = NewMetricsBatcher(cfg, tsExecFn, logf)

	return a
}

func (a *DBAdapter) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	poolMgr, err := NewDBPoolManager(ctx, a.cfg, a.logf)
	if err != nil {
		return fmt.Errorf("dbadapter: init pool: %w", err)
	}
	a.poolMgr = poolMgr

	pool := poolMgr.Pool()
	a.timeseriesSink = NewTimeseriesSink(pool, a.logf)
	a.relationalSink = NewRelationalSink(pool, a.logf)

	if err := a.timeseriesSink.EnsureSchema(ctx); err != nil {
		a.logf("dbadapter: timeseries schema warning: %v", err)
	}
	if err := a.relationalSink.EnsureSchema(ctx); err != nil {
		a.logf("dbadapter: relational schema warning: %v", err)
	}

	a.batcher.Start(ctx)

	a.wg.Add(1)
	go a.diagLoop()

	a.logf("dbadapter: started (dsn=%s max_open=%d batch=%d/%v)",
		a.cfg.DSN, a.cfg.MaxOpenConns, a.cfg.BatchMaxRecords, a.cfg.BatchFlushInterval)

	return nil
}

func (a *DBAdapter) Stop() {
	if a.closed.Swap(true) {
		return
	}
	a.logf("dbadapter: stopping...")

	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()

	a.batcher.Stop()

	if a.poolMgr != nil {
		a.poolMgr.Close()
	}

}

func (a *DBAdapter) HandleIngestedPayload(ctx context.Context, payload *ingestion.IngestionPayload) error {
	record := TimeseriesMetricRecord{
		TenantID:   payload.IngestionMetadata.ResolvedTenantID,
		AgentID:    payload.AgentPayload.AgentID,
		RecordedAt: time.Now(),
	}

	var parsed struct {
		Timestamp int64 `json:"timestamp"`
		Host      *struct {
			CPUUtilizationPct float64 `json:"cpu_utilization_pct"`
			MemoryUsedBytes   uint64  `json:"memory_used_bytes"`
			MemoryTotalBytes  uint64  `json:"memory_total_bytes"`
		} `json:"host"`
		GPU []struct {
			UUID             string  `json:"uuid"`
			SMUtilizationPct float64 `json:"sm_utilization_pct"`
			VRAMUsedBytes    uint64  `json:"vram_used_bytes"`
		} `json:"gpu"`
		NetworkProxy []struct {
			TotalTokens int `json:"total_tokens_consumed"`
		} `json:"network_proxy"`
	}

	if err := json.Unmarshal(payload.AgentPayload.Payload, &parsed); err != nil {
		return fmt.Errorf("dbadapter: parse payload: %w", err)
	}

	if parsed.Timestamp > 0 {
		record.RecordedAt = time.Unix(parsed.Timestamp, 0)
	}

	if parsed.Host != nil {
		record.CPUUtilPct = parsed.Host.CPUUtilizationPct
		record.MemoryUsedBytes = parsed.Host.MemoryUsedBytes
		record.MemoryTotalBytes = parsed.Host.MemoryTotalBytes
	}

	if len(parsed.GPU) > 0 {
		var totalSM float64
		var totalVRAM uint64
		uuids := make([]string, 0, len(parsed.GPU))
		for _, g := range parsed.GPU {
			uuids = append(uuids, g.UUID)
			totalSM += g.SMUtilizationPct
			totalVRAM += g.VRAMUsedBytes
		}
		record.GPUUUIDs = uuids
		record.SMUtilPct = totalSM / float64(len(parsed.GPU))
		record.VRAMUsedBytes = totalVRAM
	}

	if len(parsed.NetworkProxy) > 0 {
		var totalTokens int64
		for _, p := range parsed.NetworkProxy {
			totalTokens += int64(p.TotalTokens)
		}
		record.TokensConsumed = totalTokens
	}

	a.ingestionCount.Add(1)
	a.batcher.Append(record)
	return nil
}

func (a *DBAdapter) AsDownstreamHandler() ingestion.DownstreamHandler {
	return a.HandleIngestedPayload
}

func (a *DBAdapter) SaveMitigationLog(ctx context.Context, record ControlPlaneMitigationRecord) error {
	if a.relationalSink == nil {
		return fmt.Errorf("dbadapter: relational sink not initialized")
	}
	start := time.Now()

	if err := a.relationalSink.SaveMitigationLog(ctx, record); err != nil {
		return err
	}

	elapsed := time.Since(start).Seconds() * 1000

	a.emitFlushDiagnostic(record.TenantID, FlushTriggerThreshold, 1, 0, elapsed)

	return nil
}

func (a *DBAdapter) emitFlushDiagnostic(tenantID string, trigger FlushTrigger, count int, tsMs, relMs float64) {
	a.diagMu.Lock()
	defer a.diagMu.Unlock()

	activeConns := 0
	if a.poolMgr != nil {
		activeConns = a.poolMgr.ActiveConnections()
	}

	diag := DatabaseSyncTelemetry{
		DatabaseSyncTimestamp: time.Now().Unix(),
		TransactionSummary: TransactionSummary{
			TargetTenantID:        tenantID,
			BatchFlushTrigger:     string(trigger),
			RecordsCommittedCount: count,
		},
		WritePathAnalytics: WritePathAnalytics{
			TimeseriesBulkInsertDurationMs:  tsMs,
			RelationalAuditInsertDurationMs: relMs,
			ActiveDatabaseConnections:       activeConns,
		},
		PersistenceStatus: PersistenceStatusSynced,
	}

	a.lastSyncDiag.Store(&diag)

	data, _ := json.Marshal(diag)
	a.logf("dbadapter: flush: %s", string(data))
}

func (a *DBAdapter) LastDiagnostic() *DatabaseSyncTelemetry {
	v := a.lastSyncDiag.Load()
	if v == nil {
		return nil
	}
	return v.(*DatabaseSyncTelemetry)
}

func (a *DBAdapter) diagLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.logf("dbadapter: ingested=%d flushed=%d dropped=%d buffers=%s",
				a.ingestionCount.Load(), a.batcher.FlushedCount(),
				a.batcher.DroppedCount(), a.batcher.Snapshot())
		}
	}
}
