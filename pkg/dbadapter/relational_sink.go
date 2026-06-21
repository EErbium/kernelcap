package dbadapter

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const ledgerTableName = "mitigation_compliance_ledger"

type RelationalSink struct {
	pool *pgxpool.Pool
	logf func(string, ...any)
}

func NewRelationalSink(pool *pgxpool.Pool, logf func(string, ...any)) *RelationalSink {
	return &RelationalSink{pool: pool, logf: logf}
}

func (s *RelationalSink) SaveMitigationLog(ctx context.Context, record ControlPlaneMitigationRecord) error {
	if record.TenantID == "" {
		return fmt.Errorf("relational_sink: tenant_id is required")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:       pgx.ReadCommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.Deferrable,
	})
	if err != nil {
		return fmt.Errorf("relational_sink: begin tx: %w", err)
	}

	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				s.logf("relational_sink: rollback error: %v", rbErr)
			}
		}
	}()

	sql := `INSERT INTO ` + ledgerTableName + `
		(tenant_id, event_id, recorded_at, event_type, severity, source_module, target_pid, payload_json)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	ON CONFLICT (tenant_id, event_id) DO NOTHING`

	_, err = tx.Exec(ctx, sql,
		record.TenantID,
		record.EventID,
		record.RecordedAt,
		record.EventType,
		record.Severity,
		record.SourceModule,
		record.TargetPID,
		record.PayloadJSON,
	)
	if err != nil {
		return fmt.Errorf("relational_sink: exec: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("relational_sink: commit: %w", err)
	}

	return nil
}

func (s *RelationalSink) EnsureSchema(ctx context.Context) error {
	sql := `CREATE TABLE IF NOT EXISTS ` + ledgerTableName + ` (
		id BIGSERIAL,
		tenant_id VARCHAR(64) NOT NULL,
		event_id VARCHAR(64) NOT NULL,
		recorded_at TIMESTAMPTZ NOT NULL,
		event_type VARCHAR(32) NOT NULL,
		severity VARCHAR(16),
		source_module VARCHAR(32),
		target_pid BIGINT,
		payload_json JSONB NOT NULL,
		PRIMARY KEY (tenant_id, event_id)
	);`

	_, err := s.pool.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("relational_sink: create table: %w", err)
	}

	return nil
}
