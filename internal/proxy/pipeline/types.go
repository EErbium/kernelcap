package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/model"
)

type UpstreamPayload struct {
	AgentID       string       `json:"agent_id"`
	AuthTokenHash string       `json:"auth_token_hash"`
	Payload       PayloadBlock `json:"payload"`
}

type PayloadBlock struct {
	Timestamp int64                    `json:"timestamp"`
	Host      *model.HostMetrics       `json:"host,omitempty"`
	GPU       []model.GPUDeviceMetrics `json:"gpu,omitempty"`
	Proxy     []ProxyEvent             `json:"network_proxy,omitempty"`
}

type ProxyEvent struct {
	ClientPID   int    `json:"client_pid"`
	Model       string `json:"model"`
	TotalTokens int    `json:"total_tokens_consumed"`
}

func NewUpstreamPayload(agentID, authToken string, block PayloadBlock) UpstreamPayload {
	tokenHash := sha256Hex(authToken)
	return UpstreamPayload{
		AgentID:       agentID,
		AuthTokenHash: tokenHash,
		Payload:       block,
	}
}

func sha256Hex(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func PayloadFromSnapshot(snap *model.Snapshot, proxyEvents []ProxyEvent, agentID, authToken string) (UpstreamPayload, error) {
	if snap == nil {
		return UpstreamPayload{}, fmt.Errorf("snapshot is nil")
	}

	hostCopy := snap.HostMetrics

	block := PayloadBlock{
		Timestamp: snap.Timestamp,
		Host:      &hostCopy,
		Proxy:     proxyEvents,
	}

	if len(snap.GPUDevices) > 0 {
		gpuCopy := make([]model.GPUDeviceMetrics, len(snap.GPUDevices))
		copy(gpuCopy, snap.GPUDevices)
		block.GPU = gpuCopy
	}

	return NewUpstreamPayload(agentID, authToken, block), nil
}
