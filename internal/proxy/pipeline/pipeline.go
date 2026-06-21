package pipeline

import (
	"context"
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/proxy"
)

type Pipeline struct {
	AgentID   string
	AuthToken string

	SnapshotCh chan []byte
	TokenCh    chan proxy.TokenUsageEvent
	RingBuf    *RingBuffer

	aggregator *Aggregator
	streamer   *Streamer
	logf       func(string, ...any)
	cancel     context.CancelFunc
}

func New(agentID, authToken, upstreamEndpoint string, pollPeriod time.Duration, ringBufMaxMB int, logf func(string, ...any)) *Pipeline {
	if ringBufMaxMB <= 0 {
		ringBufMaxMB = 50
	}
	maxBytes := ringBufMaxMB * 1024 * 1024

	ringBuf := NewRingBuffer(maxBytes)

	return &Pipeline{
		AgentID:    agentID,
		AuthToken:  authToken,
		SnapshotCh: make(chan []byte, 64),
		TokenCh:    make(chan proxy.TokenUsageEvent, 16384),
		RingBuf:    ringBuf,
		logf:       logf,
	}
}

func (p *Pipeline) Start(ctx context.Context) {
	pipeCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	p.aggregator = NewAggregator(
		p.AgentID, p.AuthToken,
		time.Second,
		p.SnapshotCh, p.TokenCh,
		p.RingBuf, p.logf,
	)

	p.streamer = NewStreamer(
		"", p.AuthToken,
		p.RingBuf,
		time.Second, p.logf,
	)

	p.aggregator.Start(pipeCtx)
	p.streamer.Start(pipeCtx)

	p.logf("pipeline: started (ring buffer max %d MB)", p.RingBuf.Capacity()/(1024*1024))
}

func (p *Pipeline) SetUpstreamEndpoint(endpoint string) {
	if p.streamer != nil {
		p.streamer.endpoint = endpoint
	}
}

func (p *Pipeline) Stop() {
	p.logf("pipeline: stopping...")

	if p.cancel != nil {
		p.cancel()
	}

	if p.aggregator != nil {
		p.aggregator.Wait()
	}
	if p.streamer != nil {
		p.streamer.Wait()
	}

}

func (p *Pipeline) Stats() (ringEntries int, ringBytes int, snapshots int64, tokens int64) {
	return p.RingBuf.Len(), p.RingBuf.Bytes(), 0, 0
}
