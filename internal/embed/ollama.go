package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Ollama calls the local Ollama server's /api/embed endpoint.
type Ollama struct {
	BaseURL string
	Model   string
	Client  *http.Client

	dim int
}

func (o *Ollama) Name() string { return "ollama" }
func (o *Ollama) Dim() int     { return o.dim }

func (o *Ollama) Probe(ctx context.Context) error {
	vecs, err := o.Embed(ctx, []string{"probe"})
	if err != nil {
		return err
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return fmt.Errorf("ollama embed: probe returned %d vectors", len(vecs))
	}
	return nil
}

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(ollamaEmbedRequest{Model: o.Model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("ollama embed: encode request: %w", err)
	}
	url := strings.TrimRight(o.BaseURL, "/") + "/api/embed"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embed: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: call upstream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("ollama embed: upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}

	var out ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama embed: decode response: %w", err)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embed: got %d embeddings for %d inputs", len(out.Embeddings), len(texts))
	}
	if o.dim == 0 && len(out.Embeddings[0]) > 0 {
		o.dim = len(out.Embeddings[0])
	}
	return out.Embeddings, nil
}
