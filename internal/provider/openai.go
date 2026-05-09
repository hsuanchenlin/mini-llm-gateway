package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAI calls any OpenAI-compatible /v1/chat/completions endpoint
// (api.openai.com, Groq, Together, Fireworks, local llama.cpp, etc.).
//
// APIKey is sent only via the Authorization header; it never appears in
// error messages or logs produced by this package.
type OpenAI struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func (o *OpenAI) Name() string { return "openai" }

type openaiRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type openaiResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (o *OpenAI) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(openaiRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Stream:   false,
	})
	if err != nil {
		return nil, fmt.Errorf("openai: encode request: %w", err)
	}
	url := strings.TrimRight(o.BaseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: call upstream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("openai: upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}

	var out openaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("openai: upstream returned zero choices")
	}
	return &ChatResponse{
		Model:            out.Model,
		Content:          out.Choices[0].Message.Content,
		PromptTokens:     out.Usage.PromptTokens,
		CompletionTokens: out.Usage.CompletionTokens,
	}, nil
}

type openaiStreamRequest struct {
	Model         string                `json:"model"`
	Messages      []Message             `json:"messages"`
	Stream        bool                  `json:"stream"`
	StreamOptions *openaiStreamOptions  `json:"stream_options,omitempty"`
}

type openaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// Stream parses the OpenAI SSE response from /v1/chat/completions.
// stream_options.include_usage is set so the upstream emits a final usage
// chunk that we can record.
func (o *OpenAI) Stream(ctx context.Context, req ChatRequest, onChunk func(string) error) (StreamSummary, error) {
	body, err := json.Marshal(openaiStreamRequest{
		Model:         req.Model,
		Messages:      req.Messages,
		Stream:        true,
		StreamOptions: &openaiStreamOptions{IncludeUsage: true},
	})
	if err != nil {
		return StreamSummary{}, fmt.Errorf("openai: encode request: %w", err)
	}
	url := strings.TrimRight(o.BaseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return StreamSummary{}, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if o.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return StreamSummary{}, fmt.Errorf("openai: call upstream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return StreamSummary{}, fmt.Errorf("openai: upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}

	summary := StreamSummary{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return summary, fmt.Errorf("openai: decode stream chunk: %w", err)
		}
		if chunk.Model != "" {
			summary.Model = chunk.Model
		}
		if chunk.Usage != nil {
			summary.PromptTokens = chunk.Usage.PromptTokens
			summary.CompletionTokens = chunk.Usage.CompletionTokens
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				if err := onChunk(c.Delta.Content); err != nil {
					return summary, err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return summary, fmt.Errorf("openai: read stream: %w", err)
	}
	return summary, nil
}
