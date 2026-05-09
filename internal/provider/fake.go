package provider

import (
	"context"
	"fmt"
	"strings"
)

// Fake is a deterministic provider used for tests and local development.
// If Reply is empty, it echoes the last user message back.
type Fake struct {
	Reply string
}

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	content := f.Reply
	if content == "" {
		var last string
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				last = req.Messages[i].Content
				break
			}
		}
		content = fmt.Sprintf("echo: %s", last)
	}
	return &ChatResponse{
		Model:            req.Model,
		Content:          content,
		PromptTokens:     wordCount(req.Messages),
		CompletionTokens: len(strings.Fields(content)),
	}, nil
}

// Stream chunks the same content Chat would return, one word per delta,
// preserving the spaces between words across chunks.
func (f *Fake) Stream(ctx context.Context, req ChatRequest, onChunk func(string) error) (StreamSummary, error) {
	if err := ctx.Err(); err != nil {
		return StreamSummary{}, err
	}
	content := f.Reply
	if content == "" {
		var last string
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				last = req.Messages[i].Content
				break
			}
		}
		content = "echo: " + last
	}
	words := strings.Fields(content)
	for i, word := range words {
		if err := ctx.Err(); err != nil {
			return StreamSummary{}, err
		}
		delta := word
		if i > 0 {
			delta = " " + word
		}
		if err := onChunk(delta); err != nil {
			return StreamSummary{}, err
		}
	}
	return StreamSummary{
		Model:            req.Model,
		PromptTokens:     wordCount(req.Messages),
		CompletionTokens: len(words),
	}, nil
}

func wordCount(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		n += len(strings.Fields(m.Content))
	}
	return n
}
