package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type ctxKey string

const (
	CtxKeyTenantID ctxKey = "tenant_id"
	CtxKeyAgentID  ctxKey = "agent_id"
)

type AnomalyPayload struct {
	EventID     string  `json:"event_id"`
	PID         int64   `json:"pid"`
	AnomalyType string  `json:"anomaly_type"`
	Severity    string  `json:"severity"`
	GPUUID      string  `json:"gpu_uuid,omitempty"`
	Message     string  `json:"message"`
	Timestamp   int64   `json:"timestamp"`
	NodeID      string  `json:"node_id"`
	Score       float64 `json:"score,omitempty"`
}

type EventRouter interface {

	// RouteAnomaly delivers an anomaly event to the configured routing backend.
	//
	// The open-source implementation (LocalConsoleRouter) prints the event
	// as ANSI-colored JSON to stdout for local visibility.
	//
	// The enterprise implementation (DistributedCloudRouter) — injected from
	// /ee/control-plane/pubsub/distributed_router.go — writes the event to
	// a multi-tenant InfluxDB/Timescale cluster via the control-plane's
	// ingestion pipeline. Activate it at build time by swapping the
	// implementation in cmd/agent/main.go:
	//
	//     import dcr "github.com/anomalyco/ai-compute-profiler/ee/control-plane/pubsub"
	//     router := dcr.NewDistributedCloudRouter(cfg, pool)
	//
	// The interface guarantees both implementations are interchangeable
	// without modifying any code in /internal/proxy or /internal/analytics.
	RouteAnomaly(ctx context.Context, p AnomalyPayload) error
}

type LocalConsoleRouter struct {
	nodeID string
	out    *os.File
}

func NewLocalConsoleRouter(nodeID string) *LocalConsoleRouter {
	return &LocalConsoleRouter{
		nodeID: nodeID,
		out:    os.Stdout,
	}
}

func (r *LocalConsoleRouter) RouteAnomaly(ctx context.Context, p AnomalyPayload) error {
	if p.NodeID == "" {
		p.NodeID = r.nodeID
	}
	if p.Timestamp == 0 {
		p.Timestamp = time.Now().UnixMilli()
	}

	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("local_console: marshal: %w", err)
	}

	color := "\033[93m"
	reset := "\033[0m"
	switch p.Severity {
	case "CRITICAL":
		color = "\033[91m"
	case "WARNING":
		color = "\033[93m"
	case "INFO":
		color = "\033[96m"
	}

	fmt.Fprintf(r.out, "%s[ANOMALY] %s%s\n", color, string(data), reset)
	return nil
}
