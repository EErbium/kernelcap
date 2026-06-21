package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type DeliveryStatus string

const (
	DeliveryStatusDelivered DeliveryStatus = "DELIVERED"
	DeliveryStatusFailed    DeliveryStatus = "FAILED"
	DeliveryStatusRetrying  DeliveryStatus = "RETRYING"
	DeliveryStatusDead      DeliveryStatus = "DEAD_LETTER"
)

type Config struct {
	RetryMaxAttempts  int           `json:"retry_max_attempts" yaml:"retry_max_attempts"`
	RetryBaseDelay    time.Duration `json:"retry_base_delay" yaml:"retry_base_delay"`
	RetryMaxDelay     time.Duration `json:"retry_max_delay" yaml:"retry_max_delay"`
	DispatchTimeout   time.Duration `json:"dispatch_timeout" yaml:"dispatch_timeout"`
	WorkerCount       int           `json:"worker_count" yaml:"worker_count"`
	JobQueueSize      int           `json:"job_queue_size" yaml:"job_queue_size"`
	SSEKeepAlive      time.Duration `json:"sse_keep_alive" yaml:"sse_keep_alive"`
}

func DefaultConfig() Config {
	return Config{
		RetryMaxAttempts: 5,
		RetryBaseDelay:   1 * time.Second,
		RetryMaxDelay:    30 * time.Second,
		DispatchTimeout:  10 * time.Second,
		WorkerCount:      4,
		JobQueueSize:     256,
		SSEKeepAlive:     30 * time.Second,
	}
}

type WebhookConfig struct {
	TenantID string            `json:"tenant_id"`
	URL      string            `json:"url"`
	Secret   string            `json:"secret,omitempty"`
	Active   bool              `json:"active"`
	Headers  map[string]string `json:"headers,omitempty"`
}

type RetryTask struct {
	TenantID    string
	TargetURL   string
	Secret      string
	Headers     map[string]string
	Body        []byte
	Attempt     int
	LastError   string
	LastStatus  int
	NextRetryAt time.Time
}

type DispatchJob struct {
	TenantID string
	Config   WebhookConfig
	Body     []byte
	AlertID  string
	Severity string
}

type WebhookDispatchTelemetry struct {
	DispatchTimestamp  int64          `json:"dispatch_timestamp"`
	TargetTenantID     string         `json:"target_tenant_id"`
	TargetURL          string         `json:"target_url"`
	HTTPStatusCode     int            `json:"http_status_code"`
	DeliveryLatencyMs  float64        `json:"delivery_latency_ms"`
	DeliveryStatus     DeliveryStatus `json:"delivery_status"`
	RetryAttempt       int            `json:"retry_attempt"`
	SignaturePresent   bool           `json:"signature_present"`
}

type SSEConnectionTelemetry struct {
	ConnectTimestamp     int64   `json:"connect_timestamp"`
	DisconnectTimestamp  int64   `json:"disconnect_timestamp,omitempty"`
	ClientIP             string  `json:"client_ip"`
	TenantID             string  `json:"tenant_id,omitempty"`
	EventsDispatched     int     `json:"events_dispatched"`
	ConnectionDurationMs float64 `json:"connection_duration_ms,omitempty"`
}

type SSEEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

func signPayload(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func formatSignature(sig string) string {
	return "sha256=" + sig
}
