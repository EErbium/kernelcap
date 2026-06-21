package mitigator

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/policy"
)

type ActionState string

const (
	ActionPaused       ActionState = "PAUSED"
	ActionThrottle     ActionState = "THROTTLED"
	ActionResumed      ActionState = "RESUMED"
	ActionOrphaned     ActionState = "ORPHANED_CLEANED"
	ActionDriftSynced  ActionState = "DRIFT_SYNCED"
)

type MechanismType string

const (
	MechanismSignalStop     MechanismType = "SIGNAL_STOP"
	MechanismContainerPause MechanismType = "CONTAINER_PAUSE"
	MechanismCgroupFreeze   MechanismType = "CGROUP_FREEZE"
)

type ActionStatus string

const (
	ActionStatusSuccess ActionStatus = "SUCCESS"
	ActionStatusSkipped ActionStatus = "SKIPPED"
	ActionStatusFailed  ActionStatus = "FAILED"
)

type TargetIdentifier struct {
	PID         int    `json:"pid"`
	ContainerID string `json:"container_id"`
	ProcessName string `json:"process_name"`
}

type ActionExecuted struct {
	Mechanism           MechanismType `json:"mechanism"`
	SignalSent          string        `json:"signal_sent"`
	Status              ActionStatus  `json:"status"`
	ExecutionDurationMs float64       `json:"execution_duration_ms"`
}

type PolicyContext struct {
	IsWhitelistedEntity bool   `json:"is_whitelisted_entity"`
	ReversibilityToken  string `json:"reversibility_token"`
}

type MitigationEvent struct {
	MitigationEventID string           `json:"mitigation_event_id"`
	Timestamp         int64            `json:"timestamp"`
	Target            TargetIdentifier `json:"target_identifier"`
	Action            ActionExecuted   `json:"action_executed"`
	Policy            PolicyContext    `json:"policy_context"`
}

type LedgerEntry struct {
	PID               int
	ContainerID       string
	ProcessName       string
	AnomalyType       string
	Timestamp         int64
	State             ActionState
	ReversibilityToken string
	Mechanism         MechanismType
	EventID           string
}

type Config struct {
	DockerSocketPath string
	DockerTimeout    time.Duration
	CgroupRoot       string
	ProcRoot         string
	WhitelistNames   []string
	WhitelistIDs     []string
	BufferSize       int
	Policy           *policy.Engine
}

func DefaultConfig() Config {
	return Config{
		DockerSocketPath: "unix:///var/run/docker.sock",
		DockerTimeout:    5 * time.Second,
		CgroupRoot:       "/sys/fs/cgroup",
		ProcRoot:         "/proc",
		WhitelistNames:   []string{"systemd", "dockerd", "containerd", "kubelet", "sshd", "init", "kerneloops"},
		BufferSize:       64,
	}
}

func generateEventID() string {
	var buf [16]byte
	rand.Read(buf[:])
	return "mit_" + hex.EncodeToString(buf[:])
}

func generateRevToken() string {
	var buf [8]byte
	rand.Read(buf[:])
	return "rev_token_" + hex.EncodeToString(buf[:])
}
