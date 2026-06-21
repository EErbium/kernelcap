package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestWebhookRegistryCRUD(t *testing.T) {
	r := NewWebhookRegistry()

	cfg := &WebhookConfig{
		TenantID: "tenant-alpha",
		URL:      "https://hooks.example.com/alert",
		Secret:   "sec_123",
		Active:   true,
	}

	if err := r.Add(cfg); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, ok := r.Get("tenant-alpha")
	if !ok {
		t.Fatal("expected to find tenant-alpha")
	}
	if got.URL != "https://hooks.example.com/alert" {
		t.Fatalf("unexpected url: %s", got.URL)
	}

	updated := &WebhookConfig{
		TenantID: "tenant-alpha",
		URL:      "https://hooks.example.com/v2/alert",
		Secret:   "sec_456",
		Active:   false,
	}
	r.Update(updated)

	got, ok = r.Get("tenant-alpha")
	if !ok {
		t.Fatal("expected to find after update")
	}
	if got.URL != "https://hooks.example.com/v2/alert" {
		t.Fatalf("unexpected url after update: %s", got.URL)
	}
	if got.Active {
		t.Fatal("expected inactive after update")
	}

	r.Remove("tenant-alpha")
	_, ok = r.Get("tenant-alpha")
	if ok {
		t.Fatal("expected not found after remove")
	}
}

func TestWebhookRegistryList(t *testing.T) {
	r := NewWebhookRegistry()
	r.Add(&WebhookConfig{TenantID: "t1", URL: "http://a.com", Secret: "s1", Active: true})
	r.Add(&WebhookConfig{TenantID: "t2", URL: "http://b.com", Secret: "s2", Active: false})

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 in list, got %d", len(list))
	}

	for _, cfg := range list {
		if cfg.Secret != "" {
			t.Fatal("secret should be redacted in List()")
		}
	}

	active := r.ListActive()
	if len(active) != 1 {
		t.Fatalf("expected 1 active, got %d", len(active))
	}
	if active[0].TenantID != "t1" {
		t.Fatalf("expected t1 active, got %s", active[0].TenantID)
	}
}

func TestRegistryConcurrent(t *testing.T) {
	r := NewWebhookRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cfg := &WebhookConfig{
				TenantID: fmt.Sprintf("tenant-%d", id),
				URL:      fmt.Sprintf("http://example.com/%d", id),
				Active:   id%2 == 0,
			}
			r.Add(cfg)
		}(i)
	}
	wg.Wait()

	if len(r.List()) != 50 {
		t.Fatalf("expected 50 entries, got %d", len(r.List()))
	}
}

func TestSignPayload(t *testing.T) {
	body := []byte(`{"event":"test","data":"hello"}`)
	secret := "test_secret_key"

	sig := signPayload(body, secret)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	if sig != expected {
		t.Fatalf("signature mismatch:\ngot:  %s\nexp:  %s", sig, expected)
	}

	header := formatSignature(sig)
	if header != "sha256="+expected {
		t.Fatalf("unexpected header format: %s", header)
	}
}

func TestSignPayloadDifferentSecrets(t *testing.T) {
	body := []byte(`{"alert":"critical"}`)
	sig1 := signPayload(body, "secret1")
	sig2 := signPayload(body, "secret2")

	if sig1 == sig2 {
		t.Fatal("signatures should differ for different secrets")
	}
}

func TestSignPayloadDifferentBodies(t *testing.T) {
	sig1 := signPayload([]byte(`{"a":1}`), "secret")
	sig2 := signPayload([]byte(`{"a":2}`), "secret")

	if sig1 == sig2 {
		t.Fatal("signatures should differ for different bodies")
	}
}

func TestWebhookDispatcherDelivered(t *testing.T) {
	var (
		mu     sync.Mutex
		recv   bool
		gotSig string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		recv = true
		gotSig = r.Header.Get("X-AICP-Signature")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.WorkerCount = 1
	registry := NewWebhookRegistry()
	retryCh := make(chan *RetryTask, 10)
	dispatcher := NewWebhookDispatcher(cfg, registry, retryCh, t.Logf)

	dispatcher.Start(context.Background())
	defer dispatcher.Stop()

	body := json.RawMessage(`{"event":"test"}`)
	job := &DispatchJob{
		TenantID: "tenant-dispatch",
		Config: WebhookConfig{
			TenantID: "tenant-dispatch",
			URL:      srv.URL,
			Secret:   "whsec_test",
			Active:   true,
		},
		Body: body,
	}

	dispatcher.Enqueue(job)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	delivered := recv
	sig := gotSig
	mu.Unlock()

	if !delivered {
		t.Fatal("expected webhook to be delivered")
	}
	if sig == "" {
		t.Fatal("expected X-AICP-Signature header")
	}

	select {
	case <-retryCh:
		t.Fatal("expected no retry task for successful delivery")
	default:
	}
}

func TestWebhookDispatcherNon2xx(t *testing.T) {
	var attemptCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.WorkerCount = 1
	registry := NewWebhookRegistry()
	retryCh := make(chan *RetryTask, 10)
	dispatcher := NewWebhookDispatcher(cfg, registry, retryCh, t.Logf)

	dispatcher.Start(context.Background())
	defer dispatcher.Stop()

	body := json.RawMessage(`{"event":"fail"}`)
	job := &DispatchJob{
		TenantID: "tenant-fail",
		Config: WebhookConfig{
			TenantID: "tenant-fail",
			URL:      srv.URL,
			Secret:   "",
			Active:   true,
		},
		Body: body,
	}

	dispatcher.Enqueue(job)

	time.Sleep(200 * time.Millisecond)

	select {
	case task := <-retryCh:
		if task.TenantID != "tenant-fail" {
			t.Fatalf("unexpected tenant: %s", task.TenantID)
		}
		if task.Attempt != 1 {
			t.Fatalf("expected attempt 1, got %d", task.Attempt)
		}
	default:
		t.Fatal("expected retry task for failed delivery")
	}
}

func TestWebhookDispatcherNetworkError(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WorkerCount = 1
	registry := NewWebhookRegistry()
	retryCh := make(chan *RetryTask, 10)
	dispatcher := NewWebhookDispatcher(cfg, registry, retryCh, t.Logf)

	dispatcher.Start(context.Background())
	defer dispatcher.Stop()

	body := json.RawMessage(`{"event":"fail"}`)
	job := &DispatchJob{
		TenantID: "tenant-netfail",
		Config: WebhookConfig{
			TenantID: "tenant-netfail",
			URL:      "http://127.0.0.1:19999/nonexistent",
			Secret:   "",
			Active:   true,
		},
		Body: body,
	}

	dispatcher.Enqueue(job)

	time.Sleep(500 * time.Millisecond)

	select {
	case task := <-retryCh:
		if task.TenantID != "tenant-netfail" {
			t.Fatalf("unexpected tenant: %s", task.TenantID)
		}
	default:
		t.Fatal("expected retry task for network error")
	}
}

func TestRetryQueueExponentialBackoff(t *testing.T) {
	rq := NewRetryQueue(DefaultConfig(), nil, t.Logf)

	delays := make([]time.Duration, 5)
	for i := 1; i <= 5; i++ {
		delays[i-1] = rq.backoff(i)
	}

	for i := 1; i < len(delays); i++ {
		if delays[i] < delays[i-1] {
			t.Logf("delays: %v", delays)
			t.Fatalf("delay should increase: attempt %d (%v) < attempt %d (%v)",
				i+1, delays[i], i, delays[i-1])
		}
	}

	if delays[0] < time.Millisecond {
		t.Fatalf("backoff too small: %v", delays[0])
	}

	maxDelay := DefaultConfig().RetryMaxDelay + time.Duration(float64(DefaultConfig().RetryMaxDelay.Milliseconds()))*time.Millisecond
	if delays[len(delays)-1] > maxDelay+time.Second {
		t.Fatalf("backoff exceeded max delay: %v > %v", delays[len(delays)-1], maxDelay)
	}
}

func TestRetryQueueMaxAttempts(t *testing.T) {
	var attemptCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.RetryMaxAttempts = 3
	cfg.RetryBaseDelay = 10 * time.Millisecond
	cfg.RetryMaxDelay = 100 * time.Millisecond

	taskCh := make(chan *RetryTask, 10)
	rq := NewRetryQueue(cfg, taskCh, t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rq.Start(ctx)
	defer rq.Stop()

	task := &RetryTask{
		TenantID:   "tenant-retry",
		TargetURL:  srv.URL,
		Secret:     "test",
		Body:       []byte(`{"alert":"test"}`),
		Attempt:    0,
		LastError:  "initial",
		LastStatus: 503,
	}

	taskCh <- task

	time.Sleep(2 * time.Second)

	attempts := attemptCount.Load()
	t.Logf("retry attempts: %d", attempts)
	if attempts < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", attempts)
	}
}

func TestSSEClientConnectAndDisconnect(t *testing.T) {
	broker := NewSSEBroker(DefaultConfig(), t.Logf)
	broker.Start(context.Background())
	defer broker.Stop()

	srv := httptest.NewServer(http.HandlerFunc(broker.ServeHTTP))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %s", resp.Header.Get("Content-Type"))
	}

	if broker.ActiveClients() != 1 {
		t.Fatalf("expected 1 active client, got %d", broker.ActiveClients())
	}

	buf := make([]byte, 256)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read: %v", err)
	}
	body := string(buf[:n])
	if !strings.Contains(body, "connected") {
		t.Fatalf("expected connected event, got: %s", body)
	}

	body2 := json.RawMessage(`{"event_id":"evt_001","timestamp":100,"alert_payload":{}}`)
	broker.Broadcast("alert", body2)

	n, err = resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read: %v", err)
	}
	eventBody := string(buf[:n])
	if !strings.Contains(eventBody, "evt_001") {
		t.Fatalf("expected evt_001 in event, got: %s", eventBody)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)

	if broker.ActiveClients() != 0 {
		t.Fatalf("expected 0 active clients after disconnect, got %d", broker.ActiveClients())
	}
}

func TestSSEBroadcastMultipleClients(t *testing.T) {
	broker := NewSSEBroker(DefaultConfig(), t.Logf)
	broker.Start(context.Background())
	defer broker.Stop()

	srv := httptest.NewServer(http.HandlerFunc(broker.ServeHTTP))
	defer srv.Close()

	clientCount := 3
	lines := make([]string, clientCount)
	lineMu := sync.Mutex{}
	var connected sync.WaitGroup
	connected.Add(clientCount)

	for i := 0; i < clientCount; i++ {
		go func(idx int) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("client %d: request: %v", idx, err)
				connected.Done()
				return
			}
			defer resp.Body.Close()

			connected.Done()

			buf := make([]byte, 4096)
			for {
				n, err := resp.Body.Read(buf)
				data := string(buf[:n])
				if strings.Contains(data, "evt_multi") {
					lineMu.Lock()
					lines[idx] = data
					lineMu.Unlock()
					return
				}
				if err != nil {
					return
				}
			}
		}(i)
	}

	connected.Wait()

	eventData := json.RawMessage(`{"event_id":"evt_multi","data":"broadcast"}`)
	broker.Broadcast("alert", eventData)

	time.Sleep(500 * time.Millisecond)

	for i := 0; i < clientCount; i++ {
		lineMu.Lock()
		data := lines[i]
		lineMu.Unlock()
		if data == "" {
			t.Errorf("client %d did not receive event", i)
		} else if !strings.Contains(data, "evt_multi") {
			t.Errorf("client %d missing evt_multi in: %s", i, data)
		}
	}
}

func TestEngineAlertLoop(t *testing.T) {
	alertCh := make(chan json.RawMessage, 10)
	registry := NewWebhookRegistry()

	cfg := DefaultConfig()
	engine := NewWebhookEngine(cfg, registry, alertCh, t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engine.Start(ctx)
	defer engine.Stop()

	alert := json.RawMessage(`{"event_id":"evt_engine","severity":"CRITICAL"}`)
	alertCh <- alert

	time.Sleep(200 * time.Millisecond)
}

func TestEngineAlertWithWebhookDispatch(t *testing.T) {
	var received atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	alertCh := make(chan json.RawMessage, 10)
	registry := NewWebhookRegistry()
	cfg := DefaultConfig()
	cfg.RetryMaxAttempts = 1
	cfg.WorkerCount = 1

	registry.Add(&WebhookConfig{
		TenantID: "tenant-engine-test",
		URL:      srv.URL,
		Secret:   "whsec_engine",
		Active:   true,
	})

	engine := NewWebhookEngine(cfg, registry, alertCh, t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engine.Start(ctx)
	defer engine.Stop()

	alert := json.RawMessage(`{"event_id":"evt_webhook_test","severity":"CRITICAL"}`)
	alertCh <- alert

	time.Sleep(500 * time.Millisecond)

	if !received.Load() {
		t.Fatal("expected webhook to be delivered via engine")
	}
}

func TestEngineAlertWithInactiveConfig(t *testing.T) {
	var received atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	alertCh := make(chan json.RawMessage, 10)
	registry := NewWebhookRegistry()
	cfg := DefaultConfig()
	cfg.WorkerCount = 1

	registry.Add(&WebhookConfig{
		TenantID: "tenant-inactive",
		URL:      srv.URL,
		Secret:   "",
		Active:   false,
	})

	engine := NewWebhookEngine(cfg, registry, alertCh, t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engine.Start(ctx)
	defer engine.Stop()

	alert := json.RawMessage(`{"event_id":"evt_inactive"}`)
	alertCh <- alert

	time.Sleep(300 * time.Millisecond)

	if received.Load() {
		t.Fatal("webhook should not be called for inactive config")
	}
}

func TestEngineAlertChannelClose(t *testing.T) {
	alertCh := make(chan json.RawMessage, 1)
	registry := NewWebhookRegistry()
	cfg := DefaultConfig()

	engine := NewWebhookEngine(cfg, registry, alertCh, t.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engine.Start(ctx)
	alertCh <- json.RawMessage(`{"test":true}`)
	close(alertCh)

	time.Sleep(200 * time.Millisecond)
}

func TestRouteRegistration(t *testing.T) {
	alertCh := make(chan json.RawMessage, 10)
	registry := NewWebhookRegistry()
	engine := NewWebhookEngine(DefaultConfig(), registry, alertCh, t.Logf)

	dummyAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}

	r := chi.NewRouter()
	registrator := engine.RouteRegistrator(dummyAuth, dummyAuth, dummyAuth)
	registrator(r)

	srv := httptest.NewServer(r)
	defer srv.Close()

	t.Run("sse_endpoint", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v2/events/stream")
		if err != nil {
			t.Fatalf("sse request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for SSE, got %d", resp.StatusCode)
		}
		if resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("expected text/event-stream, got %s", resp.Header.Get("Content-Type"))
		}
		resp.Body.Close()
	})

	t.Run("webhook_crud_list", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v2/webhooks/config")
		if err != nil {
			t.Fatalf("list request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for list, got %d", resp.StatusCode)
		}
	})

	t.Run("webhook_crud_create", func(t *testing.T) {
		body := bytes.NewReader([]byte(`{"tenant_id":"route-test","url":"http://example.com/wh","active":true}`))
		resp, err := http.Post(srv.URL+"/api/v2/webhooks/config", "application/json", body)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201 for create, got %d", resp.StatusCode)
		}

		got, ok := registry.Get("route-test")
		if !ok {
			t.Fatal("expected registry to have route-test")
		}
		if got.URL != "http://example.com/wh" {
			t.Fatalf("unexpected url: %s", got.URL)
		}
	})

	t.Run("webhook_crud_get", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v2/webhooks/config/route-test")
		if err != nil {
			t.Fatalf("get request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for get, got %d", resp.StatusCode)
		}
	})

	t.Run("webhook_crud_not_found", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v2/webhooks/config/nonexistent")
		if err != nil {
			t.Fatalf("not found request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 for not found, got %d", resp.StatusCode)
		}
	})

	t.Run("webhook_crud_update", func(t *testing.T) {
		body := bytes.NewReader([]byte(`{"url":"http://example.com/wh2","active":false}`))
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v2/webhooks/config/route-test", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("update request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for update, got %d", resp.StatusCode)
		}

		got, _ := registry.Get("route-test")
		if got.URL != "http://example.com/wh2" {
			t.Fatalf("unexpected url after update: %s", got.URL)
		}
	})

	t.Run("webhook_crud_delete", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v2/webhooks/config/route-test", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("delete request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("expected 204 for delete, got %d", resp.StatusCode)
		}

		if _, ok := registry.Get("route-test"); ok {
			t.Fatal("expected registry to be empty after delete")
		}
	})
}

func TestDispatcherDiagnosticCallback(t *testing.T) {
	var diag atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.WorkerCount = 1
	registry := NewWebhookRegistry()
	retryCh := make(chan *RetryTask, 10)
	dispatcher := NewWebhookDispatcher(cfg, registry, retryCh, t.Logf)

	dispatcher.SetDiagnosticCallback(func(d WebhookDispatchTelemetry) {
		diag.Store(&d)
	})

	dispatcher.Start(context.Background())
	defer dispatcher.Stop()

	job := &DispatchJob{
		TenantID: "tenant-diag",
		Config: WebhookConfig{
			TenantID: "tenant-diag",
			URL:      srv.URL,
			Secret:   "whsec_diag",
			Active:   true,
		},
		Body: json.RawMessage(`{"event":"diag_test"}`),
	}

	dispatcher.Enqueue(job)
	time.Sleep(300 * time.Millisecond)

	d := diag.Load()
	if d == nil {
		t.Fatal("expected diagnostic callback to fire")
	}
	dd := d.(*WebhookDispatchTelemetry)
	if dd.DeliveryStatus != DeliveryStatusDelivered {
		t.Fatalf("expected DELIVERED, got %s", dd.DeliveryStatus)
	}
	if dd.TargetTenantID != "tenant-diag" {
		t.Fatalf("expected tenant-diag, got %s", dd.TargetTenantID)
	}
	if !dd.SignaturePresent {
		t.Fatal("expected signature present")
	}
}

func TestWebhookDispatchTelemetryJSON(t *testing.T) {
	diag := WebhookDispatchTelemetry{
		DispatchTimestamp: 1782352800,
		TargetTenantID:    "tenant-json-test",
		TargetURL:         "https://hooks.example.com/endpoint",
		HTTPStatusCode:    200,
		DeliveryLatencyMs: 12.5,
		DeliveryStatus:    DeliveryStatusDelivered,
		RetryAttempt:      0,
		SignaturePresent:  true,
	}

	data, err := json.Marshal(diag)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["dispatch_timestamp"].(float64) != 1782352800 {
		t.Fatalf("unexpected timestamp")
	}
	if parsed["target_tenant_id"] != "tenant-json-test" {
		t.Fatalf("unexpected tenant")
	}
	if parsed["delivery_status"] != "DELIVERED" {
		t.Fatalf("unexpected status: %s", parsed["delivery_status"])
	}
	if parsed["signature_present"] != true {
		t.Fatal("expected signature present")
	}
}

func TestSSEConnectionTelemetryJSON(t *testing.T) {
	diag := SSEConnectionTelemetry{
		ConnectTimestamp:     1782352800,
		DisconnectTimestamp:  1782352810,
		ClientIP:             "10.0.0.1",
		TenantID:             "tenant-sse-test",
		EventsDispatched:     42,
		ConnectionDurationMs: 10000.5,
	}

	data, err := json.Marshal(diag)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["connect_timestamp"].(float64) != 1782352800 {
		t.Fatalf("unexpected connect timestamp")
	}
	if parsed["events_dispatched"].(float64) != 42 {
		t.Fatalf("unexpected events count")
	}
	if parsed["client_ip"] != "10.0.0.1" {
		t.Fatalf("unexpected client IP")
	}
}


