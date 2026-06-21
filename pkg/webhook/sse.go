package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/ingestion"
)

type sseClient struct {
	ch       chan []byte
	ctx      context.Context
	cancel   context.CancelFunc
	tenantID string
	clientIP string
	created  time.Time
	events   atomic.Int64
}

type SSEBroker struct {
	cfg       Config
	clients   sync.Map
	logf      func(string, ...any)
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	diagFn    func(SSEConnectionTelemetry)
}

func NewSSEBroker(cfg Config, logf func(string, ...any)) *SSEBroker {
	return &SSEBroker{
		cfg:  cfg,
		logf: logf,
	}
}

func (b *SSEBroker) Start(ctx context.Context) {
	b.ctx, b.cancel = context.WithCancel(ctx)
	b.wg.Add(1)
	go b.keepAliveLoop()
	b.logf("webhook: SSE broker started")
}

func (b *SSEBroker) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	b.wg.Wait()
}

func (b *SSEBroker) SetDiagnosticCallback(fn func(SSEConnectionTelemetry)) {
	b.diagFn = fn
}

func (b *SSEBroker) Broadcast(eventType string, data json.RawMessage) {
	var buf []byte
	buf = append(buf, []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(data)))...)

	b.clients.Range(func(key, value any) bool {
		client := value.(*sseClient)
		select {
		case client.ch <- buf:
			client.events.Add(1)
		default:
			b.logf("webhook: SSE client %s buffer full, dropping event", client.clientIP)
		}
		return true
	})
}

func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	clientIP := extractClientIP(r)
	tenantID, _ := r.Context().Value(ingestion.CtxKeyTenantID).(string)

	ctx, cancel := context.WithCancel(r.Context())
	client := &sseClient{
		ch:       make(chan []byte, 64),
		ctx:      ctx,
		cancel:   cancel,
		tenantID: tenantID,
		clientIP: clientIP,
		created:  time.Now(),
	}

	b.clients.Store(client.ch, client)

	connectTime := time.Now()
	b.logf("webhook: SSE client connected ip=%s tenant=%s", clientIP, tenantID)

	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"connected\"}\n\n")
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			b.cleanupClient(client, connectTime)
			return
		case <-r.Context().Done():
			client.cancel()
			b.cleanupClient(client, connectTime)
			return
		case msg := <-client.ch:
			_, err := w.Write(msg)
			if err != nil {
				client.cancel()
				b.cleanupClient(client, connectTime)
				return
			}
			flusher.Flush()
		}
	}
}

func (b *SSEBroker) cleanupClient(client *sseClient, connectTime time.Time) {
	b.clients.Delete(client.ch)
	duration := time.Since(connectTime).Seconds() * 1000
	events := client.events.Load()

	b.logf("webhook: SSE client disconnected ip=%s tenant=%s duration=%.0fms events=%d",
		client.clientIP, client.tenantID, duration, events)

	if b.diagFn != nil {
		b.diagFn(SSEConnectionTelemetry{
			ConnectTimestamp:     connectTime.Unix(),
			DisconnectTimestamp:  time.Now().Unix(),
			ClientIP:             client.clientIP,
			TenantID:             client.tenantID,
			EventsDispatched:     int(events),
			ConnectionDurationMs: duration,
		})
	}
}

func (b *SSEBroker) keepAliveLoop() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.cfg.SSEKeepAlive)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.clients.Range(func(key, value any) bool {
				client := value.(*sseClient)
				select {
				case client.ch <- []byte(": heartbeat\n\n"):
				default:
				}
				return true
			})
		}
	}
}

func (b *SSEBroker) ActiveClients() int {
	count := 0
	b.clients.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func extractClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return fwd
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return real
	}
	return r.RemoteAddr
}
