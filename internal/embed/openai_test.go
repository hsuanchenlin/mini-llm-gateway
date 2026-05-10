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

const testKey = "sk-embed-DO-NOT-LEAK-1234"

func TestOpenAIEmbedHappyPath(t *testing.T) {
	var sentReq openaiEmbedRequest
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sentReq)
		// Return out-of-order on purpose so we test the index re-ordering.
		_, _ = w.Write([]byte(`{
			"data": [
				{"index": 1, "embedding": [0.4, 0.5, 0.6]},
				{"index": 0, "embedding": [0.1, 0.2, 0.3]}
			]
		}`))
	}))
	defer srv.Close()

	o := &OpenAI{BaseURL: srv.URL, APIKey: testKey, Model: "text-embedding-3-small", Client: srv.Client()}
	vecs, err := o.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotAuth != "Bearer "+testKey {
		t.Errorf("auth = %q, want Bearer <key>", gotAuth)
	}
	if sentReq.Model != "text-embedding-3-small" {
		t.Errorf("upstream model = %q", sentReq.Model)
	}
	if len(vecs) != 2 {
		t.Fatalf("vecs = %d, want 2", len(vecs))
	}
	if vecs[0][0] != 0.1 || vecs[1][0] != 0.4 {
		t.Errorf("vectors not re-ordered by index: vecs[0]=%v vecs[1]=%v", vecs[0], vecs[1])
	}
	if o.Dim() != 3 {
		t.Errorf("Dim() = %d after first call, want 3", o.Dim())
	}
}

func TestOpenAIEmbedErrorDoesNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad model"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	o := &OpenAI{BaseURL: srv.URL, APIKey: testKey, Model: "x", Client: srv.Client()}
	_, err := o.Embed(context.Background(), []string{"x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("API key leaked in error: %q", err.Error())
	}
}

func TestOpenAIEmbedProbeSetsDim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0,0,0,0,0]}]}`))
	}))
	defer srv.Close()

	o := &OpenAI{BaseURL: srv.URL, APIKey: testKey, Model: "x", Client: srv.Client()}
	if err := o.Probe(context.Background()); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if o.Dim() != 5 {
		t.Errorf("Dim() = %d after probe, want 5", o.Dim())
	}
}
