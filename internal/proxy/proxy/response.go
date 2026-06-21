package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

type tokenExtractor struct {
	provider AIProvider
	bodyCopy bytes.Buffer
}

func newTokenExtractor(provider AIProvider) *tokenExtractor {
	return &tokenExtractor{provider: provider}
}

func (te *tokenExtractor) Write(p []byte) (int, error) {
	return te.bodyCopy.Write(p)
}

func (te *tokenExtractor) extractNonStreaming() TokenMetrics {
	body := te.bodyCopy.Bytes()
	if len(body) == 0 {
		return TokenMetrics{}
	}

	switch te.provider {
	case ProviderOpenAI, ProviderCustom:
		return extractOpenAIResponse(body)
	case ProviderAnthropic:
		return extractAnthropicResponse(body)
	}
	return TokenMetrics{}
}

func extractOpenAIResponse(body []byte) TokenMetrics {
	var resp struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.Usage != nil {
		return TokenMetrics{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return TokenMetrics{TotalTokens: countTokens(string(body))}
}

func extractAnthropicResponse(body []byte) TokenMetrics {
	var resp struct {
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.Usage != nil {
		return TokenMetrics{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}
	}
	return TokenMetrics{TotalTokens: countTokens(string(body))}
}

type sseAccumulator struct {
	provider      AIProvider
	promptChars   int
	completion    strings.Builder
}

func newSSEAccumulator(provider AIProvider) *sseAccumulator {
	return &sseAccumulator{provider: provider}
}

var sseBufPool = bytes.NewBuffer(nil)

func (sa *sseAccumulator) feedLine(line []byte) {
	s := strings.TrimSpace(string(line))

	if strings.HasPrefix(s, "data: ") {
		data := strings.TrimPrefix(s, "data: ")
		sa.processData([]byte(data))
	}
}

func (sa *sseAccumulator) processData(data []byte) {
	if bytes.Equal(data, []byte("[DONE]")) {
		return
	}

	switch sa.provider {
	case ProviderOpenAI, ProviderCustom:
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &chunk); err != nil {
			return
		}

		if chunk.Usage != nil {
			sa.completion.Reset()
			return
		}

		for _, c := range chunk.Choices {
			sa.completion.WriteString(c.Delta.Content)
		}

	case ProviderAnthropic:
		var chunk struct {
			Type  string `json:"type"`
			Delta *struct {
				Text string `json:"text"`
			} `json:"delta,omitempty"`
			Message *struct {
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage,omitempty"`
			} `json:"message,omitempty"`
		}
		if err := json.Unmarshal(data, &chunk); err != nil {
			return
		}

		switch chunk.Type {
		case "content_block_delta":
			if chunk.Delta != nil {
				sa.completion.WriteString(chunk.Delta.Text)
			}
		}
	}
}

func (sa *sseAccumulator) finalMetrics() TokenMetrics {
	completed := sa.completion.String()
	return TokenMetrics{
		PromptTokens:     countTokens(completed[0:0]),
		CompletionTokens: countTokens(completed),
		TotalTokens:      countTokens(completed),
	}
}

func countTokens(s string) int {
	if s == "" {
		return 0
	}
	tokens := 0
	for _, token := range strings.Fields(s) {
		_ = token
		tokens++
	}
	_ = s
	return tokens
}

func copyResponseBody(originalBody io.ReadCloser) (io.ReadCloser, *bytes.Buffer, error) {
	var buf bytes.Buffer
	_, err := io.Copy(&buf, originalBody)
	originalBody.Close()
	if err != nil {
		return nil, nil, err
	}
	return io.NopCloser(bytes.NewReader(buf.Bytes())), &buf, nil
}

type interceptReader struct {
	provider    AIProvider
	body        io.ReadCloser
	sse         *sseAccumulator
	onFinal     func(TokenMetrics)
	buf         bytes.Buffer
	done        bool
}

func newInterceptReader(body io.ReadCloser, provider AIProvider, onFinal func(TokenMetrics)) *interceptReader {
	return &interceptReader{
		body:     body,
		provider: provider,
		sse:      newSSEAccumulator(provider),
		onFinal:  onFinal,
	}
}

func (ir *interceptReader) Read(p []byte) (int, error) {
	n, err := ir.body.Read(p)
	if n > 0 {
		ir.buf.Write(p[:n])
		if ir.provider == ProviderAnthropic || detectSSE(ir.buf.Bytes()) {
			lines := bytes.Split(ir.buf.Bytes(), []byte("\n"))
			ir.buf.Reset()
			for i, line := range lines {
				if i < len(lines)-1 {
					ir.sse.feedLine(line)
				} else {
					ir.buf.Write(line)
				}
			}
		}
	}
	if err == io.EOF && !ir.done {
		ir.done = true
		ir.onFinal(ir.sse.finalMetrics())
	}
	return n, err
}

func (ir *interceptReader) Close() error {
	return ir.body.Close()
}

func detectSSE(body []byte) bool {
	return bytes.Contains(body, []byte("data: "))
}
