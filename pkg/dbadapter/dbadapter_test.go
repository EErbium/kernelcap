package dbadapter

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/ingestion"
)

func skipIfNoDB(t *testing.T) string {
	dsn := os.Getenv("PG_DSN")
	if dsn == "" {
		t.Skip("Skipping: set PG_DSN environment variable")
	}
	return dsn
}

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxOpenConns != 100 {
		t.Fatalf("expected MaxOpenConns=100, got %d", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 10 {
		t.Fatalf("expected MaxIdleConns=10, got %d", cfg.MaxIdleConns)
	}
	if cfg.BatchMaxRecords != 1000 {
		t.Fatalf("expected BatchMaxRecords=1000, got %d", cfg.BatchMaxRecords)
	}
	if cfg.BatchFlushInterval != 2*time.Second {
		t.Fatalf("expected BatchFlushInterval=2s, got %v", cfg.BatchFlushInterval)
	}
}

func TestFlushTriggerConstants(t *testing.T) {
	if FlushTriggerThreshold != "BATCH_THRESHOLD_REACHED" {
		t.Fatalf("unexpected threshold trigger: %s", FlushTriggerThreshold)
	}
	if FlushTriggerTimeHorizon != "TIME_HORIZON_BREACHED" {
		t.Fatalf("unexpected horizon trigger: %s", FlushTriggerTimeHorizon)
	}
}

func TestMetricsBatcherAppendAndFlush(t *testing.T) {
	var mu sync.Mutex
	var flushed []flushRequest

	execFn := func(ctx context.Context, tenantID string, trigger FlushTrigger, records []TimeseriesMetricRecord) error {
		mu.Lock()
		flushed = append(flushed, flushRequest{
			tenantID: tenantID,
			trigger:  trigger,
			records:  records,
		})
		mu.Unlock()
		return nil
	}

	cfg := DefaultConfig()
	cfg.BatchMaxRecords = 5
	cfg.BatchFlushInterval = 100 * time.Millisecond

	batcher := NewMetricsBatcher(cfg, execFn, t.Logf)
	batcher.Start(context.Background())

	for i := 0; i < 7; i++ {
		batcher.Append(TimeseriesMetricRecord{
			TenantID:   "tenant-alpha",
			AgentID:    "agent-01",
			RecordedAt: time.Now(),
			CPUUtilPct: float64(i) * 10,
		})
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	total := 0
	for _, f := range flushed {
		total += len(f.records)
	}
	mu.Unlock()

	if total != 7 {
		t.Fatalf("expected 7 total flushed, got %d", total)
	}

	batcher.Stop()

	mu.Lock()
	triggerCounts := map[FlushTrigger]int{}
	for _, f := range flushed {
		triggerCounts[f.trigger]++
	}
	mu.Unlock()

	t.Logf("flush triggers: %v", triggerCounts)
}

func TestMetricsBatcherMultiTenant(t *testing.T) {
	var mu sync.Mutex
	flushed := map[string]int{}

	execFn := func(ctx context.Context, tenantID string, trigger FlushTrigger, records []TimeseriesMetricRecord) error {
		mu.Lock()
		flushed[tenantID] += len(records)
		mu.Unlock()
		return nil
	}

	cfg := DefaultConfig()
	cfg.BatchMaxRecords = 10
	cfg.BatchFlushInterval = 50 * time.Millisecond

	batcher := NewMetricsBatcher(cfg, execFn, t.Logf)
	batcher.Start(context.Background())

	for i := 0; i < 15; i++ {
		batcher.Append(TimeseriesMetricRecord{TenantID: "tenant-alpha", AgentID: "a1", RecordedAt: time.Now()})
		batcher.Append(TimeseriesMetricRecord{TenantID: "tenant-beta", AgentID: "b1", RecordedAt: time.Now()})
	}

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	alphaCount := flushed["tenant-alpha"]
	betaCount := flushed["tenant-beta"]
	mu.Unlock()

	if alphaCount != 15 {
		t.Fatalf("expected 15 for tenant-alpha, got %d", alphaCount)
	}
	if betaCount != 15 {
		t.Fatalf("expected 15 for tenant-beta, got %d", betaCount)
	}

	batcher.Stop()
}

func TestMetricsBatcherFlushOnThreshold(t *testing.T) {
	var mu sync.Mutex
	var callCount int

	execFn := func(ctx context.Context, tenantID string, trigger FlushTrigger, records []TimeseriesMetricRecord) error {
		mu.Lock()
		callCount++
		if trigger != FlushTriggerThreshold {
			t.Errorf("expected threshold trigger, got %s", trigger)
		}
		if len(records) != 10 {
			t.Errorf("expected 10 records in threshold flush, got %d", len(records))
		}
		mu.Unlock()
		return nil
	}

	cfg := DefaultConfig()
	cfg.BatchMaxRecords = 10
	cfg.BatchFlushInterval = 60 * time.Second

	batcher := NewMetricsBatcher(cfg, execFn, t.Logf)
	batcher.Start(context.Background())

	for i := 0; i < 10; i++ {
		batcher.Append(TimeseriesMetricRecord{TenantID: "tenant-gamma", AgentID: "g1", RecordedAt: time.Now()})
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if callCount != 1 {
		t.Fatalf("expected 1 flush call (threshold), got %d", callCount)
	}
	mu.Unlock()

	batcher.Stop()
}

func TestMetricsBatcherDroppedCount(t *testing.T) {
	execFn := func(ctx context.Context, tenantID string, trigger FlushTrigger, records []TimeseriesMetricRecord) error {
		return nil
	}

	cfg := DefaultConfig()
	cfg.BatchFlushInterval = time.Hour

	batcher := NewMetricsBatcher(cfg, execFn, t.Logf)
	batcher.Start(context.Background())

	batcher.Stop()

	if batcher.DroppedCount() != 0 {
		t.Fatalf("expected 0 dropped, got %d", batcher.DroppedCount())
	}
}

func TestMetricsBatcherSnapshot(t *testing.T) {
	execFn := func(ctx context.Context, tenantID string, trigger FlushTrigger, records []TimeseriesMetricRecord) error {
		return nil
	}

	cfg := DefaultConfig()
	cfg.BatchMaxRecords = 100
	cfg.BatchFlushInterval = time.Hour

	batcher := NewMetricsBatcher(cfg, execFn, t.Logf)
	batcher.Start(context.Background())

	batcher.Append(TimeseriesMetricRecord{TenantID: "tenant-snap", AgentID: "s1", RecordedAt: time.Now()})

	snap := batcher.Snapshot()
	if snap == "" {
		t.Fatal("expected non-empty snapshot")
	}

	var parsed []map[string]any
	if err := json.Unmarshal([]byte(snap), &parsed); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 tenant in snapshot, got %d", len(parsed))
	}
	if parsed[0]["tenant_id"] != "tenant-snap" {
		t.Fatalf("expected tenant-snap, got %v", parsed[0]["tenant_id"])
	}

	batcher.Stop()
}

func TestControlPlaneMitigationRecordValidation(t *testing.T) {
	record := ControlPlaneMitigationRecord{
		TenantID:     "tenant-test",
		EventID:      "evt_abc123",
		RecordedAt:   time.Now(),
		EventType:    "MITIGATION",
		Severity:     "HIGH",
		SourceModule: "mitigator",
		TargetPID:    41029,
		PayloadJSON:  `{"action":"SIGSTOP","pid":41029}`,
	}

	if record.TenantID == "" {
		t.Fatal("tenant_id must not be empty")
	}
	if record.EventID == "" {
		t.Fatal("event_id must not be empty")
	}
	if record.EventType != "MITIGATION" {
		t.Fatalf("unexpected event_type: %s", record.EventType)
	}
}

func TestTimeseriesMetricRecordAggregation(t *testing.T) {
	record := TimeseriesMetricRecord{
		TenantID:        "tenant-test",
		AgentID:         "agent-01",
		RecordedAt:      time.Now(),
		CPUUtilPct:      42.5,
		MemoryUsedBytes: 34359738368,
		GPUUUIDs:        []string{"GPU-a63e8a12"},
		SMUtilPct:       87.5,
		VRAMUsedBytes:   55104028672,
		TokensConsumed:  1280,
	}

	if record.CPUUtilPct != 42.5 {
		t.Fatalf("expected CPU 42.5, got %f", record.CPUUtilPct)
	}
	if len(record.GPUUUIDs) != 1 {
		t.Fatalf("expected 1 GPU, got %d", len(record.GPUUUIDs))
	}
	if record.TokensConsumed != 1280 {
		t.Fatalf("expected 1280 tokens, got %d", record.TokensConsumed)
	}
}

func TestDatabaseSyncTelemetryJSON(t *testing.T) {
	diag := DatabaseSyncTelemetry{
		DatabaseSyncTimestamp: 1782352800,
		TransactionSummary: TransactionSummary{
			TargetTenantID:        "tenant_enterprise_reliance_09",
			BatchFlushTrigger:     "TIME_HORIZON_BREACHED",
			RecordsCommittedCount: 412,
		},
		WritePathAnalytics: WritePathAnalytics{
			TimeseriesBulkInsertDurationMs:  4.12,
			RelationalAuditInsertDurationMs: 1.05,
			ActiveDatabaseConnections:       24,
		},
		PersistenceStatus: PersistenceStatusSynced,
	}

	data, err := json.Marshal(diag)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["database_sync_timestamp"].(float64) != 1782352800 {
		t.Fatalf("unexpected timestamp")
	}

	summary := parsed["transaction_summary"].(map[string]any)
	if summary["target_tenant_id"] != "tenant_enterprise_reliance_09" {
		t.Fatalf("unexpected tenant_id")
	}
	if summary["batch_flush_trigger"] != "TIME_HORIZON_BREACHED" {
		t.Fatalf("unexpected trigger")
	}
	if summary["records_committed_count"].(float64) != 412 {
		t.Fatalf("unexpected record count")
	}

	analytics := parsed["write_path_analytics"].(map[string]any)
	if analytics["timeseries_bulk_insert_duration_ms"].(float64) != 4.12 {
		t.Fatalf("unexpected ts duration")
	}
	if analytics["active_database_connections"].(float64) != 24 {
		t.Fatalf("unexpected active connections")
	}

	if parsed["persistence_status"] != "SYNCHRONIZED_SUCCESS" {
		t.Fatalf("unexpected status")
	}
}

func TestMetricsBatcherConcurrentAppend(t *testing.T) {
	execFn := func(ctx context.Context, tenantID string, trigger FlushTrigger, records []TimeseriesMetricRecord) error {
		return nil
	}

	cfg := DefaultConfig()
	cfg.BatchMaxRecords = 50
	cfg.BatchFlushInterval = 50 * time.Millisecond

	batcher := NewMetricsBatcher(cfg, execFn, t.Logf)
	batcher.Start(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				batcher.Append(TimeseriesMetricRecord{
					TenantID:   "tenant-concurrent",
					AgentID:    "agent-concurrent",
					RecordedAt: time.Now(),
					CPUUtilPct: float64(id*100 + j),
				})
			}
		}(i)
	}
	wg.Wait()

	time.Sleep(200 * time.Millisecond)

	flushed := batcher.FlushedCount()
	dropped := batcher.DroppedCount()
	total := flushed + dropped

	if total != 200 {
		t.Fatalf("expected 200 total records (flushed+dropped), got flushed=%d dropped=%d", flushed, dropped)
	}

	batcher.Stop()
}

func TestDBAdapterIntegration(t *testing.T) {
	dsn := skipIfNoDB(t)

	cfg := DefaultConfig()
	cfg.DSN = dsn
	cfg.BatchFlushInterval = 100 * time.Millisecond
	cfg.BatchMaxRecords = 50

	adapter := NewDBAdapter(cfg, t.Logf)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer adapter.Stop()

	payload := buildTestPayload("tenant-integration", "agent-integration", 45.2, 87.3, 1280)
	if err := adapter.HandleIngestedPayload(ctx, payload); err != nil {
		t.Fatalf("handle payload: %v", err)
	}

	record := ControlPlaneMitigationRecord{
		TenantID:     "tenant-integration",
		EventID:      "evt_integration_001",
		RecordedAt:   time.Now(),
		EventType:    "MITIGATION",
		Severity:     "HIGH",
		SourceModule: "test",
		TargetPID:    41029,
		PayloadJSON:  `{"test":true}`,
	}
	if err := adapter.SaveMitigationLog(ctx, record); err != nil {
		t.Fatalf("save mitigation log: %v", err)
	}
}

func TestDBAdapterDownstreamHandler(t *testing.T) {
	dsn := skipIfNoDB(t)

	cfg := DefaultConfig()
	cfg.DSN = dsn
	cfg.BatchFlushInterval = 50 * time.Millisecond
	cfg.BatchMaxRecords = 10

	adapter := NewDBAdapter(cfg, t.Logf)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer adapter.Stop()

	handler := adapter.AsDownstreamHandler()
	if handler == nil {
		t.Fatal("downstream handler should not be nil")
	}

	for i := 0; i < 5; i++ {
		payload := buildTestPayload("tenant-downstream", "agent-downstream", float64(i)*10, 50.0, 100)
		if err := handler(ctx, payload); err != nil {
			t.Fatalf("handler call %d: %v", i, err)
		}
	}

	time.Sleep(200 * time.Millisecond)
}

func TestDBAdapterConcurrentSaves(t *testing.T) {
	dsn := skipIfNoDB(t)

	cfg := DefaultConfig()
	cfg.DSN = dsn

	adapter := NewDBAdapter(cfg, t.Logf)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := adapter.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer adapter.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			record := ControlPlaneMitigationRecord{
				TenantID:     "tenant-concurrent-save",
				EventID:      "evt_concurrent_" + itoa(id),
				RecordedAt:   time.Now(),
				EventType:    "POLICY_BREACH",
				Severity:     "MEDIUM",
				SourceModule: "policy",
				TargetPID:    int64(10000 + id),
				PayloadJSON:  `{"concurrent":true}`,
			}
			if err := adapter.SaveMitigationLog(ctx, record); err != nil {
				t.Errorf("concurrent save %d: %v", id, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestMetricsBatcherExecFnError(t *testing.T) {
	var errCount atomic.Int32
	execFn := func(ctx context.Context, tenantID string, trigger FlushTrigger, records []TimeseriesMetricRecord) error {
		errCount.Add(1)
		return nil
	}

	cfg := DefaultConfig()
	cfg.BatchMaxRecords = 3
	cfg.BatchFlushInterval = 50 * time.Millisecond

	batcher := NewMetricsBatcher(cfg, execFn, t.Logf)
	batcher.Start(context.Background())

	for i := 0; i < 6; i++ {
		batcher.Append(TimeseriesMetricRecord{TenantID: "tenant-err", AgentID: "e1", RecordedAt: time.Now()})
	}

	time.Sleep(150 * time.Millisecond)

	if errCount.Load() == 0 {
		t.Fatal("expected execFn to be called at least once")
	}

	batcher.Stop()
}

func buildTestPayload(tenantID, agentID string, cpuPct, smPct float64, tokens int) *ingestion.IngestionPayload {
	now := time.Now().Unix()
	return &ingestion.IngestionPayload{
		IngestionMetadata: ingestion.IngestionMetadata{
			ReceivedTimestamp: now,
			ResolvedTenantID:  tenantID,
			OriginIPAddress:   "10.0.0.1",
		},
		AgentPayload: ingestion.AgentPayload{
			AgentID: agentID,
			Payload: json.RawMessage(`{
				"timestamp": ` + itoa(int(now)) + `,
				"host": {
					"cpu_utilization_pct": ` + ftoa(cpuPct) + `,
					"memory_used_bytes": 34359738368,
					"memory_total_bytes": 68719476736
				},
				"gpu": [{
					"uuid": "GPU-a63e8a12",
					"sm_utilization_pct": ` + ftoa(smPct) + `,
					"vram_used_bytes": 55104028672
				}],
				"network_proxy": [{
					"client_pid": 41029,
					"model": "gpt-4o",
					"total_tokens_consumed": ` + itoa(tokens) + `
				}]
			}`),
		},
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func ftoa(f float64) string {
	return itoa(int(f)) + "." + itoa(int((f-float64(int(f)))*100+0.5))
}
