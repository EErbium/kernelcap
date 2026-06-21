package alerter

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type Config struct {
	SuppressionWindow   time.Duration
	InternalBufferSize  int
	FanOutCount         int
	EventIDPrefix       string
}

func DefaultConfig() Config {
	return Config{
		SuppressionWindow:  30 * time.Second,
		InternalBufferSize: 256,
		FanOutCount:        2,
		EventIDPrefix:      "evt_",
	}
}

type PropagationMetadata struct {
	IsDeduplicated            bool `json:"is_deduplicated"`
	CumulativeOccurrences     int  `json:"cumulative_occurrences_in_window"`
	SuppressionWindowSeconds  int  `json:"suppression_window_seconds"`
}

type TelemetrySnapshot struct {
	SMUtilizationPct float64 `json:"sm_utilization_pct"`
	VRAMUsedBytes    uint64  `json:"vram_used_bytes"`
}

type AlertPayload struct {
	TargetPID   int64              `json:"target_pid"`
	GPUUID      string             `json:"gpu_uuid"`
	AnomalyType string             `json:"anomaly_type"`
	Severity    string             `json:"severity"`
	Telemetry   TelemetrySnapshot  `json:"telemetry_snapshot"`
}

type ConsolidatedAlert struct {
	EventID   string              `json:"event_id"`
	Timestamp int64               `json:"timestamp"`
	Metadata  PropagationMetadata `json:"propagation_metadata"`
	Payload   AlertPayload        `json:"alert_payload"`
}

type DedupEntry struct {
	FirstSeen       int64
	OccurrenceCount int
}

func generateEventID(prefix string) string {
	var buf [8]byte
	rand.Read(buf[:])
	return prefix + hex.EncodeToString(buf[:])
}
