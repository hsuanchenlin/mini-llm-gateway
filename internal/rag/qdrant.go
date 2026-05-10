package rag

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Qdrant is a VectorStore backed by a Qdrant server (REST API on :6333).
//
// Each chunk becomes one Qdrant point; the document_id, chunk_index, and
// original text live in the point's payload so a search returns everything
// in a single round-trip.
type Qdrant struct {
	BaseURL    string
	Collection string
	Client     *http.Client
}

func (q *Qdrant) httpClient() *http.Client {
	if q.Client != nil {
		return q.Client
	}
	return http.DefaultClient
}

func (q *Qdrant) url(path string) string {
	return strings.TrimRight(q.BaseURL, "/") + path
}

// EnsureCollection is idempotent: GET /collections/{name}, create only on 404.
func (q *Qdrant) EnsureCollection(ctx context.Context, dim int) error {
	checkReq, err := http.NewRequestWithContext(ctx, http.MethodGet, q.url("/collections/"+q.Collection), nil)
	if err != nil {
		return fmt.Errorf("qdrant: build check: %w", err)
	}
	resp, err := q.httpClient().Do(checkReq)
	if err != nil {
		return fmt.Errorf("qdrant: check collection: %w", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("qdrant: check collection status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	createBody, _ := json.Marshal(map[string]any{
		"vectors": map[string]any{"size": dim, "distance": "Cosine"},
	})
	createReq, err := http.NewRequestWithContext(ctx, http.MethodPut, q.url("/collections/"+q.Collection), bytes.NewReader(createBody))
	if err != nil {
		return fmt.Errorf("qdrant: build create: %w", err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	cresp, err := q.httpClient().Do(createReq)
	if err != nil {
		return fmt.Errorf("qdrant: create collection: %w", err)
	}
	defer cresp.Body.Close()
	if cresp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(cresp.Body, 512))
		return fmt.Errorf("qdrant: create collection status %d: %s", cresp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

type qdrantPoint struct {
	ID      string         `json:"id"`
	Vector  []float32      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

func (q *Qdrant) Upsert(ctx context.Context, points []Point) error {
	if len(points) == 0 {
		return nil
	}
	qpoints := make([]qdrantPoint, len(points))
	for i, p := range points {
		qpoints[i] = qdrantPoint{
			ID:     p.ID,
			Vector: p.Vector,
			Payload: map[string]any{
				"document_id": p.DocumentID,
				"chunk_index": p.ChunkIndex,
				"text":        p.Text,
			},
		}
	}
	body, _ := json.Marshal(map[string]any{"points": qpoints})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		q.url("/collections/"+q.Collection+"/points?wait=true"),
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("qdrant: build upsert: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := q.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: upsert: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("qdrant: upsert status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

type qdrantSearchResponse struct {
	Result []struct {
		ID      any            `json:"id"`
		Score   float32        `json:"score"`
		Payload map[string]any `json:"payload"`
	} `json:"result"`
}

func (q *Qdrant) Search(ctx context.Context, query []float32, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 5
	}
	body, _ := json.Marshal(map[string]any{
		"vector":       query,
		"limit":        limit,
		"with_payload": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		q.url("/collections/"+q.Collection+"/points/search"),
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("qdrant: build search: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := q.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("qdrant: search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("qdrant: search status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out qdrantSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("qdrant: decode search: %w", err)
	}
	hits := make([]SearchHit, 0, len(out.Result))
	for _, r := range out.Result {
		h := SearchHit{Score: r.Score}
		if id, ok := r.ID.(string); ok {
			h.ID = id
		}
		if did, ok := r.Payload["document_id"].(string); ok {
			h.DocumentID = did
		}
		if ci, ok := r.Payload["chunk_index"].(float64); ok {
			h.ChunkIndex = int(ci)
		}
		if t, ok := r.Payload["text"].(string); ok {
			h.Text = t
		}
		hits = append(hits, h)
	}
	return hits, nil
}

func (q *Qdrant) DeleteByDocumentID(ctx context.Context, docID string) error {
	body, _ := json.Marshal(map[string]any{
		"filter": map[string]any{
			"must": []any{
				map[string]any{
					"key":   "document_id",
					"match": map[string]any{"value": docID},
				},
			},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		q.url("/collections/"+q.Collection+"/points/delete?wait=true"),
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("qdrant: build delete: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := q.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: delete: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("qdrant: delete status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// NewUUID returns a random RFC 4122 v4 UUID. Used as Qdrant point IDs
// (Qdrant accepts UUID strings or unsigned ints).
func NewUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
