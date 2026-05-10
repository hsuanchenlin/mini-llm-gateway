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

// OpenAI calls any OpenAI-compatible /v1/embeddings endpoint.
//
// APIKey is sent only via the Authorization header; never logged or returned
// in error messages produced by this package.
type OpenAI struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client

	dim int // populated by Probe / first successful Embed
}

func (o *OpenAI) Name() string { return "openai" }
func (o *OpenAI) Dim() int     { return o.dim }

func (o *OpenAI) Probe(ctx context.Context) error {
	vecs, err := o.Embed(ctx, []string{"probe"})
	if err != nil {
		return err
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return fmt.Errorf("openai embed: probe returned %d vectors", len(vecs))
	}
	return nil
}

type openaiEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openaiEmbedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (o *OpenAI) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(openaiEmbedRequest{Model: o.Model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("openai embed: encode request: %w", err)
	}
	url := strings.TrimRight(o.BaseURL, "/") + "/v1/embeddings"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai embed: build request: %w", err)
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
		return nil, fmt.Errorf("openai embed: call upstream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("openai embed: upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}

	var out openaiEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("openai embed: decode response: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("openai embed: got %d embeddings for %d inputs", len(out.Data), len(texts))
	}

	// Re-order by index in case upstream returns out of order.
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vecs) {
			return nil, fmt.Errorf("openai embed: index %d out of range", d.Index)
		}
		vecs[d.Index] = d.Embedding
	}
	if o.dim == 0 && len(vecs[0]) > 0 {
		o.dim = len(vecs[0])
	}
	return vecs, nil
}
