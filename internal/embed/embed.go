// Package embed turns text into vector embeddings, used by the RAG layer.
//
// Mirrors the design of internal/provider: an Embedder interface plus a
// Registry built from env config. Implementations talk to the same upstreams
// (Ollama, any OpenAI-compatible API).
package embed

import "context"

// Embedder turns a batch of texts into a batch of vectors. Implementations
// preserve input order: out[i] is the embedding of texts[i].
type Embedder interface {
	Name() string
	// Dim returns the dimensionality of vectors this embedder produces.
	// Must be valid after Probe (or after at least one successful Embed call).
	Dim() int
	// Probe makes a tiny test call so we can learn Dim() and fail fast at
	// startup if the upstream is misconfigured.
	Probe(ctx context.Context) error
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}
