package rag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQdrantEnsureCollectionCreatesWhenMissing(t *testing.T) {
	var calls []string
	var createBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			createBody, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(`{"result":true,"status":"ok"}`))
		}
	}))
	defer srv.Close()

	q := &Qdrant{BaseURL: srv.URL, Collection: "chunks", Client: srv.Client()}
	if err := q.EnsureCollection(context.Background(), 768); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(calls) != 2 || calls[0] != "GET /collections/chunks" || calls[1] != "PUT /collections/chunks" {
		t.Errorf("calls = %v, want [GET, PUT]", calls)
	}
	if !strings.Contains(string(createBody), `"size":768`) || !strings.Contains(string(createBody), `"distance":"Cosine"`) {
		t.Errorf("create body missing fields: %s", createBody)
	}
}

func TestQdrantEnsureCollectionSkipsCreateWhenExists(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		_, _ = w.Write([]byte(`{"result":{"status":"green"}}`))
	}))
	defer srv.Close()

	q := &Qdrant{BaseURL: srv.URL, Collection: "chunks", Client: srv.Client()}
	if err := q.EnsureCollection(context.Background(), 768); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(calls) != 1 || calls[0] != "GET /collections/chunks" {
		t.Errorf("calls = %v, want only GET", calls)
	}
}

func TestQdrantUpsert(t *testing.T) {
	var got struct {
		Points []qdrantPoint `json:"points"`
	}
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"}}`))
	}))
	defer srv.Close()

	q := &Qdrant{BaseURL: srv.URL, Collection: "c", Client: srv.Client()}
	err := q.Upsert(context.Background(), []Point{
		{ID: "u1", Vector: []float32{0.1, 0.2}, DocumentID: "d", ChunkIndex: 3, Text: "hello"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotPath != "/collections/c/points?wait=true" {
		t.Errorf("upsert path = %q", gotPath)
	}
	if len(got.Points) != 1 || got.Points[0].ID != "u1" {
		t.Errorf("upserted points = %+v", got.Points)
	}
	if got.Points[0].Payload["document_id"] != "d" || got.Points[0].Payload["text"] != "hello" {
		t.Errorf("payload = %+v", got.Points[0].Payload)
	}
}

func TestQdrantSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"limit":3`) {
			t.Errorf("expected limit:3 in body, got %s", body)
		}
		if !strings.Contains(string(body), `"with_payload":true`) {
			t.Errorf("expected with_payload:true")
		}
		_, _ = w.Write([]byte(`{
			"result": [
				{"id": "u1", "score": 0.92, "payload": {"document_id": "d1", "chunk_index": 0, "text": "first match"}},
				{"id": "u2", "score": 0.83, "payload": {"document_id": "d1", "chunk_index": 1, "text": "second match"}}
			]
		}`))
	}))
	defer srv.Close()

	q := &Qdrant{BaseURL: srv.URL, Collection: "c", Client: srv.Client()}
	hits, err := q.Search(context.Background(), []float32{0.1, 0.2, 0.3}, 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].ID != "u1" || hits[0].Text != "first match" || hits[0].ChunkIndex != 0 {
		t.Errorf("hits[0] = %+v", hits[0])
	}
	if hits[1].DocumentID != "d1" {
		t.Errorf("payload not parsed: %+v", hits[1])
	}
}

func TestQdrantDeleteByDocumentID(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"result":{"status":"completed"}}`))
	}))
	defer srv.Close()

	q := &Qdrant{BaseURL: srv.URL, Collection: "c", Client: srv.Client()}
	if err := q.DeleteByDocumentID(context.Background(), "doc-xyz"); err != nil {
		t.Fatalf("err: %v", err)
	}
	// {"filter":{"must":[{"key":"document_id","match":{"value":"doc-xyz"}}]}}
	filter, _ := got["filter"].(map[string]any)
	must, _ := filter["must"].([]any)
	if len(must) != 1 {
		t.Fatalf("filter.must = %v, want 1 entry", must)
	}
	cond, _ := must[0].(map[string]any)
	if cond["key"] != "document_id" {
		t.Errorf("filter key = %v", cond["key"])
	}
}

func TestNewUUIDIsValidV4Shape(t *testing.T) {
	id := NewUUID()
	if len(id) != 36 {
		t.Errorf("uuid length = %d, want 36", len(id))
	}
	if id[14] != '4' {
		t.Errorf("expected version-4 marker at index 14, got %q", id[14])
	}
	if id[19] != '8' && id[19] != '9' && id[19] != 'a' && id[19] != 'b' {
		t.Errorf("variant marker at index 19 = %q, want 8/9/a/b", id[19])
	}
}
