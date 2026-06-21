package ingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestTenantStoreConcurrency(t *testing.T) {
	store := NewTenantStore()
	var wg sync.WaitGroup
	n := 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token := "token"
			store.AddTenant("tenant", token)
		}(i)
	}
	wg.Wait()

	if store.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", store.Len())
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Lookup("somehash")
		}()
	}
	wg.Wait()
}

func TestTenantStoreAddLookupRemove(t *testing.T) {
	store := NewTenantStore()
	store.AddTenant("tenant-alpha", "super-secret-token")

	hash := sha256Hex("super-secret-token")
	tenantID, ok := store.Lookup(hash)
	if !ok {
		t.Fatal("expected tenant to be found")
	}
	if tenantID != "tenant-alpha" {
		t.Fatalf("expected 'tenant-alpha', got %q", tenantID)
	}

	_, ok = store.Lookup("nonexistent-hash")
	if ok {
		t.Fatal("expected nonexistent hash to not be found")
	}

	store.Remove("tenant-alpha")
	if store.Len() != 0 {
		t.Fatal("expected store to be empty after remove")
	}
}

func TestTenantAuthMiddlewareValid(t *testing.T) {
	store := NewTenantStore()
	store.AddTenant("test-tenant", "valid-token")

	mw := TenantAuthMiddleware(store)

	var capturedTenant, capturedAgent string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTenant, _ = r.Context().Value(ctxKeyTenantID).(string)
		capturedAgent, _ = r.Context().Value(ctxKeyAgentID).(string)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Agent-ID", "agent-01")
	req.Header.Set("Authorization", "Bearer valid-token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if capturedTenant != "test-tenant" {
		t.Fatalf("expected tenant 'test-tenant', got %q", capturedTenant)
	}
	if capturedAgent != "agent-01" {
		t.Fatalf("expected agent 'agent-01', got %q", capturedAgent)
	}
}

func TestTenantAuthMiddlewareMissingHeader(t *testing.T) {
	store := NewTenantStore()
	mw := TenantAuthMiddleware(store)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestTenantAuthMiddlewareBadToken(t *testing.T) {
	store := NewTenantStore()
	store.AddTenant("test-tenant", "valid-token")

	mw := TenantAuthMiddleware(store)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Agent-ID", "agent-01")
	req.Header.Set("Authorization", "Bearer wrong-token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestServerSubmitValid(t *testing.T) {
	store := NewTenantStore()
	store.AddTenant("test-tenant", "test-token-abc")

	received := make(chan *IngestionPayload, 1)
	downstream := func(ctx context.Context, p *IngestionPayload) error {
		received <- p
		return nil
	}

	cfg := DefaultConfig()
	cfg.WorkerCount = 2
	cfg.JobQueueSize = 10

	server := NewIngestionServer(cfg, downstream, t.Logf)
	server.dispatcher.Start()
	defer server.dispatcher.Stop()

	r := chi.NewRouter()
	r.Use(TenantAuthMiddleware(store))
	r.Post("/api/v2/telemetry/submit", server.handleSubmit)

	body := `{"agent_id":"agent-01","auth_token_hash":"abc123","payload":{"cpu":42.0,"memory_used_bytes":1073741824}}`
	req := httptest.NewRequest("POST", "/api/v2/telemetry/submit", bytes.NewReader([]byte(body)))
	req.Header.Set("X-Agent-ID", "agent-01")
	req.Header.Set("Authorization", "Bearer test-token-abc")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case p := <-received:
		if p.AgentPayload.AgentID != "agent-01" {
			t.Fatalf("expected agent_id 'agent-01', got %q", p.AgentPayload.AgentID)
		}
		if p.IngestionMetadata.ResolvedTenantID != "test-tenant" {
			t.Fatalf("expected tenant 'test-tenant', got %q", p.IngestionMetadata.ResolvedTenantID)
		}
		if p.IngestionMetadata.ProcessingWorkerID == 0 {
			t.Fatal("expected non-zero worker ID")
		}
		if p.IngestionMetadata.ReceivedTimestamp == 0 {
			t.Fatal("expected non-zero received timestamp")
		}
		var payloadInner map[string]any
		if err := json.Unmarshal(p.AgentPayload.Payload, &payloadInner); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payloadInner["cpu"] != float64(42.0) {
			t.Fatalf("expected cpu 42.0, got %v", payloadInner["cpu"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for downstream handler")
	}
}

func TestServerSubmitMissingFields(t *testing.T) {
	store := NewTenantStore()
	store.AddTenant("test-tenant", "test-token-abc")

	server := NewIngestionServer(DefaultConfig(), nil, t.Logf)
	server.dispatcher.Start()
	defer server.dispatcher.Stop()

	r := chi.NewRouter()
	r.Use(TenantAuthMiddleware(store))
	r.Post("/api/v2/telemetry/submit", server.handleSubmit)

	tests := []struct {
		name       string
		body       string
		expected   int
		headerID   string
		authHeader string
	}{
		{"empty agent_id", `{"agent_id":"","payload":{"cpu":42}}`, http.StatusUnprocessableEntity, "agent-01", "Bearer test-token-abc"},
		{"missing payload", `{"agent_id":"agent-01"}`, http.StatusUnprocessableEntity, "agent-01", "Bearer test-token-abc"},
		{"empty payload", `{"agent_id":"agent-01","payload":null}`, http.StatusUnprocessableEntity, "agent-01", "Bearer test-token-abc"},
		{"no auth header", `{"agent_id":"agent-01","payload":{"cpu":42}}`, http.StatusUnauthorized, "agent-01", ""},
		{"no agent header", `{"agent_id":"agent-01","payload":{"cpu":42}}`, http.StatusUnauthorized, "", "Bearer test-token-abc"},
		{"agent_id mismatch", `{"agent_id":"different-agent","payload":{"cpu":42}}`, http.StatusForbidden, "agent-01", "Bearer test-token-abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v2/telemetry/submit", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("X-Agent-ID", tt.headerID)
			req.Header.Set("Authorization", tt.authHeader)
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expected {
				t.Fatalf("expected %d, got %d: %s", tt.expected, w.Code, w.Body.String())
			}
		})
	}
}

func TestServerSubmitInvalidJSON(t *testing.T) {
	store := NewTenantStore()
	store.AddTenant("test-tenant", "test-token-abc")

	server := NewIngestionServer(DefaultConfig(), nil, t.Logf)
	server.dispatcher.Start()
	defer server.dispatcher.Stop()

	r := chi.NewRouter()
	r.Use(TenantAuthMiddleware(store))
	r.Post("/api/v2/telemetry/submit", server.handleSubmit)

	req := httptest.NewRequest("POST", "/api/v2/telemetry/submit", bytes.NewReader([]byte(`{invalid json`)))
	req.Header.Set("X-Agent-ID", "agent-01")
	req.Header.Set("Authorization", "Bearer test-token-abc")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServerSubmitBadContentType(t *testing.T) {
	store := NewTenantStore()
	store.AddTenant("test-tenant", "test-token-abc")

	server := NewIngestionServer(DefaultConfig(), nil, t.Logf)
	server.dispatcher.Start()
	defer server.dispatcher.Stop()

	r := chi.NewRouter()
	r.Use(TenantAuthMiddleware(store))
	r.Post("/api/v2/telemetry/submit", server.handleSubmit)

	req := httptest.NewRequest("POST", "/api/v2/telemetry/submit", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Agent-ID", "agent-01")
	req.Header.Set("Authorization", "Bearer test-token-abc")
	req.Header.Set("Content-Type", "text/plain")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServerSubmitOversizedPayload(t *testing.T) {
	store := NewTenantStore()
	store.AddTenant("test-tenant", "test-token-abc")

	cfg := DefaultConfig()
	cfg.MaxPayloadSize = 128
	cfg.WorkerCount = 2
	cfg.JobQueueSize = 10

	server := NewIngestionServer(cfg, nil, t.Logf)
	server.dispatcher.Start()
	defer server.dispatcher.Stop()

	r := chi.NewRouter()
	r.Use(TenantAuthMiddleware(store))
	r.Post("/api/v2/telemetry/submit", server.handleSubmit)

	bigPayload := make([]byte, 256)
	for i := range bigPayload {
		bigPayload[i] = byte('a')
	}
	body := `{"agent_id":"agent-01","payload":"` + string(bigPayload) + `"}`

	req := httptest.NewRequest("POST", "/api/v2/telemetry/submit", bytes.NewReader([]byte(body)))
	req.Header.Set("X-Agent-ID", "agent-01")
	req.Header.Set("Authorization", "Bearer test-token-abc")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDispatcherBasic(t *testing.T) {
	var mu sync.Mutex
	var received []*IngestionPayload

	downstream := func(ctx context.Context, p *IngestionPayload) error {
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		return nil
	}

	cfg := DefaultConfig()
	cfg.WorkerCount = 3
	cfg.JobQueueSize = 100

	dispatcher := NewIngestionDispatcher(cfg, downstream, t.Logf)
	dispatcher.Start()

	for i := 0; i < 10; i++ {
		dispatcher.Dispatch(
			"test-tenant",
			"agent-01",
			"10.0.0.1",
			json.RawMessage(`{"cpu":42}`),
			time.Now(),
		)
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count != 10 {
		t.Fatalf("expected 10 processed jobs, got %d", count)
	}

	dispatcher.Stop()

	mu.Lock()
	if len(received) != 10 {
		t.Fatalf("expected 10 total after stop, got %d", len(received))
	}
	mu.Unlock()
}

func TestDispatcherContextCancellation(t *testing.T) {
	var calls atomic.Int32
	downstream := func(ctx context.Context, p *IngestionPayload) error {
		calls.Add(1)
		return nil
	}

	cfg := DefaultConfig()
	cfg.WorkerCount = 2

	dispatcher := NewIngestionDispatcher(cfg, downstream, t.Logf)
	dispatcher.Start()
	dispatcher.Stop()

	dispatcher.Dispatch("tenant", "agent", "10.0.0.1", json.RawMessage(`{}`), time.Now())

	time.Sleep(50 * time.Millisecond)
	if calls.Load() > 0 {
		t.Fatal("downstream should not be called after stop")
	}
}

func TestTenantAuthMiddlewareInvalidAuthFormat(t *testing.T) {
	store := NewTenantStore()
	store.AddTenant("test-tenant", "valid-token")

	mw := TenantAuthMiddleware(store)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	tests := []struct {
		name       string
		authHeader string
	}{
		{"missing Bearer prefix", "Token valid-token"},
		{"empty token", "Bearer "},
		{"malformed", "Basic dXNlcjpwYXNz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("X-Agent-ID", "agent-01")
			req.Header.Set("Authorization", tt.authHeader)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", w.Code)
			}
		})
	}
}

func TestServerSubmitWithWorkerIDIncrement(t *testing.T) {
	store := NewTenantStore()
	store.AddTenant("test-tenant", "test-token-abc")

	var mu sync.Mutex
	workerIDs := make(map[uint64]int)

	downstream := func(ctx context.Context, p *IngestionPayload) error {
		mu.Lock()
		workerIDs[p.IngestionMetadata.ProcessingWorkerID]++
		mu.Unlock()
		return nil
	}

	cfg := DefaultConfig()
	cfg.WorkerCount = 4
	cfg.JobQueueSize = 100

	server := NewIngestionServer(cfg, downstream, t.Logf)
	server.dispatcher.Start()
	defer server.dispatcher.Stop()

	r := chi.NewRouter()
	r.Use(TenantAuthMiddleware(store))
	r.Post("/api/v2/telemetry/submit", server.handleSubmit)

	for i := 0; i < 20; i++ {
		body := []byte(`{"agent_id":"agent-01","payload":{"i":` + itoa(i) + `}}`)
		req := httptest.NewRequest("POST", "/api/v2/telemetry/submit", bytes.NewReader(body))
		req.Header.Set("X-Agent-ID", "agent-01")
		req.Header.Set("Authorization", "Bearer test-token-abc")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusAccepted {
			t.Fatalf("request %d: expected 202, got %d", i, w.Code)
		}
	}

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	total := 0
	for _, count := range workerIDs {
		total += count
	}
	mu.Unlock()

	if total != 20 {
		t.Fatalf("expected 20 total jobs, got %d", total)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
