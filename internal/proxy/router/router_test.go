package router

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestChopMessages_BelowCap(t *testing.T) {
	body := `{"model":"gpt-4","messages":[{"role":"system","content":"you are helpful"},{"role":"user","content":"hello"}]}`
	modified, saved, err := ChopMessages([]byte(body), 20, 10, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saved != 0 {
		t.Errorf("expected 0 tokens saved, got %d", saved)
	}
	if string(modified) != body {
		t.Errorf("body changed unexpectedly:\ngot:  %s\nwant: %s", string(modified), body)
	}
}

func TestChopMessages_AboveCap(t *testing.T) {
	msgs := `{"role":"system","content":"you are a helpful assistant"}`
	for i := 0; i < 30; i++ {
		msgs += `,{"role":"user","content":"message ` + itoa(i) + `"}`
	}
	body := `{"model":"gpt-4","messages":[` + msgs + `]}`

	modified, saved, err := ChopMessages([]byte(body), 20, 10, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saved <= 0 {
		t.Errorf("expected positive tokens saved, got %d", saved)
	}

	var req openAIRequest
	if err := json.Unmarshal(modified, &req); err != nil {
		t.Fatalf("unmarshal modified: %v", err)
	}
	if len(req.Messages) < 5 {
		t.Errorf("messages too few after chop: %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("first message should preserve system role, got %s", req.Messages[0].Role)
	}
}

func TestChopMessages_EmptyMessages(t *testing.T) {
	body := `{"model":"gpt-4","messages":[]}`
	modified, saved, err := ChopMessages([]byte(body), 20, 10, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saved != 0 {
		t.Errorf("expected 0 tokens saved, got %d", saved)
	}
	if string(modified) != body {
		t.Errorf("body changed unexpectedly: %s", string(modified))
	}
}

func TestChopMessages_NoSystemRole(t *testing.T) {
	msgs := `{"role":"user","content":"first"}`
	for i := 0; i < 25; i++ {
		msgs += `,{"role":"user","content":"msg ` + itoa(i) + `"}`
	}
	body := `{"model":"gpt-4","messages":[` + msgs + `]}`

	modified, saved, err := ChopMessages([]byte(body), 10, 5, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saved <= 0 {
		t.Errorf("expected tokens saved, got %d", saved)
	}

	var req openAIRequest
	if err := json.Unmarshal(modified, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.Messages) < 2 || len(req.Messages) > 8 {
		t.Errorf("unexpected message count: %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("first message should be summary with role system, got %s", req.Messages[0].Role)
	}
}

func TestFallbackRewrite_Model(t *testing.T) {
	fr := NewFallbackRouter("http://127.0.0.1:8000/v1/chat/completions", "meta-llama/Llama-3-8b-Instruct", "local-token")
	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)

	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
	modified, err := fr.RewriteRequest(req, body)
	if err != nil {
		t.Fatalf("RewriteRequest: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(modified, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["model"] != "meta-llama/Llama-3-8b-Instruct" {
		t.Errorf("model = %v, want llama", raw["model"])
	}
}

func TestFallbackRewrite_URL(t *testing.T) {
	fr := NewFallbackRouter("http://127.0.0.1:8000/v1/chat/completions", "llama", "")
	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)

	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
	_, err := fr.RewriteRequest(req, body)
	if err != nil {
		t.Fatalf("RewriteRequest: %v", err)
	}

	if req.URL.String() != "http://127.0.0.1:8000/v1/chat/completions" {
		t.Errorf("URL = %s, want fallback", req.URL.String())
	}
	if req.Host != "127.0.0.1:8000" {
		t.Errorf("Host = %s, want 127.0.0.1:8000", req.Host)
	}
}

func TestFallbackRewrite_AuthHeader(t *testing.T) {
	fr := NewFallbackRouter("http://127.0.0.1:8000/v1/chat/completions", "llama", "local-key-123")
	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)

	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-prod-key")
	_, err := fr.RewriteRequest(req, body)
	if err != nil {
		t.Fatalf("RewriteRequest: %v", err)
	}

	if req.Header.Get("Authorization") != "Bearer local-key-123" {
		t.Errorf("Authorization = %s, want Bearer local-key-123", req.Header.Get("Authorization"))
	}
}

func TestFallbackRewrite_Response(t *testing.T) {
	fr := NewFallbackRouter("http://127.0.0.1:8000/v1/chat/completions", "meta-llama/Llama-3-8b-Instruct", "")

	response := `{"id":"chatcmpl-local","object":"chat.completion","created":1782352800,"model":"meta-llama/Llama-3-8b-Instruct","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`

	rewritten, err := fr.RewriteResponse([]byte(response), "gpt-4o")
	if err != nil {
		t.Fatalf("RewriteResponse: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rewritten, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["model"] != "gpt-4o" {
		t.Errorf("model = %v, want gpt-4o", resp["model"])
	}
}

func TestRegistry_ActivateLookup(t *testing.T) {
	r := NewRegistry(60 * time.Second)

	state := r.Activate(12345, RemedyTokenChop, "gpt-4")
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.PID != 12345 {
		t.Errorf("PID = %d, want 12345", state.PID)
	}
	if state.Remedy != RemedyTokenChop {
		t.Errorf("Remedy = %s, want TOKEN_CHOP", state.Remedy)
	}
	if !state.Active {
		t.Error("expected active")
	}

	lookedUp := r.Lookup(12345)
	if lookedUp == nil {
		t.Fatal("expected to find PID")
	}
	if lookedUp.PID != 12345 {
		t.Errorf("PID mismatch on lookup")
	}
}

func TestRegistry_CoolingOff(t *testing.T) {
	r := NewRegistry(50 * time.Millisecond)

	r.Activate(99999, RemedyFallbackRoute, "gpt-4")

	if r.Lookup(99999) == nil {
		t.Fatal("expected active immediately after activation")
	}

	time.Sleep(100 * time.Millisecond)

	if r.Lookup(99999) != nil {
		t.Error("expected cooled-off after duration")
	}
}

func TestRegistry_RefreshPreventsCooling(t *testing.T) {
	r := NewRegistry(100 * time.Millisecond)

	r.Activate(11111, RemedyTokenChop, "gpt-4")
	time.Sleep(60 * time.Millisecond)
	r.RefreshAlert(11111)

	time.Sleep(60 * time.Millisecond)

	if r.Lookup(11111) == nil {
		t.Error("expected still active after refresh")
	}
}

func TestRegistry_Deactivate(t *testing.T) {
	r := NewRegistry(60 * time.Second)
	r.Activate(1, RemedyTokenChop, "gpt-4")
	r.Deactivate(1)
	if r.Lookup(1) != nil {
		t.Error("expected nil after deactivate")
	}
}

func TestRegistry_RecordTokensSaved(t *testing.T) {
	r := NewRegistry(60 * time.Second)
	r.Activate(42, RemedyTokenChop, "gpt-4")
	r.RecordTokensSaved(42, 1000)
	r.RecordTokensSaved(42, 500)

	total := r.TotalTokensSaved()
	if total != 1500 {
		t.Errorf("TotalTokensSaved = %d, want 1500", total)
	}
}

func TestRegistry_ActiveCount(t *testing.T) {
	r := NewRegistry(60 * time.Second)
	r.Activate(1, RemedyTokenChop, "gpt-4")
	r.Activate(2, RemedyFallbackRoute, "claude")
	r.Activate(3, RemedyBoth, "gpt-4")

	chop, fallback := r.ActiveCount()
	if chop != 2 {
		t.Errorf("chop count = %d, want 2", chop)
	}
	if fallback != 2 {
		t.Errorf("fallback count = %d, want 2", fallback)
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry(10 * time.Second)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pid := int64(1000 + i)
			r.Activate(pid, RemedyTokenChop, "gpt-4")
			r.Lookup(pid)
			r.RecordTokensSaved(pid, i*100)
			r.RefreshAlert(pid)
			r.ActiveCount()
		}(i)
	}
	wg.Wait()

	if r.TotalTokensSaved() <= 0 {
		t.Error("expected positive total tokens saved")
	}
}

func TestRouterEvent_JSONSchema(t *testing.T) {
	evt := RouterEvent{
		MitigationTimestamp: 1782352800,
		InterceptedProcess: InterceptedProcess{
			PID:                 41029,
			OriginalTargetModel: "gpt-4o",
		},
		AppliedRemedy: AppliedRemedy{
			Type: "DYNAMIC_FALLBACK_ROUTING",
			Details: RemedyDetails{
				ReroutedToLocalEndpoint: "http://127.0.0.1:8000/v1",
				SubstitutedModelString:  "meta-llama/Llama-3-8b-Instruct",
				TokensSavedByChopper:    4192,
			},
		},
		ExecutionTelemetry: ExecutionTelemetry{
			ProcessingOverheadMs:   1.12,
			RoutingHandshakeStatus: HandshakeRoutedAndVerified,
		},
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded RouterEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.InterceptedProcess.PID != 41029 {
		t.Errorf("PID mismatch")
	}
	if decoded.AppliedRemedy.Type != "DYNAMIC_FALLBACK_ROUTING" {
		t.Errorf("remedy type mismatch")
	}
	if decoded.ExecutionTelemetry.RoutingHandshakeStatus != HandshakeRoutedAndVerified {
		t.Errorf("handshake status mismatch")
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	required := []string{"mitigation_timestamp", "intercepted_process", "applied_remedy", "execution_telemetry"}
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}
}

func TestInterceptRequest_NoState(t *testing.T) {
	tr := New(DefaultConfig(), func(string, ...any) {})

	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)
	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)

	modified, changed, evt := tr.InterceptRequest(body, req, 0, "gpt-4")
	if changed {
		t.Error("expected unchanged for PID 0")
	}
	if evt != nil {
		t.Error("expected nil event for PID 0")
	}
	if string(modified) != string(body) {
		t.Error("body should be unchanged")
	}
}

func TestInterceptRequest_TokenChop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MaxMessagesBeforeChop = 5
	cfg.KeepRecentMessages = 3
	tr := New(cfg, func(string, ...any) {})

	tr.reg.Activate(42, RemedyTokenChop, "gpt-4")

	msgs := `{"role":"user","content":"hello"}`
	for i := 0; i < 15; i++ {
		msgs += `,{"role":"user","content":"msg ` + itoa(i) + `"}`
	}
	body := []byte(`{"model":"gpt-4","messages":[` + msgs + `]}`)

	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
	modified, changed, evt := tr.InterceptRequest(body, req, 42, "gpt-4")

	if !changed {
		t.Error("expected changed=true")
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}
	if evt.AppliedRemedy.Type != string(RemedyTokenChop) {
		t.Errorf("remedy type = %s, want TOKEN_CHOP", evt.AppliedRemedy.Type)
	}
	if evt.AppliedRemedy.Details.TokensSavedByChopper <= 0 {
		t.Errorf("expected positive tokens saved, got %d", evt.AppliedRemedy.Details.TokensSavedByChopper)
	}

	var req2 openAIRequest
	if err := json.Unmarshal(modified, &req2); err != nil {
		t.Fatalf("unmarshal modified: %v", err)
	}
	if len(req2.Messages) > 8 {
		t.Errorf("too many messages after chop: %d", len(req2.Messages))
	}
}

func TestInterceptRequest_FallbackRoute(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.FallbackEndpoint = "http://127.0.0.1:8000/v1/chat/completions"
	cfg.FallbackModel = "meta-llama/Llama-3-8b-Instruct"
	cfg.FallbackAuthToken = "local-key"
	tr := New(cfg, func(string, ...any) {})

	tr.reg.Activate(77, RemedyFallbackRoute, "gpt-4o")

	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-real")

	_, changed, evt := tr.InterceptRequest(body, req, 77, "gpt-4o")
	if !changed {
		t.Error("expected changed=true")
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}
	if evt.AppliedRemedy.Type != string(RemedyFallbackRoute) {
		t.Errorf("remedy type = %s", evt.AppliedRemedy.Type)
	}
	if req.URL.Host != "127.0.0.1:8000" {
		t.Errorf("URL host = %s, want 127.0.0.1:8000", req.URL.Host)
	}
	if req.Header.Get("Authorization") != "Bearer local-key" {
		t.Errorf("Authorization = %s", req.Header.Get("Authorization"))
	}
}

func TestRecordResponse_Fallback(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	tr := New(cfg, func(string, ...any) {})

	tr.reg.Activate(99, RemedyFallbackRoute, "gpt-4")

	response := `{"id":"chatcmpl-abc","object":"chat.completion","created":1782352800,"model":"meta-llama/Llama-3-8b-Instruct","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`

	rewritten, changed := tr.RecordResponse([]byte(response), 99, "gpt-4")
	if !changed {
		t.Error("expected changed=true")
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rewritten, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["model"] != "gpt-4" {
		t.Errorf("model = %v, want gpt-4", resp["model"])
	}
}

func TestAlertListener_ActivatesState(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	tr := New(cfg, func(string, ...any) {})

	trigger := AlertTrigger{
		PID:         123,
		AnomalyType: "SEMANTIC_REPETITION_LOOP",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alertCh := make(chan AlertTrigger, 1)
	tr.Start(ctx, alertCh)
	alertCh <- trigger
	time.Sleep(100 * time.Millisecond)

	state := tr.reg.Lookup(123)
	if state == nil {
		t.Fatal("expected state to be created after alert")
	}
	if state.Remedy != RemedyTokenChop {
		t.Errorf("remedy = %s, want TOKEN_CHOP", state.Remedy)
	}

	tr.Stop()
}

func TestSnapshot(t *testing.T) {
	r := NewRegistry(60 * time.Second)
	r.Activate(1, RemedyTokenChop, "gpt-4")
	r.Activate(2, RemedyFallbackRoute, "claude")

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Errorf("snapshot length = %d, want 2", len(snap))
	}
}
