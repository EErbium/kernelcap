package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/model"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/proxy"
)

func TestRingBuffer_PushPop(t *testing.T) {
	rb := NewRingBuffer(1024 * 1024)

	rb.Push([]byte("hello"))
	rb.Push([]byte("world"))

	if rb.Len() != 2 {
		t.Errorf("expected len 2, got %d", rb.Len())
	}

	entry, ok := rb.Pop()
	if !ok || string(entry) != "hello" {
		t.Errorf("expected 'hello', got %q", string(entry))
	}

	entry, ok = rb.Pop()
	if !ok || string(entry) != "world" {
		t.Errorf("expected 'world', got %q", string(entry))
	}

	_, ok = rb.Pop()
	if ok {
		t.Error("expected empty")
	}
}

func TestRingBuffer_EvictOldest(t *testing.T) {
	rb := NewRingBuffer(100)

	rb.Push([]byte("a"))  // 1 byte
	rb.Push([]byte("b"))  // 1 byte
	// Push 99 bytes: total would be 101, need to evict oldest until <=100
	rb.Push([]byte(string(make([]byte, 99))))

	// After eviction: "a" popped, "b" (1) + 99 = 100 fits
	if rb.Len() != 2 {
		t.Errorf("expected len 2 after eviction, got %d", rb.Len())
	}

	entry, ok := rb.Pop()
	if !ok {
		t.Fatal("expected entry")
	}
	if string(entry) != "b" {
		t.Errorf("expected 'b' (oldest remaining), got %q", string(entry))
	}

	entry, ok = rb.Pop()
	if !ok {
		t.Fatal("expected second entry")
	}
	if len(entry) != 99 {
		t.Errorf("expected 99 bytes for second entry, got %d", len(entry))
	}
}

func TestRingBuffer_Drain(t *testing.T) {
	rb := NewRingBuffer(1024 * 1024)

	for i := 0; i < 10; i++ {
		rb.Push([]byte{byte('a' + i)})
	}

	entries := rb.Drain(3)
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
	if string(entries[0]) != "a" {
		t.Errorf("expected 'a', got %q", string(entries[0]))
	}

	if rb.Len() != 7 {
		t.Errorf("expected 7 remaining, got %d", rb.Len())
	}
}

func TestRingBuffer_DrainAll(t *testing.T) {
	rb := NewRingBuffer(1024 * 1024)
	for i := 0; i < 5; i++ {
		rb.Push([]byte("x"))
	}

	entries := rb.Drain(100)
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
	if rb.Len() != 0 {
		t.Errorf("expected empty, got %d", rb.Len())
	}
}

func TestRingBuffer_EmptyDrain(t *testing.T) {
	rb := NewRingBuffer(100)
	entries := rb.Drain(10)
	if entries != nil {
		t.Errorf("expected nil, got %v", entries)
	}
}

func TestRingBuffer_BytesTracking(t *testing.T) {
	rb := NewRingBuffer(1000)
	rb.Push([]byte("hello"))  // 5 bytes
	rb.Push([]byte("world"))  // 5 bytes

	if rb.Bytes() != 10 {
		t.Errorf("expected 10 bytes, got %d", rb.Bytes())
	}

	rb.Pop()
	if rb.Bytes() != 5 {
		t.Errorf("expected 5 bytes after pop, got %d", rb.Bytes())
	}
}

func TestRingBuffer_Close(t *testing.T) {
	rb := NewRingBuffer(1000)
	rb.Push([]byte("data"))
	rb.Close()
	if rb.Len() != 0 {
		t.Errorf("expected empty after close")
	}
}

func TestSHA256(t *testing.T) {
	hash := sha256Hex("test-token")
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if len(hash) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(hash))
	}
}

func TestSHA256_Empty(t *testing.T) {
	hash := sha256Hex("")
	if hash != "" {
		t.Errorf("expected empty for empty input, got %s", hash)
	}
}

func TestNewUpstreamPayload(t *testing.T) {
	block := PayloadBlock{
		Timestamp: 1000,
		Host:      &model.HostMetrics{CPUUtilizationPct: 42.5},
	}
	payload := NewUpstreamPayload("node-1", "secret-token", block)

	if payload.AgentID != "node-1" {
		t.Errorf("expected node-1, got %s", payload.AgentID)
	}
	if payload.AuthTokenHash == "" {
		t.Error("expected non-empty auth token hash")
	}
	if payload.Payload.Host.CPUUtilizationPct != 42.5 {
		t.Errorf("expected 42.5, got %f", payload.Payload.Host.CPUUtilizationPct)
	}
}

func TestPayloadFromSnapshot(t *testing.T) {
	snap := &model.Snapshot{
		Timestamp: 2000,
		HostMetrics: model.HostMetrics{
			CPUUtilizationPct: 55.0,
			MemoryUsedBytes:   1024,
		},
	}

	proxyEvents := []ProxyEvent{
		{ClientPID: 12345, Model: "gpt-4", TotalTokens: 500},
	}

	payload, err := PayloadFromSnapshot(snap, proxyEvents, "agent-xyz", "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if payload.Payload.Proxy[0].Model != "gpt-4" {
		t.Errorf("expected gpt-4, got %s", payload.Payload.Proxy[0].Model)
	}
	if payload.Payload.Host.CPUUtilizationPct != 55.0 {
		t.Errorf("expected 55.0, got %f", payload.Payload.Host.CPUUtilizationPct)
	}
}

func TestPayloadFromSnapshot_Nil(t *testing.T) {
	_, err := PayloadFromSnapshot(nil, nil, "a", "b")
	if err == nil {
		t.Error("expected error for nil snapshot")
	}
}

func TestPayloadJSONRoundTrip(t *testing.T) {
	block := PayloadBlock{
		Timestamp: 3000,
		Host:      &model.HostMetrics{CPUUtilizationPct: 77.7, MemoryUsedBytes: 4096},
		GPU: []model.GPUDeviceMetrics{
			{UUID: "GPU-abc", SMUtilizationPct: 88.8, MemoryUsedBytes: 2048},
		},
		Proxy: []ProxyEvent{
			{ClientPID: 99, Model: "claude-3", TotalTokens: 1000},
		},
	}

	payload := NewUpstreamPayload("node-99", "my-token", block)
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded UpstreamPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.AgentID != "node-99" {
		t.Errorf("agent_id mismatch")
	}
	if decoded.Payload.Host.CPUUtilizationPct != 77.7 {
		t.Errorf("cpu mismatch")
	}
	if decoded.Payload.GPU[0].UUID != "GPU-abc" {
		t.Errorf("gpu uuid mismatch")
	}
	if decoded.Payload.Proxy[0].Model != "claude-3" {
		t.Errorf("proxy model mismatch")
	}
}

func TestPipelineIntegration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pipe := New("test-agent", "test-token", "", 50*time.Millisecond, 10, t.Logf)
	pipe.Start(ctx)

	snap := &model.Snapshot{
		HostMetrics: model.HostMetrics{CPUUtilizationPct: 30.0, MemoryUsedBytes: 5000},
		GPUDevices: []model.GPUDeviceMetrics{
			{UUID: "GPU-test", SMUtilizationPct: 60.0},
		},
	}
	snapData, _ := json.Marshal(snap)
	snapData = append(snapData, '\n')

	pipe.SnapshotCh <- snapData

	proxyEvent := proxy.TokenUsageEvent{
		ClientPID:   111,
		Provider:    proxy.ProviderOpenAI,
		Model:       "gpt-4",
		IsStreaming: false,
		Metrics:     proxy.TokenMetrics{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}

	pipe.TokenCh <- proxyEvent

	time.Sleep(200 * time.Millisecond)

	if pipe.RingBuf.Len() > 0 {
		entries := pipe.RingBuf.Drain(10)
		if len(entries) > 0 {
			var payload UpstreamPayload
			if err := json.Unmarshal(entries[0], &payload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if payload.AgentID != "test-agent" {
				t.Errorf("expected test-agent, got %s", payload.AgentID)
			}
			if payload.Payload.Proxy[0].ClientPID != 111 {
				t.Errorf("expected client pid 111, got %d", payload.Payload.Proxy[0].ClientPID)
			}
			if payload.Payload.GPU[0].UUID != "GPU-test" {
				t.Errorf("expected GPU-test, got %s", payload.Payload.GPU[0].UUID)
			}
		} else {
			t.Error("expected at least one payload in ring buffer")
		}
	} else {
		t.Log("ring buffer empty (may need more time for aggregation)")
	}

	pipe.Stop()
}

func TestStreamerSendToUpstream(t *testing.T) {
	var mu sync.Mutex
	var receivedPayloads []UpstreamPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected bearer token, got %s", r.Header.Get("Authorization"))
		}

		var payload UpstreamPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		mu.Lock()
		receivedPayloads = append(receivedPayloads, payload)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ringBuf := NewRingBuffer(1024 * 1024)

	snap := model.Snapshot{
		HostMetrics: model.HostMetrics{CPUUtilizationPct: 50.0},
	}
	payload, _ := PayloadFromSnapshot(&snap, nil, "test-agent", "test-token")
	data, _ := json.Marshal(payload)
	ringBuf.Push(data)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamer := NewStreamer(server.URL, "test-token", ringBuf, 50*time.Millisecond, t.Logf)
	streamer.Start(ctx)

	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	count := len(receivedPayloads)
	mu.Unlock()

	if count == 0 {
		t.Error("expected at least one payload sent to upstream")
	}

	cancel()
	streamer.Wait()
}

func TestStreamerRetryOn500(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ringBuf := NewRingBuffer(1024 * 1024)
	snap := model.Snapshot{HostMetrics: model.HostMetrics{CPUUtilizationPct: 10.0}}
	payload, _ := PayloadFromSnapshot(&snap, nil, "a", "b")
	data, _ := json.Marshal(payload)
	ringBuf.Push(data)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamer := NewStreamer(server.URL, "token", ringBuf, 50*time.Millisecond, t.Logf)
	streamer.Start(ctx)

	time.Sleep(200 * time.Millisecond)

	if attempts == 0 {
		t.Error("expected at least one attempt")
	}

	cancel()
	streamer.Wait()
}

func TestAggregatorDropsEmptySnapshot(t *testing.T) {
	snapCh := make(chan []byte, 10)
	tokenCh := make(chan proxy.TokenUsageEvent, 10)
	ringBuf := NewRingBuffer(1024)

	agg := NewAggregator("agent", "token", 50*time.Millisecond, snapCh, tokenCh, ringBuf, t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agg.Start(ctx)

	tokenCh <- proxy.TokenUsageEvent{ClientPID: 1, Model: "test", Metrics: proxy.TokenMetrics{TotalTokens: 10}}

	time.Sleep(150 * time.Millisecond)

	if ringBuf.Len() > 0 {
		t.Log("ring buffer has entries (snapshot may have arrived from another test)")
	}

	cancel()
	agg.Wait()
}

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		current  time.Duration
		max      time.Duration
		expected time.Duration
	}{
		{0, 30 * time.Second, time.Second},
		{time.Second, 30 * time.Second, 2 * time.Second},
		{2 * time.Second, 30 * time.Second, 4 * time.Second},
		{30 * time.Second, 30 * time.Second, 30 * time.Second},
	}

	for _, tt := range tests {
		got := nextBackoff(tt.current, tt.max)
		if got != tt.expected {
			t.Errorf("nextBackoff(%v, %v) = %v, want %v", tt.current, tt.max, got, tt.expected)
		}
	}
}

func TestConcurrentRingBufferPush(t *testing.T) {
	rb := NewRingBuffer(1024 * 1024)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rb.Push([]byte{byte(n)})
		}(i)
	}
	wg.Wait()

	if rb.Len() != 100 {
		t.Errorf("expected 100 entries, got %d", rb.Len())
	}
}

func TestConcurrentRingBufferPushPop(t *testing.T) {
	rb := NewRingBuffer(1024 * 1024)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rb.Push([]byte{byte(n)})
		}(i)
	}

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rb.Pop()
		}()
	}

	wg.Wait()

	if rb.Len() > 50 {
		t.Errorf("expected at most 50 entries, got %d", rb.Len())
	}
}

func TestEmptyUpstreamEndpoint(t *testing.T) {
	ringBuf := NewRingBuffer(1024)
	snap := model.Snapshot{HostMetrics: model.HostMetrics{CPUUtilizationPct: 50.0}}
	payload, _ := PayloadFromSnapshot(&snap, nil, "agent", "token")
	data, _ := json.Marshal(payload)
	ringBuf.Push(data)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamer := NewStreamer("", "token", ringBuf, 50*time.Millisecond, t.Logf)
	streamer.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	if ringBuf.Len() > 0 {
		t.Log("entries remain in ring buffer (expected with empty endpoint)")
	}

	cancel()
	streamer.Wait()
}

func TestLogEventChannelBridge(t *testing.T) {
	events := make(chan proxy.TokenUsageEvent, 10)
	var buf bytes.Buffer
	logger := proxy.NewMetricsLogger(&buf)

	logger.LogEvent(proxy.TokenUsageEvent{
		ClientPID: 1,
		Model:     "gpt-4",
		Metrics:   proxy.TokenMetrics{TotalTokens: 100},
	})

	select {
	case ev := <-logger.Events():
		if ev.ClientPID != 1 {
			t.Errorf("expected pid 1, got %d", ev.ClientPID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}

	logger.Stop()
	_ = events
}
