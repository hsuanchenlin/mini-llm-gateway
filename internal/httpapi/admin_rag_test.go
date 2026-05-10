package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mini-llm-gateway/internal/config"
	"mini-llm-gateway/internal/embed"
	"mini-llm-gateway/internal/provider"
	"mini-llm-gateway/internal/rag"
	"mini-llm-gateway/internal/store"
)

// newRAGTestServer wires a Server with a real rag.Service backed by Fake
// embedder + InMemoryStore + tempdir SQLite.
func newRAGTestServer(t *testing.T) (http.Handler, *store.SQLite, *rag.Service) {
	t.Helper()
	repo, err := store.OpenSQLite(filepath.Join(t.TempDir(), "rag.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	vec := rag.NewInMemoryStore()
	_ = vec.EnsureCollection(context.Background(), 8)

	svc := &rag.Service{
		Embedder:  embed.Fake{},
		Vectors:   vec,
		Documents: repo,
		ChunkSize: 200,
		Overlap:   20,
	}

	cfg := config.Config{
		DefaultProvider: "fake",
		DefaultModel:    "fake-1",
		RequestTimeout:  5 * time.Second,
		RAGTopK:         3,
	}
	registry := provider.Registry{"fake": &provider.Fake{}}
	return New(cfg, registry, repo, svc).Handler(), repo, svc
}

func TestAdminCreateDocument(t *testing.T) {
	h, _, svc := newRAGTestServer(t)

	body := `{"title":"Onboarding","body":"Welcome to mini-llm-gateway. The default port is 8090."}`
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/admin/documents", strings.NewReader(body)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Document adminDocument `json:"document"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(resp.Document.ID, "doc-") {
		t.Errorf("doc id = %q", resp.Document.ID)
	}
	if resp.Document.Title != "Onboarding" {
		t.Errorf("title = %q", resp.Document.Title)
	}
	if resp.Document.ChunkCount < 1 {
		t.Errorf("chunk_count = %d, want >= 1", resp.Document.ChunkCount)
	}

	docs, _ := svc.ListDocuments(context.Background(), 10)
	if len(docs) != 1 {
		t.Errorf("listed %d docs, want 1", len(docs))
	}
}

func TestAdminCreateDocumentRequiresFields(t *testing.T) {
	h, _, _ := newRAGTestServer(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/admin/documents", strings.NewReader(`{"title":"x"}`)))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestAdminListAndDeleteDocuments(t *testing.T) {
	h, _, svc := newRAGTestServer(t)
	ctx := context.Background()
	d1, _ := svc.Ingest(ctx, "first", "alpha bravo charlie delta echo foxtrot golf")
	d2, _ := svc.Ingest(ctx, "second", "the quick brown fox jumps over the lazy dog")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin/documents", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d", rr.Code)
	}
	var resp struct {
		Documents []adminDocument `json:"documents"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Documents) != 2 {
		t.Fatalf("listed %d, want 2", len(resp.Documents))
	}

	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest(http.MethodDelete, "/admin/documents/"+d1.ID, nil))
	if rr2.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", rr2.Code)
	}

	rr3 := httptest.NewRecorder()
	h.ServeHTTP(rr3, httptest.NewRequest(http.MethodDelete, "/admin/documents/does-not-exist", nil))
	if rr3.Code != http.StatusNotFound {
		t.Errorf("delete missing status = %d, want 404", rr3.Code)
	}

	// d2 should still be there
	docs, _ := svc.ListDocuments(ctx, 10)
	if len(docs) != 1 || docs[0].ID != d2.ID {
		t.Errorf("after delete, docs = %+v", docs)
	}
}

func TestAdminDocumentEndpointsReturn503WhenRAGDisabled(t *testing.T) {
	h := newTestHandler(t) // built with rag=nil
	for _, c := range []struct {
		method, path string
	}{
		{http.MethodPost, "/admin/documents"},
		{http.MethodGet, "/admin/documents"},
		{http.MethodDelete, "/admin/documents/x"},
	} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(c.method, c.path, strings.NewReader(`{"title":"x","body":"y"}`)))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", c.method, c.path, rr.Code)
		}
	}
}

func TestChatRAGTrueWhenDisabledReturns400(t *testing.T) {
	h := newTestHandler(t) // rag=nil
	body := `{"rag":true,"messages":[{"role":"user","content":"hi"}]}`
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "rag_disabled") {
		t.Errorf("body should mention rag_disabled: %s", rr.Body.String())
	}
}

func TestChatRAGInjectsContextAndLogsChunkIDs(t *testing.T) {
	h, repo, svc := newRAGTestServer(t)
	ctx := context.Background()
	doc, err := svc.Ingest(ctx, "facts", "The default port is 8090. The fake provider always echoes the user message.")
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	_ = doc

	body := `{"rag":true,"messages":[{"role":"user","content":"what is the default port"}]}`
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	// The fake provider echoes the LAST user message (we don't change that).
	// We only need to verify that a system message containing context was prepended,
	// which is observable via the request log entry's RagChunkIDs.
	entries, _ := repo.List(ctx, 10, time.Now().Add(time.Hour))
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].RagChunkIDs == "" {
		t.Errorf("expected non-empty rag_chunk_ids in log; got entry %+v", entries[0])
	}
}
