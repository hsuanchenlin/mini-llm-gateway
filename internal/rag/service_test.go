package rag

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mini-llm-gateway/internal/embed"
	"mini-llm-gateway/internal/store"
)

func newTestService(t *testing.T) (*Service, *store.SQLite, *InMemoryStore) {
	t.Helper()
	repo, err := store.OpenSQLite(filepath.Join(t.TempDir(), "rag.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	vec := NewInMemoryStore()
	_ = vec.EnsureCollection(context.Background(), 8)

	svc := &Service{
		Embedder:  embed.Fake{},
		Vectors:   vec,
		Documents: repo,
		ChunkSize: 30,
		Overlap:   5,
	}
	return svc, repo, vec
}

func TestServiceIngestAndRetrieve(t *testing.T) {
	svc, repo, _ := newTestService(t)
	ctx := context.Background()

	body := strings.Repeat("Mini LLM Gateway is a Go portfolio project. ", 5)
	doc, err := svc.Ingest(ctx, "About", body)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if doc.ID == "" || !strings.HasPrefix(doc.ID, "doc-") {
		t.Errorf("doc id = %q", doc.ID)
	}
	if doc.ChunkCount < 2 {
		t.Errorf("expected multiple chunks, got %d", doc.ChunkCount)
	}

	// Doc round-trips through SQLite
	got, err := repo.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("get document: %v", err)
	}
	if got.Title != "About" || got.Body != body {
		t.Errorf("doc mismatch")
	}

	// Retrieval returns hits, all tagged with this doc's id
	hits, err := svc.Retrieve(ctx, "Mini LLM Gateway", 5)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	for _, h := range hits {
		if h.DocumentID != doc.ID {
			t.Errorf("hit.DocumentID = %q, want %q", h.DocumentID, doc.ID)
		}
		if h.Text == "" {
			t.Errorf("hit text empty")
		}
	}
}

func TestServiceDeleteRemovesFromBothStores(t *testing.T) {
	svc, repo, vec := newTestService(t)
	ctx := context.Background()

	doc, err := svc.Ingest(ctx, "x", strings.Repeat("hello world ", 10))
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if err := svc.Delete(ctx, doc.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetDocument(ctx, doc.ID); !errors.Is(err, store.ErrDocumentNotFound) {
		t.Errorf("expected doc gone from sqlite, got %v", err)
	}
	hits, _ := vec.Search(ctx, []float32{1, 0, 0, 0, 0, 0, 0, 0}, 10)
	for _, h := range hits {
		if h.DocumentID == doc.ID {
			t.Errorf("vector for deleted doc still present: %+v", h)
		}
	}
}

func TestServiceIngestEmptyBody(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.Ingest(context.Background(), "title", ""); err == nil {
		t.Errorf("expected error for empty body")
	}
}

// failingDocStore lets us verify that an upsert failure rolls the doc back.
type failingVecStore struct{ inner VectorStore }

func (f failingVecStore) EnsureCollection(ctx context.Context, dim int) error {
	return f.inner.EnsureCollection(ctx, dim)
}
func (failingVecStore) Upsert(_ context.Context, _ []Point) error {
	return errors.New("simulated upsert failure")
}
func (f failingVecStore) Search(ctx context.Context, q []float32, n int) ([]SearchHit, error) {
	return f.inner.Search(ctx, q, n)
}
func (f failingVecStore) DeleteByDocumentID(ctx context.Context, id string) error {
	return f.inner.DeleteByDocumentID(ctx, id)
}

func TestServiceRollsBackDocOnUpsertFailure(t *testing.T) {
	svc, repo, vec := newTestService(t)
	svc.Vectors = failingVecStore{inner: vec}

	ctx := context.Background()
	_, err := svc.Ingest(ctx, "x", strings.Repeat("hello world ", 10))
	if err == nil {
		t.Fatal("expected ingest to fail")
	}

	docs, _ := repo.ListDocuments(ctx, 10)
	if len(docs) != 0 {
		t.Errorf("expected doc rolled back, but found %d docs", len(docs))
	}
}
