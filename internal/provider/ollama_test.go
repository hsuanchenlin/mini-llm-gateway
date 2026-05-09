package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaHappyPath(t *testing.T) {
	var captured ollamaRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		_, _ = w.Write([]byte(`{
			"model": "llama2",
			"message": {"role": "assistant", "content": "hi back"},
			"done": true,
			"prompt_eval_count": 7,
			"eval_count": 11
		}`))
	}))
	defer srv.Close()

	o := &Ollama{BaseURL: srv.URL, Client: srv.Client()}
	resp, err := o.Chat(context.Background(), ChatRequest{
		Model: "llama2",
		Messages: []Message{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured.Stream {
		t.Errorf("expected stream:false to be sent upstream")
	}
	if captured.Model != "llama2" {
		t.Errorf("upstream model = %q, want llama2", captured.Model)
	}
	if len(captured.Messages) != 1 || captured.Messages[0].Content != "hi" {
		t.Errorf("upstream messages = %+v", captured.Messages)
	}
	if resp.Content != "hi back" {
		t.Errorf("content = %q, want hi back", resp.Content)
	}
	if resp.Model != "llama2" {
		t.Errorf("model = %q", resp.Model)
	}
	if resp.PromptTokens != 7 || resp.CompletionTokens != 11 {
		t.Errorf("tokens = (%d,%d), want (7,11)", resp.PromptTokens, resp.CompletionTokens)
	}
}

func TestOllamaTrimsTrailingSlash(t *testing.T) {
	gotPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"model":"x","message":{"role":"assistant","content":"ok"},"done":true}`))
	}))
	defer srv.Close()

	o := &Ollama{BaseURL: srv.URL + "/", Client: srv.Client()}
	if _, err := o.Chat(context.Background(), ChatRequest{Model: "x", Messages: []Message{{Role: "user", Content: "y"}}}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotPath != "/api/chat" {
		t.Errorf("path = %q, want /api/chat (trailing slash should not duplicate)", gotPath)
	}
}

func TestOllamaUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	o := &Ollama{BaseURL: srv.URL, Client: srv.Client()}
	_, err := o.Chat(context.Background(), ChatRequest{Model: "missing", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error from 404 upstream")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention status 404, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error should include upstream body snippet, got %q", err.Error())
	}
}

func TestOllamaStreamHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Confirm we're asking for stream:true upstream.
		var sent ollamaRequest
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sent)
		if !sent.Stream {
			t.Errorf("expected stream:true sent upstream, got %+v", sent)
		}
		// Send three NDJSON lines: two content chunks then done.
		_, _ = w.Write([]byte(
			`{"model":"llama2","message":{"role":"assistant","content":"Hello"},"done":false}` + "\n" +
				`{"model":"llama2","message":{"role":"assistant","content":" world"},"done":false}` + "\n" +
				`{"model":"llama2","message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":7,"eval_count":11}` + "\n",
		))
	}))
	defer srv.Close()

	o := &Ollama{BaseURL: srv.URL, Client: srv.Client()}
	var got string
	chunks := 0
	summary, err := o.Stream(context.Background(),
		ChatRequest{Model: "llama2", Messages: []Message{{Role: "user", Content: "hi"}}},
		func(d string) error { got += d; chunks++; return nil })
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "Hello world" {
		t.Errorf("got = %q, want %q", got, "Hello world")
	}
	if chunks != 2 {
		t.Errorf("chunks = %d, want 2 (empty content of done chunk skipped)", chunks)
	}
	if summary.Model != "llama2" {
		t.Errorf("summary.Model = %q", summary.Model)
	}
	if summary.PromptTokens != 7 || summary.CompletionTokens != 11 {
		t.Errorf("summary tokens = (%d,%d)", summary.PromptTokens, summary.CompletionTokens)
	}
}

func TestOllamaStreamUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()
	o := &Ollama{BaseURL: srv.URL, Client: srv.Client()}
	_, err := o.Stream(context.Background(),
		ChatRequest{Model: "x", Messages: []Message{{Role: "user", Content: "y"}}},
		func(string) error {
			t.Errorf("onChunk should not be called on upstream error")
			return nil
		})
	if err == nil {
		t.Fatal("expected error from 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %q, expected status code", err.Error())
	}
}

func TestOllamaContextCanceledBeforeRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream should not be called when ctx is already canceled")
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := &Ollama{BaseURL: srv.URL, Client: srv.Client()}
	if _, err := o.Chat(ctx, ChatRequest{Model: "x", Messages: []Message{{Role: "user", Content: "y"}}}); err == nil {
		t.Error("expected error when ctx canceled")
	}
}
