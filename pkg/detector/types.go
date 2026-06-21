package detector

import (
	"sync"
	"time"
)

type LoopType string

const (
	LoopSemanticRepetition LoopType = "SEMANTIC_REPETITION_LOOP"
)

type Config struct {
	WindowSize          int
	TTL                 time.Duration
	FreqThreshold       int
	FreqWindow          time.Duration
	SimilarityThreshold float64
	AlertCooldown       time.Duration
	GCInterval          time.Duration
}

func DefaultConfig() Config {
	return Config{
		WindowSize:          20,
		TTL:                 5 * time.Minute,
		FreqThreshold:       5,
		FreqWindow:          10 * time.Second,
		SimilarityThreshold: 0.88,
		AlertCooldown:       30 * time.Second,
		GCInterval:          30 * time.Second,
	}
}

type DetectorEvent struct {
	PID       int64
	Timestamp int64
	Provider  string
	Model     string
	Payload   string
}

type AlertMetrics struct {
	RequestsInWindow   int     `json:"requests_in_window"`
	TimeWindowSeconds  float64 `json:"time_window_seconds"`
	MeanSimilarityIndex float64 `json:"mean_similarity_index"`
}

type AlertEvidence struct {
	Provider             string `json:"provider"`
	Model                string `json:"model"`
	RepeatedPayloadSnippet string `json:"repeated_payload_snippet"`
}

type AlertSummary struct {
	TargetPID       int64       `json:"target_pid"`
	IsDeadlocked    bool        `json:"is_deadlocked"`
	ConfidenceScore float64     `json:"confidence_score"`
	LoopType        LoopType    `json:"loop_type"`
	Metrics         AlertMetrics `json:"metrics"`
}

type Alert struct {
	Timestamp int64        `json:"timestamp"`
	Summary   AlertSummary `json:"analysis_summary"`
	Evidence  AlertEvidence `json:"evidence"`
}

type Record struct {
	Timestamp int64
	Provider  string
	Model     string
	Payload   string
	SimHash   uint64
}

type ProcessWindow struct {
	mu          sync.Mutex
	records     []Record
	maxSize     int
	lastAccess  time.Time
	lastAlertAt time.Time
}
