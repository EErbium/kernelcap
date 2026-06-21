package proxy

import (
	"fmt"
	"io"
	"os"
	"time"
)

type AIProvider string

const (
	ProviderOpenAI    AIProvider = "openai"
	ProviderAnthropic AIProvider = "anthropic"
	ProviderCustom    AIProvider = "custom"
)

type RequestMeta struct {
	Model    string
	Stream   bool
	Metadata map[string]any
}

type TokenMetrics struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type TokenUsageEvent struct {
	Timestamp   int64        `json:"timestamp"`
	ClientPID   int          `json:"client_pid"`
	Provider    AIProvider   `json:"provider"`
	Model       string       `json:"model"`
	IsStreaming bool         `json:"is_streaming"`
	Metrics     TokenMetrics `json:"metrics"`
}

type UpstreamRoute struct {
	HostPattern string
	Provider    AIProvider
	BaseURL     string
}

func DefaultRoutes() []UpstreamRoute {
	return []UpstreamRoute{
		{HostPattern: "api.openai.com", Provider: ProviderOpenAI, BaseURL: "https://api.openai.com"},
		{HostPattern: "api.anthropic.com", Provider: ProviderAnthropic, BaseURL: "https://api.anthropic.com"},
	}
}

func (e TokenUsageEvent) LogLine() string {
	return fmt.Sprintf("timestamp=%d pid=%d provider=%s model=%q streaming=%t prompt_tokens=%d completion_tokens=%d total_tokens=%d\n",
		e.Timestamp, e.ClientPID, e.Provider, e.Model, e.IsStreaming,
		e.Metrics.PromptTokens, e.Metrics.CompletionTokens, e.Metrics.TotalTokens)
}

type MetricsLogger struct {
	events chan TokenUsageEvent
	done   chan struct{}
	w      io.Writer
}

func NewMetricsLogger(w io.Writer) *MetricsLogger {
	if w == nil {
		w = os.Stderr
	}
	ml := &MetricsLogger{
		events: make(chan TokenUsageEvent, 4096),
		done:   make(chan struct{}),
		w:      w,
	}
	go ml.loop()
	return ml
}

func (ml *MetricsLogger) loop() {
	for ev := range ml.events {
		ev.Timestamp = time.Now().Unix()
		line := ev.LogLine()
		ml.w.Write([]byte(line))
	}
	close(ml.done)
}

func (ml *MetricsLogger) Events() <-chan TokenUsageEvent {
	return ml.events
}

func (ml *MetricsLogger) LogEvent(ev TokenUsageEvent) {
	select {
	case ml.events <- ev:
	default:
	}
}

func (ml *MetricsLogger) Stop() {
	close(ml.events)
	<-ml.done
}
