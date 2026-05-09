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
	"mini-llm-gateway/internal/provider"
	"mini-llm-gateway/internal/store"
)

func newAdminTestServer(t *testing.T) (http.Handler, *store.SQLite) {
	t.Helper()
	repo, err := store.OpenSQLite(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	cfg := config.Config{
		DefaultProvider: "fake",
		DefaultModel:    "fake-1",
		RequestTimeout:  5 * time.Second,
	}
	registry := provider.Registry{"fake": &provider.Fake{}}
	return New(cfg, registry, repo).Handler(), repo
}

func TestChatCompletionsLogsHappyPath(t *testing.T) {
	h, repo := newAdminTestServer(t)

	rr := httptest.NewRecorder()
	body := `{"messages":[{"role":"user","content":"hello world"}]}`
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	entries, err := repo.List(context.Background(), 10, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("logged %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Provider != "fake" {
		t.Errorf("provider = %q, want fake", e.Provider)
	}
	if e.Model != "fake-1" {
		t.Errorf("model = %q, want fake-1", e.Model)
	}
	if e.StatusCode != 200 {
		t.Errorf("status_code = %d, want 200", e.StatusCode)
	}
	if e.PromptChars != len("hello world") {
		t.Errorf("prompt_chars = %d, want %d", e.PromptChars, len("hello world"))
	}
	if e.CompletionChars == 0 {
		t.Errorf("expected non-zero completion_chars")
	}
	if !strings.HasPrefix(e.ID, "chatcmpl-") {
		t.Errorf("id = %q, want chatcmpl- prefix", e.ID)
	}
	if e.ErrorText != "" {
		t.Errorf("happy path should have no error_text, got %q", e.ErrorText)
	}
}

func TestChatCompletionsLogsValidationFailure(t *testing.T) {
	h, repo := newAdminTestServer(t)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[]}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}

	entries, _ := repo.List(context.Background(), 10, time.Now().Add(time.Hour))
	if len(entries) != 1 {
		t.Fatalf("logged %d entries, want 1", len(entries))
	}
	if entries[0].StatusCode != 400 {
		t.Errorf("logged status = %d, want 400", entries[0].StatusCode)
	}
	if entries[0].ErrorText == "" {
		t.Errorf("expected error_text in failed-request log")
	}
}

func TestAdminRequestsHappyPath(t *testing.T) {
	h, repo := newAdminTestServer(t)
	ctx := context.Background()

	base := time.Now().Add(-1 * time.Hour)
	for i := 0; i < 3; i++ {
		err := repo.Log(ctx, store.Entry{
			ID:         string(rune('a' + i)),
			Timestamp:  base.Add(time.Duration(i) * time.Second),
			Provider:   "fake",
			Model:      "fake-1",
			StatusCode: 200,
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin/requests?limit=10", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var resp adminRequestsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Requests) != 3 {
		t.Fatalf("got %d requests, want 3", len(resp.Requests))
	}
	if resp.NextBefore != "" {
		t.Errorf("next_before = %q, want empty when results < limit", resp.NextBefore)
	}
	if resp.Requests[0].ID != "c" {
		t.Errorf("newest = %q, want c", resp.Requests[0].ID)
	}
}

func TestAdminRequestsPagination(t *testing.T) {
	h, repo := newAdminTestServer(t)
	ctx := context.Background()
	base := time.Now().Add(-1 * time.Hour).Truncate(time.Millisecond)
	for i := 0; i < 5; i++ {
		err := repo.Log(ctx, store.Entry{
			ID:         string(rune('a' + i)),
			Timestamp:  base.Add(time.Duration(i) * time.Second),
			Provider:   "fake",
			Model:      "fake-1",
			StatusCode: 200,
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin/requests?limit=2", nil))
	var page1 adminRequestsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &page1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if len(page1.Requests) != 2 {
		t.Fatalf("page1 = %d, want 2", len(page1.Requests))
	}
	if page1.Requests[0].ID != "e" || page1.Requests[1].ID != "d" {
		t.Errorf("page1 ids = [%s %s], want [e d]", page1.Requests[0].ID, page1.Requests[1].ID)
	}
	if page1.NextBefore == "" {
		t.Fatal("expected next_before when full page returned")
	}

	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/admin/requests?limit=2&before="+page1.NextBefore, nil))
	var page2 adminRequestsResponse
	if err := json.Unmarshal(rr2.Body.Bytes(), &page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if len(page2.Requests) != 2 {
		t.Errorf("page2 = %d, want 2", len(page2.Requests))
	}
	if page2.Requests[0].ID != "c" || page2.Requests[1].ID != "b" {
		t.Errorf("page2 ids = [%s %s], want [c b]", page2.Requests[0].ID, page2.Requests[1].ID)
	}
}

func TestAdminRequestsRejectsBadLimit(t *testing.T) {
	h, _ := newAdminTestServer(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin/requests?limit=abc", nil))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestAdminRequestsRejectsBadBefore(t *testing.T) {
	h, _ := newAdminTestServer(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin/requests?before=not-a-time", nil))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestAdminProviders(t *testing.T) {
	h, _ := newAdminTestServer(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin/providers", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp adminProvidersResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.DefaultProvider != "fake" {
		t.Errorf("default_provider = %q", resp.DefaultProvider)
	}
	if resp.DefaultModel != "fake-1" {
		t.Errorf("default_model = %q", resp.DefaultModel)
	}
	if len(resp.Providers) != 1 || resp.Providers[0].Name != "fake" {
		t.Errorf("providers = %+v", resp.Providers)
	}
}
