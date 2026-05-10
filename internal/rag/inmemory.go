package rag

import (
	"context"
	"math"
	"sort"
	"sync"
)

// InMemoryStore is a VectorStore that keeps everything in RAM and does
// brute-force cosine similarity on every search. Useful for tests and
// for small deployments (<10k chunks). State is lost on restart.
type InMemoryStore struct {
	mu     sync.RWMutex
	points map[string]Point
	dim    int
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{points: map[string]Point{}}
}

func (s *InMemoryStore) EnsureCollection(_ context.Context, dim int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dim = dim
	return nil
}

func (s *InMemoryStore) Upsert(_ context.Context, points []Point) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range points {
		s.points[p.ID] = p
	}
	return nil
}

func (s *InMemoryStore) Search(_ context.Context, query []float32, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 5
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	hits := make([]SearchHit, 0, len(s.points))
	for _, p := range s.points {
		hits = append(hits, SearchHit{
			ID:         p.ID,
			Score:      cosineSim(query, p.Vector),
			DocumentID: p.DocumentID,
			ChunkIndex: p.ChunkIndex,
			Text:       p.Text,
		})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if limit < len(hits) {
		hits = hits[:limit]
	}
	return hits, nil
}

func (s *InMemoryStore) DeleteByDocumentID(_ context.Context, docID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.points {
		if p.DocumentID == docID {
			delete(s.points, id)
		}
	}
	return nil
}

func cosineSim(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / math.Sqrt(na*nb))
}
