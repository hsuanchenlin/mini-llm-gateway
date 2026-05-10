package embed

import (
	"context"
	"hash/fnv"
	"math"
)

// Fake is a deterministic embedder used in tests. It produces an 8-dimensional
// vector seeded by an FNV hash of the input. Same text → same vector → so
// retrieval tests can predict which "documents" rank highest.
type Fake struct{}

func (Fake) Name() string                 { return "fake" }
func (Fake) Dim() int                     { return 8 }
func (Fake) Probe(context.Context) error  { return nil }

func (Fake) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = fakeVector(t, 8)
	}
	return out, nil
}

// fakeVector hashes text into a deterministic unit-norm vector. Two identical
// inputs produce the same vector; similar inputs produce different vectors
// (this is *not* semantically meaningful — just stable for tests).
func fakeVector(text string, dim int) []float32 {
	v := make([]float32, dim)
	for i := 0; i < dim; i++ {
		h := fnv.New32a()
		h.Write([]byte{byte(i)})
		h.Write([]byte(text))
		v[i] = float32(h.Sum32()%2000)/1000 - 1 // in [-1, 1]
	}
	// L2-normalize so cosine similarity behaves nicely.
	var norm float32
	for _, x := range v {
		norm += x * x
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm == 0 {
		return v
	}
	for i := range v {
		v[i] /= norm
	}
	return v
}
