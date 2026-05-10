package embed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaEmbedHappyPath(t *testing.T) {
	var sent ollamaEmbedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sent)
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3,0.4],[0.5,0.6,0.7,0.8]]}`))
	}))
	defer srv.Close()

	o := &Ollama{BaseURL: srv.URL, Model: "nomic-embed-text", Client: srv.Client()}
	vecs, err := o.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sent.Model != "nomic-embed-text" {
		t.Errorf("upstream model = %q", sent.Model)
	}
	if len(sent.Input) != 2 || sent.Input[0] != "first" {
		t.Errorf("upstream input = %v", sent.Input)
	}
	if len(vecs) != 2 || vecs[0][0] != 0.1 || vecs[1][3] != 0.8 {
		t.Errorf("unexpected vectors: %v", vecs)
	}
	if o.Dim() != 4 {
		t.Errorf("Dim() = %d, want 4", o.Dim())
	}
}

func TestOllamaEmbedTrimsTrailingSlash(t *testing.T) {
	gotPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"embeddings":[[0.1]]}`))
	}))
	defer srv.Close()

	o := &Ollama{BaseURL: srv.URL + "/", Model: "x", Client: srv.Client()}
	if _, err := o.Embed(context.Background(), []string{"x"}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotPath != "/api/embed" {
		t.Errorf("path = %q, want /api/embed", gotPath)
	}
}

func TestOllamaEmbedUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	o := &Ollama{BaseURL: srv.URL, Model: "x", Client: srv.Client()}
	_, err := o.Embed(context.Background(), []string{"x"})
	if err == nil {
		t.Fatal("expected error from 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention status 404, got %q", err.Error())
	}
}
