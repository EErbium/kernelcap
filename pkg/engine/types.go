package engine

type ImpactMetrics struct {
	VRAMLeakBytesPerSec float64 `json:"vram_leak_bytes_per_sec"`
	TokenWasteCount     int     `json:"token_waste_count"`
}

type AnomalyEntry struct {
	PID           int64         `json:"pid"`
	State         string        `json:"state"`
	ImpactMetrics ImpactMetrics `json:"impact_metrics"`
}

type SelfCheckMetrics struct {
	ProfilerMemoryRSSBytes uint64  `json:"profiler_memory_rss_bytes"`
	ProfilerCPUUtilPct     float64 `json:"profiler_cpu_utilization_pct"`
}

type MitigationSummary struct {
	Enabled        bool   `json:"enabled"`
	ActiveCount    int    `json:"active_mitigation_count"`
	TotalActions   int    `json:"total_actions_recorded"`
	LastEventID    string `json:"last_mitigation_event_id,omitempty"`
}

type RouterSummary struct {
	Enabled          bool   `json:"enabled"`
	ActiveChops      int    `json:"active_token_chops"`
	ActiveFallbacks  int    `json:"active_fallback_routes"`
	TotalTokensSaved int    `json:"total_tokens_saved"`
}

type PolicySummary struct {
	ActivePendingCount int  `json:"active_pending_mitigations"`
	TotalTracked       int  `json:"total_tracked_actions"`
	SweeperActive      bool `json:"sweeper_active"`
}

type RollbackSummary struct {
	RollbacksTriggered   int `json:"rollbacks_triggered"`
	DriftResolved        int `json:"drift_resolved"`
	OrphansCleaned       int `json:"orphans_cleaned"`
	ActiveMitigations    int `json:"active_mitigations_scanned"`
}

type UnifiedMetrics struct {
	EngineStatus    string             `json:"engine_status"`
	UptimeSeconds   int64              `json:"uptime_seconds"`
	LocalNodeID     string             `json:"local_node_id"`
	ActivePIDsCount int                `json:"active_monitored_pids_count"`
	Anomalies       []AnomalyEntry     `json:"active_system_anomalies"`
	SelfCheck       SelfCheckMetrics   `json:"system_performance_self_check"`
	Mitigations     *MitigationSummary `json:"automated_mitigations,omitempty"`
	Router          *RouterSummary     `json:"proxy_router,omitempty"`
	Policy          *PolicySummary     `json:"policy_ledger,omitempty"`
	Rollback        *RollbackSummary   `json:"state_sync_engine,omitempty"`
}
