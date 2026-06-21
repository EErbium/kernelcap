package pipeline

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/model"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/proxy"
)

type Aggregator struct {
	agentID    string
	authToken  string
	pollPeriod time.Duration
	snapshotCh <-chan []byte
	tokenCh    <-chan proxy.TokenUsageEvent
	ringBuf    *RingBuffer
	logf       func(string, ...any)
	wg         sync.WaitGroup
	stopped    chan struct{}
}

func NewAggregator(agentID, authToken string, pollPeriod time.Duration,
	snapshotCh <-chan []byte, tokenCh <-chan proxy.TokenUsageEvent,
	ringBuf *RingBuffer, logf func(string, ...any),
) *Aggregator {
	return &Aggregator{
		agentID:    agentID,
		authToken:  authToken,
		pollPeriod: pollPeriod,
		snapshotCh: snapshotCh,
		tokenCh:    tokenCh,
		ringBuf:    ringBuf,
		logf:       logf,
		stopped:    make(chan struct{}),
	}
}

func (a *Aggregator) Start(ctx context.Context) {
	a.wg.Add(1)
	go a.run(ctx)
}

func (a *Aggregator) Wait() {
	a.wg.Wait()
}

func (a *Aggregator) Stopped() <-chan struct{} {
	return a.stopped
}

func (a *Aggregator) run(ctx context.Context) {
	defer a.wg.Done()
	defer close(a.stopped)

	ticker := time.NewTicker(a.pollPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.logf("aggregator: context cancelled, flushing remaining events")
			a.aggregate()
			return
		case <-ticker.C:
			a.aggregate()
		}
	}
}

func (a *Aggregator) aggregate() {
	var latestSnap []byte
	select {
	case snap := <-a.snapshotCh:
		latestSnap = snap
	default:
	}

	if latestSnap == nil {
		// No snapshot available yet — still drain proxy events for completeness
		proxyEvents := a.drainProxyEvents()
		if len(proxyEvents) > 0 {
			a.logf("aggregator: %d proxy events without snapshot, dropping", len(proxyEvents))
		}
		return
	}

	var snap model.Snapshot
	if err := json.Unmarshal(latestSnap, &snap); err != nil {
		a.logf("aggregator: unmarshal snapshot: %v", err)
		return
	}

	proxyEvents := a.drainProxyEvents()

	var proxyPayload []ProxyEvent
	if len(proxyEvents) > 0 {
		proxyPayload = make([]ProxyEvent, 0, len(proxyEvents))
		for _, ev := range proxyEvents {
			proxyPayload = append(proxyPayload, ProxyEvent{
				ClientPID:   ev.ClientPID,
				Model:       ev.Model,
				TotalTokens: ev.Metrics.TotalTokens,
			})
		}
	}

	payload, err := PayloadFromSnapshot(&snap, proxyPayload, a.agentID, a.authToken)
	if err != nil {
		a.logf("aggregator: build payload: %v", err)
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		a.logf("aggregator: marshal payload: %v", err)
		return
	}

	a.ringBuf.Push(data)

	if len(proxyPayload) > 0 && len(data) > 0 {
		a.logf("aggregator: pushed payload host_cpu=%.1f gpu=%d proxy=%d bytes=%d",
			payload.Payload.Host.CPUUtilizationPct,
			len(payload.Payload.GPU),
			len(payload.Payload.Proxy),
			len(data),
		)
	}
}

func (a *Aggregator) drainProxyEvents() []proxy.TokenUsageEvent {
	var events []proxy.TokenUsageEvent
	for {
		select {
		case ev := <-a.tokenCh:
			events = append(events, ev)
		default:
			return events
		}
	}
}
