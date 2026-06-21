package profiler

import (
	"sync"
	"time"
)

type AnomalyType string

const (
	AnomalyIdleGPU    AnomalyType = "IDLE_GPU_HOG"
	AnomalyMemoryLeak AnomalyType = "HOST_MEMORY_LEAK"
)

type Severity string

const (
	SeverityWarning  Severity = "WARNING"
	SeverityCritical Severity = "CRITICAL"
)

type Config struct {
	WindowSize            int
	MaxAge                time.Duration
	IdleVRAMThreshold     uint64
	IdleSMThresholdPct    float64
	IdleDuration          time.Duration
	LeakSlopeThreshold    float64
	LeakRSquaredThreshold float64
	AnalysisInterval      time.Duration
	AlertCooldown         time.Duration
	GCInterval            time.Duration
}

func DefaultConfig() Config {
	return Config{
		WindowSize:            60,
		MaxAge:                5 * time.Minute,
		IdleVRAMThreshold:     4 * 1024 * 1024 * 1024,
		IdleSMThresholdPct:    2.0,
		IdleDuration:          3 * time.Minute,
		LeakSlopeThreshold:    1024 * 1024,
		LeakRSquaredThreshold: 0.90,
		AnalysisInterval:      30 * time.Second,
		AlertCooldown:         5 * time.Minute,
		GCInterval:            60 * time.Second,
	}
}

type ResourceKey struct {
	PID    int64
	GPUUID string
}

type TimeSeriesPoint struct {
	Timestamp   int64
	SMUtilPct   float64
	VRAMUsed    uint64
	VRAMTotal   uint64
	RSSBytes    uint64
	CPUUsagePct float64
}

type ResourceHistory struct {
	mu          sync.Mutex
	key         ResourceKey
	points      []TimeSeriesPoint
	maxSize     int
	maxAge      time.Duration
	lastAccess  time.Time
	lastAlertAt time.Time
}

type MetricsSummary struct {
	DurationSeconds              float64 `json:"duration_seconds"`
	CurrentVRAMAllocationBytes   uint64  `json:"current_vram_allocation_bytes"`
	RollingAvgSMUtilizationPct   float64 `json:"rolling_avg_sm_utilization_pct"`
	CalculatedEfficiencyCoefficient float64 `json:"calculated_efficiency_coefficient"`
}

type MemoryTrendAnalysis struct {
	OLSSlopeBytesPerSec float64 `json:"ols_slope_bytes_per_sec"`
	LinearityRSquared   float64 `json:"linearity_r_squared"`
}

type AnomalyAlertBody struct {
	TargetPID     int64               `json:"target_pid"`
	GPUUID        string              `json:"gpu_uuid"`
	AnomalyType   AnomalyType         `json:"anomaly_type"`
	Severity      Severity            `json:"severity"`
	MetricsSummary MetricsSummary      `json:"metrics_summary"`
	MemoryTrend    MemoryTrendAnalysis `json:"memory_trend_analysis"`
}

type AnomalyAlert struct {
	Timestamp int64             `json:"timestamp"`
	Alert     AnomalyAlertBody  `json:"anomaly_alert"`
}
