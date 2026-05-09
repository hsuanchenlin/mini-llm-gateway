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

// Ollama calls a local Ollama server's /api/chat endpoint (non-streaming).
type Ollama struct {
	BaseURL string
	Client  *http.Client
}

func (o *Ollama) Name() string { return "ollama" }

type ollamaRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ollamaResponse struct {
	Model           string  `json:"model"`
	Message         Message `json:"message"`
	Done            bool    `json:"done"`
	PromptEvalCount int     `json:"prompt_eval_count"`
	EvalCount       int     `json:"eval_count"`
}

func (o *Ollama) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(ollamaRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Stream:   false,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama: encode request: %w", err)
	}
	url := strings.TrimRight(o.BaseURL, "/") + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: call upstream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("ollama: upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}

	var out ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama: decode response: %w", err)
	}
	return &ChatResponse{
		Model:            out.Model,
		Content:          out.Message.Content,
		PromptTokens:     out.PromptEvalCount,
		CompletionTokens: out.EvalCount,
	}, nil
}

// Stream parses Ollama's NDJSON stream from /api/chat. Each line is a JSON
// object with an incremental message.content fragment, terminated by an
// object with done=true that carries the final usage counts.
func (o *Ollama) Stream(ctx context.Context, req ChatRequest, onChunk func(string) error) (StreamSummary, error) {
	body, err := json.Marshal(ollamaRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Stream:   true,
	})
	if err != nil {
		return StreamSummary{}, fmt.Errorf("ollama: encode request: %w", err)
	}
	url := strings.TrimRight(o.BaseURL, "/") + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return StreamSummary{}, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return StreamSummary{}, fmt.Errorf("ollama: call upstream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return StreamSummary{}, fmt.Errorf("ollama: upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}

	summary := StreamSummary{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var sl struct {
			Model           string `json:"model"`
			Message         struct {
				Content string `json:"content"`
			} `json:"message"`
			Done            bool `json:"done"`
			PromptEvalCount int  `json:"prompt_eval_count"`
			EvalCount       int  `json:"eval_count"`
		}
		if err := json.Unmarshal(line, &sl); err != nil {
			return summary, fmt.Errorf("ollama: decode stream line: %w", err)
		}
		if sl.Model != "" {
			summary.Model = sl.Model
		}
		if sl.Message.Content != "" {
			if err := onChunk(sl.Message.Content); err != nil {
				return summary, err
			}
		}
		if sl.Done {
			summary.PromptTokens = sl.PromptEvalCount
			summary.CompletionTokens = sl.EvalCount
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return summary, fmt.Errorf("ollama: read stream: %w", err)
	}
	return summary, nil
}
