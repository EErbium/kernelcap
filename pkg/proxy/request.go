package proxy

import (
	"encoding/json"
	"io"
)

type openAIRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []any  `json:"messages"`
}

type anthropicRequest struct {
	Model      string `json:"model"`
	Stream     bool   `json:"stream"`
	MaxTokens  int    `json:"max_tokens"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func extractRequestMeta(body []byte, provider AIProvider) RequestMeta {
	rm := RequestMeta{}

	switch provider {
	case ProviderOpenAI, ProviderCustom:
		var req openAIRequest
		if err := json.Unmarshal(body, &req); err == nil {
			rm.Model = req.Model
			rm.Stream = req.Stream
		}
	case ProviderAnthropic:
		var req anthropicRequest
		if err := json.Unmarshal(body, &req); err == nil {
			rm.Model = req.Model
			rm.Stream = req.Stream
		}
	}

	return rm
}

func readBody(r io.ReadCloser) ([]byte, error) {
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, 1<<20))
}
