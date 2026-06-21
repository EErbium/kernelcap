package dbadapter

import (
	"time"
)

type Config struct {
	DSN                string        `json:"dsn" yaml:"dsn"`
	MaxOpenConns       int           `json:"max_open_conns" yaml:"max_open_conns"`
	MaxIdleConns       int           `json:"max_idle_conns" yaml:"max_idle_conns"`
	ConnMaxLifetime    time.Duration `json:"conn_max_lifetime" yaml:"conn_max_lifetime"`
	BatchFlushInterval time.Duration `json:"batch_flush_interval" yaml:"batch_flush_interval"`
	BatchMaxRecords    int           `json:"batch_max_records" yaml:"batch_max_records"`
}

func DefaultConfig() Config {
	return Config{
		DSN:                "postgres://localhost:5432/ai_compute_profiler?sslmode=disable",
		MaxOpenConns:       100,
		MaxIdleConns:       10,
		ConnMaxLifetime:    30 * time.Minute,
		BatchFlushInterval: 2 * time.Second,
		BatchMaxRecords:    1000,
	}
}

type TimeseriesMetricRecord struct {
	TenantID        string
	AgentID         string
	RecordedAt      time.Time
	CPUUtilPct      float64
	MemoryUsedBytes uint64
	MemoryTotalBytes uint64
	GPUUUIDs        []string
	SMUtilPct       float64
	VRAMUsedBytes   uint64
	TokensConsumed  int64
}

type ControlPlaneMitigationRecord struct {
	TenantID    string
	EventID     string
	RecordedAt  time.Time
	EventType   string
	Severity    string
	SourceModule string
	TargetPID   int64
	PayloadJSON string
}

type FlushTrigger string

const (
	FlushTriggerThreshold   FlushTrigger = "BATCH_THRESHOLD_REACHED"
	FlushTriggerTimeHorizon FlushTrigger = "TIME_HORIZON_BREACHED"
)

type TransactionSummary struct {
	TargetTenantID        string `json:"target_tenant_id"`
	BatchFlushTrigger     string `json:"batch_flush_trigger"`
	RecordsCommittedCount int    `json:"records_committed_count"`
}

type WritePathAnalytics struct {
	TimeseriesBulkInsertDurationMs  float64 `json:"timeseries_bulk_insert_duration_ms"`
	RelationalAuditInsertDurationMs float64 `json:"relational_audit_insert_duration_ms"`
	ActiveDatabaseConnections       int     `json:"active_database_connections"`
}

type PersistenceStatus string

const (
	PersistenceStatusSynced   PersistenceStatus = "SYNCHRONIZED_SUCCESS"
	PersistenceStatusFailed   PersistenceStatus = "SYNCHRONIZATION_FAILED"
	PersistenceStatusPending  PersistenceStatus = "SYNCHRONIZATION_PENDING"
)

type DatabaseSyncTelemetry struct {
	DatabaseSyncTimestamp int64               `json:"database_sync_timestamp"`
	TransactionSummary    TransactionSummary   `json:"transaction_summary"`
	WritePathAnalytics    WritePathAnalytics   `json:"write_path_analytics"`
	PersistenceStatus     PersistenceStatus    `json:"persistence_status"`
}
