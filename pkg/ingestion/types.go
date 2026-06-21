package ingestion

import (
	"context"
	"encoding/json"
	"time"
)

type Config struct {
	ListenAddr     string        `json:"listen_addr" yaml:"listen_addr"`
	ReadTimeout    time.Duration `json:"read_timeout" yaml:"read_timeout"`
	WriteTimeout   time.Duration `json:"write_timeout" yaml:"write_timeout"`
	IdleTimeout    time.Duration `json:"idle_timeout" yaml:"idle_timeout"`
	MaxPayloadSize int64         `json:"max_payload_size" yaml:"max_payload_size"`
	WorkerCount    int           `json:"worker_count" yaml:"worker_count"`
	JobQueueSize   int           `json:"job_queue_size" yaml:"job_queue_size"`
}

func DefaultConfig() Config {
	return Config{
		ListenAddr:     ":8443",
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxPayloadSize: 2 * 1024 * 1024,
		WorkerCount:    50,
		JobQueueSize:   10000,
	}
}

type IngestionMetadata struct {
	ReceivedTimestamp  int64  `json:"received_timestamp"`
	ResolvedTenantID   string `json:"resolved_tenant_id"`
	OriginIPAddress    string `json:"origin_ip_address"`
	ProcessingWorkerID uint64 `json:"processing_worker_id"`
}

type AgentPayload struct {
	AgentID string          `json:"agent_id"`
	Payload json.RawMessage `json:"payload"`
}

type IngestionPayload struct {
	IngestionMetadata IngestionMetadata `json:"ingestion_metadata"`
	AgentPayload      AgentPayload      `json:"agent_payload"`
}

type IngestionJob struct {
	TenantID   string
	AgentID    string
	Payload    json.RawMessage
	OriginIP   string
	ReceivedAt time.Time
}

type DownstreamHandler func(ctx context.Context, payload *IngestionPayload) error

type contextKey string

const (
	ctxKeyTenantID contextKey = "tenant_id"
	ctxKeyAgentID  contextKey = "agent_id"
)

var (
	CtxKeyTenantID any = ctxKeyTenantID
	CtxKeyAgentID  any = ctxKeyAgentID
)
