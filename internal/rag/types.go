// Package rag implements retrieval-augmented generation: documents are
// chunked, embedded, and stored in a vector store; chat requests with
// rag=true look up the nearest chunks and prepend them to the prompt.
package rag

import "context"

// Point is one chunk's vector + metadata as it lives in the vector store.
type Point struct {
	ID         string
	Vector     []float32
	DocumentID string
	ChunkIndex int
	Text       string
}

// SearchHit is one match returned from a vector-store search.
type SearchHit struct {
	ID         string
	Score      float32
	DocumentID string
	ChunkIndex int
	Text       string
}

// VectorStore is the interface the RAG service uses to persist and query
// embeddings. Two implementations live in this package: Qdrant (production)
// and InMemoryStore (tests + small deployments without an extra service).
type VectorStore interface {
	EnsureCollection(ctx context.Context, dim int) error
	Upsert(ctx context.Context, points []Point) error
	Search(ctx context.Context, query []float32, limit int) ([]SearchHit, error)
	DeleteByDocumentID(ctx context.Context, docID string) error
}
