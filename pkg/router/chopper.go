package router

import (
	"encoding/json"
	"sync"
)

type openAIRequest struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []openAIMessage `json:"messages"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

var requestBufPool = sync.Pool{
	New: func() any { return &openAIRequest{} },
}

func ChopMessages(body []byte, maxMessages, keepRecent, divisor int) ([]byte, int, error) {
	if maxMessages <= 0 {
		maxMessages = 20
	}
	if keepRecent <= 0 {
		keepRecent = 10
	}
	if divisor <= 0 {
		divisor = 4
	}

	req := requestBufPool.Get().(*openAIRequest)
	defer requestBufPool.Put(req)

	if err := json.Unmarshal(body, req); err != nil {
		return body, 0, nil
	}

	if len(req.Messages) <= maxMessages {
		return body, 0, nil
	}

	originalLen := 0
	for _, msg := range req.Messages {
		originalLen += len(msg.Content)
	}

	systemIdx := -1
	for i, msg := range req.Messages {
		if msg.Role == "system" {
			systemIdx = i
			break
		}
	}

	var kept []openAIMessage

	if systemIdx >= 0 {
		kept = append(kept, req.Messages[systemIdx])
		dropped := len(req.Messages) - keepRecent
		if systemIdx < len(req.Messages)-keepRecent {
			dropped = len(req.Messages) - keepRecent - 1
		}
		if dropped < 0 {
			dropped = 0
		}

		tailStart := len(req.Messages) - keepRecent
		if tailStart <= systemIdx {
			tailStart = systemIdx + 1
		}
		for i := tailStart; i < len(req.Messages); i++ {
			kept = append(kept, req.Messages[i])
		}

		if dropped > 0 {
			summary := openAIMessage{
				Role:    "system",
				Content: summaryLine(dropped, originalLen, divisor),
			}
			insertAt := 1
			if insertAt > len(kept) {
				insertAt = len(kept)
			}
			kept = append(kept, openAIMessage{})
			copy(kept[insertAt+1:], kept[insertAt:])
			kept[insertAt] = summary
		}
	} else {
		start := len(req.Messages) - keepRecent
		if start < 0 {
			start = 0
		}
		kept = append(kept, req.Messages[start:]...)

		dropped := len(req.Messages) - keepRecent
		if dropped > 0 {
			summary := openAIMessage{
				Role:    "system",
				Content: summaryLine(dropped, originalLen, divisor),
			}
			kept = append([]openAIMessage{summary}, kept...)
		}
	}

	req.Messages = kept
	modified, err := json.Marshal(req)
	if err != nil {
		return body, 0, nil
	}

	newLen := 0
	for _, msg := range kept {
		newLen += len(msg.Content)
	}

	tokensSaved := (originalLen - newLen) / divisor
	if tokensSaved < 0 {
		tokensSaved = 0
	}

	return modified, tokensSaved, nil
}

func summaryLine(dropped int, droppedChars, divisor int) string {
	estTokens := droppedChars / divisor
	return "[... truncated " + itoa(dropped) + " historical messages (~" + itoa(estTokens) + " tokens) ...]"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 12)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
