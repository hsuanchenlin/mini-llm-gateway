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

const testKey = "sk-test-DO-NOT-LEAK-1234567890"

func TestOpenAIHappyPathSendsAuthAndMapsResponse(t *testing.T) {
	var captured openaiRequest
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_, _ = w.Write([]byte(`{
			"model": "gpt-4o-mini",
			"choices": [{"message": {"role": "assistant", "content": "hello!"}}],
			"usage": {"prompt_tokens": 9, "completion_tokens": 13}
		}`))
	}))
	defer srv.Close()

	o := &OpenAI{BaseURL: srv.URL, APIKey: testKey, Client: srv.Client()}
	resp, err := o.Chat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if gotAuth != "Bearer "+testKey {
		t.Errorf("Authorization = %q, want Bearer <key>", gotAuth)
	}
	if captured.Stream {
		t.Errorf("expected stream:false to be sent upstream")
	}
	if resp.Content != "hello!" {
		t.Errorf("content = %q", resp.Content)
	}
	if resp.PromptTokens != 9 || resp.CompletionTokens != 13 {
		t.Errorf("tokens = (%d,%d), want (9,13)", resp.PromptTokens, resp.CompletionTokens)
	}
	if resp.Model != "gpt-4o-mini" {
		t.Errorf("model = %q", resp.Model)
	}
}

func TestOpenAINoKeyOmitsAuthHeader(t *testing.T) {
	authSeen := "_unset_"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authSeen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"model":"x","choices":[{"message":{"role":"assistant","content":"k"}}]}`))
	}))
	defer srv.Close()

	o := &OpenAI{BaseURL: srv.URL, Client: srv.Client()}
	if _, err := o.Chat(context.Background(), ChatRequest{Model: "x", Messages: []Message{{Role: "user", Content: "y"}}}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if authSeen != "" {
		t.Errorf("expected no Authorization header without API key, got %q", authSeen)
	}
}

func TestOpenAIErrorDoesNotLeakAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid model","code":"bad"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	o := &OpenAI{BaseURL: srv.URL, APIKey: testKey, Client: srv.Client()}
	_, err := o.Chat(context.Background(), ChatRequest{Model: "x", Messages: []Message{{Role: "user", Content: "y"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("error message leaked API key: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected status code in error, got %q", err.Error())
	}
}

func TestOpenAIEmptyChoicesIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"x","choices":[]}`))
	}))
	defer srv.Close()

	o := &OpenAI{BaseURL: srv.URL, APIKey: testKey, Client: srv.Client()}
	if _, err := o.Chat(context.Background(), ChatRequest{Model: "x", Messages: []Message{{Role: "user", Content: "y"}}}); err == nil {
		t.Error("expected error for zero choices")
	}
}

func TestOpenAIStreamHappyPath(t *testing.T) {
	var sentBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sentBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Two content chunks, then a usage chunk, then [DONE].
		_, _ = w.Write([]byte(
			`data: {"model":"gpt-4o-mini","choices":[{"delta":{"role":"assistant"}}]}` + "\n\n" +
				`data: {"model":"gpt-4o-mini","choices":[{"delta":{"content":"Hi"}}]}` + "\n\n" +
				`data: {"model":"gpt-4o-mini","choices":[{"delta":{"content":" there"}}]}` + "\n\n" +
				`data: {"model":"gpt-4o-mini","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":6}}` + "\n\n" +
				`data: [DONE]` + "\n\n",
		))
	}))
	defer srv.Close()

	o := &OpenAI{BaseURL: srv.URL, APIKey: testKey, Client: srv.Client()}
	var got string
	summary, err := o.Stream(context.Background(),
		ChatRequest{Model: "gpt-4o-mini", Messages: []Message{{Role: "user", Content: "hi"}}},
		func(d string) error { got += d; return nil })
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "Hi there" {
		t.Errorf("got = %q, want 'Hi there'", got)
	}
	if summary.PromptTokens != 4 || summary.CompletionTokens != 6 {
		t.Errorf("summary tokens = (%d,%d)", summary.PromptTokens, summary.CompletionTokens)
	}
	if summary.Model != "gpt-4o-mini" {
		t.Errorf("summary.Model = %q", summary.Model)
	}

	// Confirm we asked the upstream for include_usage so it sends the usage chunk.
	if !strings.Contains(string(sentBody), `"include_usage":true`) {
		t.Errorf("expected include_usage:true in upstream request body, got %s", sentBody)
	}
	if !strings.Contains(string(sentBody), `"stream":true`) {
		t.Errorf("expected stream:true in upstream request body")
	}
}

func TestOpenAIStreamUpstreamErrorDoesNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad model", http.StatusBadRequest)
	}))
	defer srv.Close()
	o := &OpenAI{BaseURL: srv.URL, APIKey: testKey, Client: srv.Client()}
	_, err := o.Stream(context.Background(),
		ChatRequest{Model: "x", Messages: []Message{{Role: "user", Content: "y"}}},
		func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("API key leaked in stream error: %q", err.Error())
	}
}

func TestOpenAITrimsTrailingSlash(t *testing.T) {
	gotPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"model":"x","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	o := &OpenAI{BaseURL: srv.URL + "/", APIKey: testKey, Client: srv.Client()}
	if _, err := o.Chat(context.Background(), ChatRequest{Model: "x", Messages: []Message{{Role: "user", Content: "y"}}}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
}
