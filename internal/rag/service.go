package rag

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"mini-llm-gateway/internal/embed"
	"mini-llm-gateway/internal/store"
)

// DocumentStore is what Service uses to persist document metadata + body.
// Defined here (not in store package) so tests can mock it; *store.SQLite
// satisfies it implicitly.
type DocumentStore interface {
	SaveDocument(ctx context.Context, d store.Document) error
	GetDocument(ctx context.Context, id string) (store.Document, error)
	ListDocuments(ctx context.Context, limit int) ([]store.Document, error)
	DeleteDocument(ctx context.Context, id string) error
}

// Service ties together: chunker, embedder, vector store, document store.
type Service struct {
	Embedder  embed.Embedder
	Vectors   VectorStore
	Documents DocumentStore
	ChunkSize int
	Overlap   int
}

// Ingest chunks `body`, embeds each chunk, persists the document metadata,
// and upserts the chunk vectors. Returns the saved document.
func (s *Service) Ingest(ctx context.Context, title, body string) (store.Document, error) {
	chunks := Chunk(body, s.ChunkSize, s.Overlap)
	if len(chunks) == 0 {
		return store.Document{}, fmt.Errorf("rag: empty document")
	}
	vecs, err := s.Embedder.Embed(ctx, chunks)
	if err != nil {
		return store.Document{}, fmt.Errorf("rag: embed: %w", err)
	}
	if len(vecs) != len(chunks) {
		return store.Document{}, fmt.Errorf("rag: embedder returned %d vectors for %d chunks", len(vecs), len(chunks))
	}

	doc := store.Document{
		ID:         "doc-" + randomHex(8),
		Title:      title,
		Body:       body,
		ChunkCount: len(chunks),
		CreatedAt:  time.Now(),
	}
	if err := s.Documents.SaveDocument(ctx, doc); err != nil {
		return store.Document{}, fmt.Errorf("rag: save document: %w", err)
	}

	points := make([]Point, len(chunks))
	for i, c := range chunks {
		points[i] = Point{
			ID:         NewUUID(),
			Vector:     vecs[i],
			DocumentID: doc.ID,
			ChunkIndex: i,
			Text:       c,
		}
	}
	if err := s.Vectors.Upsert(ctx, points); err != nil {
		// Best-effort rollback so the doc + vectors don't drift apart.
		_ = s.Documents.DeleteDocument(context.Background(), doc.ID)
		return store.Document{}, fmt.Errorf("rag: upsert vectors: %w", err)
	}
	return doc, nil
}

// Retrieve embeds the query and returns the top-k nearest chunks across all
// documents.
func (s *Service) Retrieve(ctx context.Context, query string, k int) ([]SearchHit, error) {
	if k <= 0 {
		k = 4
	}
	vecs, err := s.Embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("rag: embed query: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("rag: empty query embedding")
	}
	return s.Vectors.Search(ctx, vecs[0], k)
}

// Delete removes a document's chunks from the vector store and its metadata
// from the document store.
func (s *Service) Delete(ctx context.Context, docID string) error {
	if err := s.Vectors.DeleteByDocumentID(ctx, docID); err != nil {
		return fmt.Errorf("rag: delete vectors: %w", err)
	}
	if err := s.Documents.DeleteDocument(ctx, docID); err != nil {
		return fmt.Errorf("rag: delete document: %w", err)
	}
	return nil
}

// ListDocuments is a thin pass-through used by the admin endpoint.
func (s *Service) ListDocuments(ctx context.Context, limit int) ([]store.Document, error) {
	return s.Documents.ListDocuments(ctx, limit)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
