package dbadapter

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var tsColumns = []string{
	"tenant_id",
	"agent_id",
	"recorded_at",
	"cpu_utilization_pct",
	"memory_used_bytes",
	"memory_total_bytes",
	"gpu_uuids",
	"sm_utilization_pct",
	"vram_used_bytes",
	"tokens_consumed",
}

const tsTableName = "telemetry_metrics"

type TimeseriesSink struct {
	pool *pgxpool.Pool
	logf func(string, ...any)
}

func NewTimeseriesSink(pool *pgxpool.Pool, logf func(string, ...any)) *TimeseriesSink {
	return &TimeseriesSink{pool: pool, logf: logf}
}

func (s *TimeseriesSink) BulkInsert(ctx context.Context, tenantID string, records []TimeseriesMetricRecord) error {
	if len(records) == 0 {
		return nil
	}

	rows := make([][]any, 0, len(records))
	for _, r := range records {
		gpuIDs := r.GPUUUIDs
		if gpuIDs == nil {
			gpuIDs = []string{}
		}
		rows = append(rows, []any{
			r.TenantID,
			r.AgentID,
			r.RecordedAt,
			r.CPUUtilPct,
			int64(r.MemoryUsedBytes),
			int64(r.MemoryTotalBytes),
			gpuIDs,
			r.SMUtilPct,
			int64(r.VRAMUsedBytes),
			r.TokensConsumed,
		})
	}

	copyCount, err := s.pool.CopyFrom(
		ctx,
		pgx.Identifier{tsTableName},
		tsColumns,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("ts_sink: CopyFrom failed for tenant=%s: %w", tenantID, err)
	}

	if int(copyCount) != len(records) {
		s.logf("ts_sink: expected %d rows, copied %d for tenant=%s", len(records), copyCount, tenantID)
	}

	return nil
}

func (s *TimeseriesSink) EnsureSchema(ctx context.Context) error {
	sql := `CREATE TABLE IF NOT EXISTS ` + tsTableName + ` (
		id BIGSERIAL,
		tenant_id VARCHAR(64) NOT NULL,
		agent_id VARCHAR(128) NOT NULL,
		recorded_at TIMESTAMPTZ NOT NULL,
		cpu_utilization_pct DOUBLE PRECISION,
		memory_used_bytes BIGINT,
		memory_total_bytes BIGINT,
		gpu_uuids TEXT[],
		sm_utilization_pct DOUBLE PRECISION,
		vram_used_bytes BIGINT,
		tokens_consumed BIGINT DEFAULT 0
	);`

	_, err := s.pool.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("ts_sink: create table: %w", err)
	}

	idxSQL := `CREATE INDEX IF NOT EXISTS idx_ts_tenant_time ON ` + tsTableName + ` (tenant_id, recorded_at DESC);`
	_, _ = s.pool.Exec(ctx, idxSQL)

	return nil
}

const insertTelemetrySQL = `INSERT INTO ` + tsTableName + `
	(tenant_id, agent_id, recorded_at, cpu_utilization_pct, memory_used_bytes,
	 memory_total_bytes, gpu_uuids, sm_utilization_pct, vram_used_bytes, tokens_consumed)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`

func timeseriesInsert(ctx context.Context, pool *pgxpool.Pool, records []TimeseriesMetricRecord) error {
	if len(records) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, r := range records {
		gpuIDs := r.GPUUUIDs
		if gpuIDs == nil {
			gpuIDs = []string{}
		}
		batch.Queue(insertTelemetrySQL,
			r.TenantID, r.AgentID, r.RecordedAt,
			r.CPUUtilPct, int64(r.MemoryUsedBytes), int64(r.MemoryTotalBytes),
			gpuIDs, r.SMUtilPct, int64(r.VRAMUsedBytes), r.TokensConsumed,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range records {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("ts_sink: batch exec: %w", err)
		}
	}

	return nil
}
