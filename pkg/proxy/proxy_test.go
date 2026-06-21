package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractRequestMeta_OpenAI(t *testing.T) {
	body := `{"model":"gpt-4","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	meta := extractRequestMeta([]byte(body), ProviderOpenAI)
	if meta.Model != "gpt-4" {
		t.Errorf("expected gpt-4, got %s", meta.Model)
	}
	if !meta.Stream {
		t.Error("expected stream=true")
	}
}

func TestExtractRequestMeta_OpenAI_NonStreaming(t *testing.T) {
	body := `{"model":"gpt-3.5-turbo","stream":false}`
	meta := extractRequestMeta([]byte(body), ProviderOpenAI)
	if meta.Stream {
		t.Error("expected stream=false")
	}
}

func TestExtractRequestMeta_Anthropic(t *testing.T) {
	body := `{"model":"claude-3-opus-20240229","stream":true,"max_tokens":1024}`
	meta := extractRequestMeta([]byte(body), ProviderAnthropic)
	if meta.Model != "claude-3-opus-20240229" {
		t.Errorf("expected claude-3-opus-20240229, got %s", meta.Model)
	}
	if !meta.Stream {
		t.Error("expected stream=true")
	}
}

func TestExtractRequestMeta_EmptyBody(t *testing.T) {
	meta := extractRequestMeta([]byte{}, ProviderOpenAI)
	if meta.Model != "" {
		t.Errorf("expected empty model, got %s", meta.Model)
	}
}

func TestExtractNonStreaming_OpenAI(t *testing.T) {
	body := `{"id":"chatcmpl-123","usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30},"choices":[{"message":{"content":"hello"}}]}`
	ext := newTokenExtractor(ProviderOpenAI)
	ext.Write([]byte(body))
	metrics := ext.extractNonStreaming()
	if metrics.PromptTokens != 10 {
		t.Errorf("expected 10 prompt tokens, got %d", metrics.PromptTokens)
	}
	if metrics.CompletionTokens != 20 {
		t.Errorf("expected 20 completion tokens, got %d", metrics.CompletionTokens)
	}
	if metrics.TotalTokens != 30 {
		t.Errorf("expected 30 total tokens, got %d", metrics.TotalTokens)
	}
}

func TestExtractNonStreaming_OpenAI_NoUsage(t *testing.T) {
	body := `{"id":"chatcmpl-123","choices":[{"message":{"content":"hello world"}}]}`
	ext := newTokenExtractor(ProviderOpenAI)
	ext.Write([]byte(body))
	metrics := ext.extractNonStreaming()
	if metrics.TotalTokens == 0 {
		t.Errorf("expected non-zero token estimate, got %d", metrics.TotalTokens)
	}
}

func TestExtractNonStreaming_Anthropic(t *testing.T) {
	body := `{"id":"msg_123","usage":{"input_tokens":15,"output_tokens":25},"content":[{"text":"hello"}]}`
	ext := newTokenExtractor(ProviderAnthropic)
	ext.Write([]byte(body))
	metrics := ext.extractNonStreaming()
	if metrics.PromptTokens != 15 {
		t.Errorf("expected 15 prompt tokens, got %d", metrics.PromptTokens)
	}
	if metrics.CompletionTokens != 25 {
		t.Errorf("expected 25 completion tokens, got %d", metrics.CompletionTokens)
	}
	if metrics.TotalTokens != 40 {
		t.Errorf("expected 40 total tokens, got %d", metrics.TotalTokens)
	}
}

func TestSSEAccumulator_OpenAI(t *testing.T) {
	sa := newSSEAccumulator(ProviderOpenAI)
	chunks := []string{
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		`data: {"choices":[{"delta":{"content":" world"}}]}`,
		`data: [DONE]`,
	}
	for _, c := range chunks {
		sa.feedLine([]byte(c))
	}
	metrics := sa.finalMetrics()
	if metrics.CompletionTokens == 0 {
		t.Errorf("expected non-zero completion tokens, got %d", metrics.CompletionTokens)
	}
	_ = metrics
}

func TestSSEAccumulator_Anthropic(t *testing.T) {
	sa := newSSEAccumulator(ProviderAnthropic)
	chunks := []string{
		`data: {"type":"content_block_delta","delta":{"text":"Hello"}}`,
		`data: {"type":"content_block_delta","delta":{"text":" world"}}`,
		`data: {"type":"message_stop"}`,
	}
	for _, c := range chunks {
		sa.feedLine([]byte(c))
	}
	metrics := sa.finalMetrics()
	if metrics.CompletionTokens == 0 {
		t.Errorf("expected non-zero completion tokens, got %d", metrics.CompletionTokens)
	}
}

func TestMatchRoute(t *testing.T) {
	p := NewProxy(ProxyConfig{})
	tests := []struct {
		host     string
		expected AIProvider
	}{
		{"api.openai.com:443", ProviderOpenAI},
		{"api.anthropic.com:443", ProviderAnthropic},
		{"api.openai.com", ProviderOpenAI},
		{"api.anthropic.com", ProviderAnthropic},
		{"unknown.example.com", ""},
	}
	for _, tt := range tests {
		route := p.matchRoute(tt.host)
		if tt.expected == "" {
			if route != nil {
				t.Errorf("expected no route for %s, got %s", tt.host, route.Provider)
			}
		} else if route == nil {
			t.Errorf("expected route for %s, got nil", tt.host)
		} else if route.Provider != tt.expected {
			t.Errorf("expected %s for %s, got %s", tt.expected, tt.host, route.Provider)
		}
	}
}

func TestForwardProxy_NonStreaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "test",
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 20,
				"total_tokens":      30,
			},
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": "hello"}},
			},
		})
	}))
	defer upstream.Close()

	proxy := NewProxy(ProxyConfig{
		LogWriter: io.Discard,
	})
	proxy.routes = []UpstreamRoute{
		{HostPattern: upstream.Listener.Addr().String(), Provider: ProviderOpenAI, BaseURL: upstream.URL},
	}

	reqBody := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "http://"+upstream.Listener.Addr().String()+"/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	proxy.handleProxy(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "prompt_tokens") {
		t.Errorf("response should contain usage: %s", string(body))
	}
}

func TestForwardProxy_Streaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		for _, chunk := range []string{
			`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
			`data: {"choices":[{"delta":{"content":" world"}}]}`,
			`data: [DONE]`,
		} {
			io.WriteString(w, chunk+"\n\n")
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	proxy := NewProxy(ProxyConfig{
		LogWriter: io.Discard,
	})
	proxy.routes = []UpstreamRoute{
		{HostPattern: upstream.Listener.Addr().String(), Provider: ProviderOpenAI, BaseURL: upstream.URL},
	}

	reqBody := `{"model":"gpt-4","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "http://"+upstream.Listener.Addr().String()+"/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	proxy.handleProxy(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Hello") {
		t.Errorf("response should contain streamed content: %s", string(body))
	}
}

func TestReadBody_Limit(t *testing.T) {
	body := io.NopCloser(strings.NewReader(`{"model":"test"}`))
	data, err := readBody(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(data), "test") {
		t.Errorf("expected body content, got %s", string(data))
	}
}

func TestLogEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := NewMetricsLogger(&buf)
	ev := TokenUsageEvent{
		ClientPID:   12345,
		Provider:    ProviderOpenAI,
		Model:       "gpt-4",
		IsStreaming: false,
		Metrics: TokenMetrics{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}
	logger.LogEvent(ev)
	logger.Stop()

	output := buf.String()
	if !strings.Contains(output, "pid=12345") {
		t.Errorf("expected pid=12345 in log, got %s", output)
	}
	if !strings.Contains(output, "prompt_tokens=10") {
		t.Errorf("expected prompt_tokens=10 in log, got %s", output)
	}
}

func TestHandleProxy_UnmatchedRoute(t *testing.T) {
	proxy := NewProxy(ProxyConfig{
		LogWriter: io.Discard,
	})
	proxy.routes = []UpstreamRoute{}

	req := httptest.NewRequest(http.MethodPost, "http://unknown.example.com/v1/chat/completions", nil)
	w := httptest.NewRecorder()

	proxy.handleProxy(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unmatched route, got %d", resp.StatusCode)
	}
	_ = body
}

func TestHandleProxy_Connect(t *testing.T) {
	proxy := NewProxy(ProxyConfig{
		LogWriter: io.Discard,
	})
	req := httptest.NewRequest(http.MethodConnect, "http://api.openai.com:443", nil)
	w := httptest.NewRecorder()

	proxy.handleProxy(w, req)

	resp := w.Result()
	resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for CONNECT, got %d", resp.StatusCode)
	}
}
