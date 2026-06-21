package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type FallbackRouter struct {
	mu       sync.RWMutex
	endpoint string
	model    string
	authToken string
}

func NewFallbackRouter(endpoint, model, authToken string) *FallbackRouter {
	return &FallbackRouter{
		endpoint: endpoint,
		model:    model,
		authToken: authToken,
	}
}

func (fr *FallbackRouter) Update(endpoint, model, authToken string) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if endpoint != "" {
		fr.endpoint = endpoint
	}
	if model != "" {
		fr.model = model
	}
	if authToken != "" {
		fr.authToken = authToken
	}
}

func (fr *FallbackRouter) Config() (endpoint, model, authToken string) {
	fr.mu.RLock()
	defer fr.mu.RUnlock()
	return fr.endpoint, fr.model, fr.authToken
}

func (fr *FallbackRouter) RewriteRequest(proxyReq *http.Request, body []byte) ([]byte, error) {
	fr.mu.RLock()
	endpoint := fr.endpoint
	model := fr.model
	authToken := fr.authToken
	fr.mu.RUnlock()

	u, err := url.Parse(endpoint)
	if err != nil {
		return body, nil
	}
	proxyReq.URL = u
	proxyReq.Host = u.Host

	if authToken != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+authToken)
	}

	modifiedBody, err := substituteModel(body, model)
	if err != nil {
		return body, nil
	}

	return modifiedBody, nil
}

func (fr *FallbackRouter) RewriteResponse(originalBody []byte, originalModel string) ([]byte, error) {
	openAIResp := struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int             `json:"index"`
			Message      json.RawMessage `json:"message"`
			FinishReason string          `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	}{}

	if err := json.Unmarshal(originalBody, &openAIResp); err != nil {
		return originalBody, nil
	}

	openAIResp.Model = originalModel
	openAIResp.Object = "chat.completion"

	modified, err := json.Marshal(openAIResp)
	if err != nil {
		return originalBody, nil
	}
	return modified, nil
}

func substituteModel(body []byte, fallbackModel string) ([]byte, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, nil
	}
	raw["model"] = fallbackModel
	modified, err := json.Marshal(raw)
	if err != nil {
		return body, nil
	}
	return modified, nil
}

func extractModel(body []byte) string {
	var raw struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	return raw.Model
}

func ExtractFallbackModelName(body []byte, originalModel string) string {
	m := extractModel(body)
	if m == "" {
		return originalModel
	}
	return m
}

func responseIsOpenAICompatible(body []byte) bool {
	return strings.Contains(string(body), `"choices"`) && strings.Contains(string(body), `"message"`)
}
