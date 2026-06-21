package router

import (
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/policy"
)

type RemedyType string

const (
	RemedyNone          RemedyType = "NONE"
	RemedyTokenChop     RemedyType = "TOKEN_CHOP"
	RemedyFallbackRoute RemedyType = "DYNAMIC_FALLBACK_ROUTING"
	RemedyBoth          RemedyType = "BOTH"
)

type HandshakeStatus string

const (
	HandshakeRoutedAndVerified HandshakeStatus = "ROUTED_AND_VERIFIED"
	HandshakeChoppedOnly       HandshakeStatus = "CHOPPED_ONLY"
	HandshakeNoAction          HandshakeStatus = "NO_ACTION"
	HandshakeFailed            HandshakeStatus = "FAILED"
)

type MitigationState struct {
	PID               int64
	Remedy            RemedyType
	OriginalModel     string
	FallbackModel     string
	ActivatedAt       int64
	LastAlertAt       int64
	CoolingUntil      int64
	TokensSaved       int
	RequestsProcessed int
	Active            bool
}

type InterceptedProcess struct {
	PID                int    `json:"pid"`
	OriginalTargetModel string `json:"original_target_model"`
}

type RemedyDetails struct {
	ReroutedToLocalEndpoint string `json:"rerouted_to_local_endpoint,omitempty"`
	SubstitutedModelString  string `json:"substituted_model_string,omitempty"`
	TokensSavedByChopper    int    `json:"tokens_saved_by_chopper"`
}

type AppliedRemedy struct {
	Type    string         `json:"type"`
	Details RemedyDetails  `json:"details"`
}

type ExecutionTelemetry struct {
	ProcessingOverheadMs   float64          `json:"processing_overhead_ms"`
	RoutingHandshakeStatus HandshakeStatus  `json:"routing_handshake_status"`
}

type RouterEvent struct {
	MitigationTimestamp int64               `json:"mitigation_timestamp"`
	InterceptedProcess  InterceptedProcess  `json:"intercepted_process"`
	AppliedRemedy       AppliedRemedy       `json:"applied_remedy"`
	ExecutionTelemetry  ExecutionTelemetry  `json:"execution_telemetry"`
}

type AlertTrigger struct {
	PID         int64
	AnomalyType string
}

type Config struct {
	Enabled             bool
	MaxMessagesBeforeChop int
	KeepRecentMessages  int
	FallbackEndpoint    string
	FallbackModel       string
	FallbackAuthToken   string
	CoolingOffDuration  time.Duration
	BufferSize          int
	TokenEstimateDivisor int
	Policy              *policy.Engine
}

func DefaultConfig() Config {
	return Config{
		Enabled:               false,
		MaxMessagesBeforeChop: 20,
		KeepRecentMessages:    10,
		FallbackEndpoint:      "http://127.0.0.1:8000/v1/chat/completions",
		FallbackModel:         "meta-llama/Llama-3-8b-Instruct",
		CoolingOffDuration:    60 * time.Second,
		BufferSize:            64,
		TokenEstimateDivisor:  4,
	}
}
