package iam

import (
	"time"
)

type Role string

const (
	RoleViewer   Role = "Viewer"
	RoleOperator Role = "Operator"
	RoleAdmin    Role = "Admin"
)

type Config struct {
	CacheTTL     time.Duration `json:"cache_ttl" yaml:"cache_ttl"`
	CacheMaxSize int           `json:"cache_max_size" yaml:"cache_max_size"`
	KeyExpiry    time.Duration `json:"key_expiry" yaml:"key_expiry"`
}

func DefaultConfig() Config {
	return Config{
		CacheTTL:     5 * time.Minute,
		CacheMaxSize: 10000,
		KeyExpiry:    365 * 24 * time.Hour,
	}
}

type APIKeyRecord struct {
	TenantID  string    `json:"tenant_id"`
	KeyHash   string    `json:"key_hash"`
	Salt      string    `json:"salt"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type CachedToken struct {
	TenantID  string    `json:"tenant_id"`
	AgentID   string    `json:"agent_id"`
	Role      Role      `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

type CachePopulationStatus string

const (
	CacheStatusHit     CachePopulationStatus = "CACHE_HIT"
	CacheStatusMiss    CachePopulationStatus = "CACHE_MISS"
	CacheStatusStale   CachePopulationStatus = "CACHE_STALE"
	CacheCommitted     CachePopulationStatus = "COMMITTED_SUCCESS"
)

type TokenMetadata struct {
	KeyPrefix           string `json:"key_prefix"`
	KeyTruncatedDisplay string `json:"key_truncated_display"`
	ExpirationTimestamp int64  `json:"expiration_timestamp"`
}

type AuditDetails struct {
	ActionTriggeredBy   string        `json:"action_triggered_by_user"`
	TargetTenantID      string        `json:"target_tenant_id"`
	AssignedRoleProfile string        `json:"assigned_role_profile"`
	TokenMetadata       TokenMetadata `json:"token_metadata"`
}

type CryptoVerification struct {
	StorageHashMechanism  string                `json:"storage_hash_mechanism"`
	CachePopulationStatus CachePopulationStatus `json:"cache_population_status"`
	AuthLatencyMs         float64               `json:"auth_latency_ms"`
}

type SecurityAuditEvent struct {
	SecurityEventTimestamp int64               `json:"security_event_timestamp"`
	TransactionType        string              `json:"transaction_type"`
	AuditDetails           AuditDetails        `json:"audit_details"`
	CryptoVerification     CryptoVerification  `json:"cryptographic_verification"`
}

const KeyPrefix = "aicp_live_"

var roleHierarchy = map[Role]int{
	RoleViewer:   0,
	RoleOperator: 1,
	RoleAdmin:    2,
}
