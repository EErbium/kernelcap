package policy

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type ActionType string

const (
	ActionSIGSTOP        ActionType = "SIGSTOP"
	ActionContainerPause ActionType = "CONTAINER_PAUSE"
	ActionAPIRouteSwap   ActionType = "API_ROUTE_SWAP"
	ActionTokenChop      ActionType = "TOKEN_CHOP"
	ActionCgroupFreeze   ActionType = "CGROUP_FREEZE"
)

type MitigationStatus string

const (
	StatusAuthorizedPending MitigationStatus = "AUTHORIZED_PENDING"
	StatusExecuted          MitigationStatus = "EXECUTED"
	StatusCompleted         MitigationStatus = "COMPLETED"
	StatusRejected          MitigationStatus = "REJECTED"
	StatusExpired           MitigationStatus = "EXPIRED"
)

type RuleName string

const (
	RuleSystemBlacklist RuleName = "SYSTEM_BLACKLIST_CHECK"
	RuleUserWhitelist   RuleName = "USER_WHITELIST_CHECK"
	RuleVelocityCap     RuleName = "VELOCITY_CAP_CHECK"
	RulePIDBoundary     RuleName = "PID_BOUNDARY_CHECK"
)

type MitigationIntent struct {
	TargetPID    int64
	ContainerID  string
	ProcessName  string
	ActionType   ActionType
	AnomalyType  string
	SourceModule string
	Metadata     map[string]any
}

type MitigationRecord struct {
	TransactionID    string
	Timestamp        int64
	Expiry           int64
	TargetPID        int64
	ContainerID      string
	ProcessName      string
	ActionType       ActionType
	Status           MitigationStatus
	EvaluatedRules   int
	MatchingPolicies []RuleName
	RiskScore        float64
	HistoricalCount  int
	CooldownSeconds  int
	RejectionReason  string
	Metadata         map[string]any
}

type PolicyEvaluation struct {
	IsAuthorized          bool     `json:"is_authorized"`
	EvaluatedRulesCount   int      `json:"evaluated_rules_count"`
	MatchingPoliciesApplied []string `json:"matching_policies_applied"`
	RejectionReason       string   `json:"rejection_reason,omitempty"`
}

type MitigationTargetMetadata struct {
	TargetPID         int64   `json:"target_pid"`
	ContainerName     string  `json:"container_name"`
	RiskScoreAssigned float64 `json:"risk_score_assigned"`
}

type LedgerStateSnapshot struct {
	CurrentActionStatus                  MitigationStatus `json:"current_action_status"`
	HistoricalInterventionsOnTargetCount int               `json:"historical_interventions_on_target_count"`
	EnforcedCooldownDurationSeconds      int               `json:"enforced_cooldown_duration_seconds"`
}

type PolicyEvent struct {
	LedgerTransactionID string                     `json:"ledger_transaction_id"`
	Timestamp           int64                      `json:"timestamp"`
	PolicyEvaluation    PolicyEvaluation           `json:"policy_evaluation"`
	MitigationTarget    MitigationTargetMetadata    `json:"mitigation_target_metadata"`
	LedgerState         LedgerStateSnapshot         `json:"ledger_state_snapshot"`
}

type Config struct {
	BlacklistNames  []string
	WhitelistNames  []string
	WhitelistIDs    []string
	MaxActionsPerPID int
	VelocityWindow  time.Duration
	CooldownSeconds int
	LedgerMaxEntries int
	SweepInterval   time.Duration
	PIDThreshold    int
	BufferSize      int
}

func DefaultConfig() Config {
	return Config{
		BlacklistNames:   []string{"systemd", "dockerd", "containerd", "kubelet", "init", "kerneloops"},
		MaxActionsPerPID: 5,
		VelocityWindow:   10 * time.Minute,
		CooldownSeconds:  60,
		LedgerMaxEntries: 10000,
		SweepInterval:    5 * time.Minute,
		PIDThreshold:     100,
		BufferSize:       64,
	}
}

func generateTxID() string {
	var buf [16]byte
	rand.Read(buf[:])
	return "tx_" + hex.EncodeToString(buf[:])
}
