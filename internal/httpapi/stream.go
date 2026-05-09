package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"mini-llm-gateway/internal/provider"
	"mini-llm-gateway/internal/store"
)

// streamChunkPayload is one OpenAI-shaped SSE event.
type streamChunkPayload struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []streamChoice `json:"choices"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type streamDelta struct {
	Content string `json:"content,omitempty"`
}

// streamChat handles the stream=true branch of /v1/chat/completions. It
// delays writing response headers until the first chunk so that an upstream
// failure before any output can still surface as a JSON error response.
func (s *Server) streamChat(
	w http.ResponseWriter,
	ctx context.Context,
	p provider.Provider,
	providerName, model, id string,
	started time.Time,
	messages []provider.Message,
	entry *store.Entry,
	fail func(int, string, string),
) {
	streamer, ok := p.(provider.Streamer)
	if !ok {
		fail(http.StatusBadRequest, "invalid_request", "provider "+providerName+" does not support streaming")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(http.StatusInternalServerError, "internal_error", "response writer does not support flushing")
		return
	}

	headersWritten := false
	contentLen := 0

	writeHeadersOnce := func() {
		if headersWritten {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		headersWritten = true
	}

	sendChunk := func(delta string) error {
		writeHeadersOnce()
		chunk := streamChunkPayload{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: started.Unix(),
			Model:   model,
			Choices: []streamChoice{{
				Index: 0,
				Delta: streamDelta{Content: delta},
			}},
		}
		body, _ := json.Marshal(chunk)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
			return err
		}
		flusher.Flush()
		contentLen += len(delta)
		return nil
	}

	summary, err := streamer.Stream(
		ctx,
		provider.ChatRequest{Model: model, Messages: messages},
		sendChunk,
	)
	if err != nil {
		if !headersWritten {
			status := http.StatusBadGateway
			if errors.Is(err, context.DeadlineExceeded) {
				status = http.StatusGatewayTimeout
			}
			fail(status, "provider_error", err.Error())
			return
		}
		errEvent, _ := json.Marshal(map[string]any{
			"error": map[string]string{"type": "provider_error", "message": err.Error()},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", errEvent)
		flusher.Flush()
		entry.StatusCode = http.StatusOK
		entry.ErrorText = err.Error()
		entry.CompletionChars = contentLen
		return
	}

	finalModel := model
	if summary.Model != "" {
		finalModel = summary.Model
	}
	stop := "stop"
	finalChunk := streamChunkPayload{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: started.Unix(),
		Model:   finalModel,
		Choices: []streamChoice{{
			Index:        0,
			Delta:        streamDelta{},
			FinishReason: &stop,
		}},
	}
	finalBody, _ := json.Marshal(finalChunk)
	writeHeadersOnce()
	_, _ = fmt.Fprintf(w, "data: %s\n\n", finalBody)
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	entry.StatusCode = http.StatusOK
	entry.CompletionChars = contentLen
	if summary.Model != "" {
		entry.Model = summary.Model
	}
	entry.PromptTokens = summary.PromptTokens
	entry.CompletionTokens = summary.CompletionTokens
}
