package httpapi

import (
	"context"
	"encoding/json"
	"io"
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

// nonStreamingProvider implements provider.Provider but NOT provider.Streamer,
// so we can verify the gateway rejects stream=true against it.
type nonStreamingProvider struct{}

func (nonStreamingProvider) Name() string { return "no-stream" }
func (nonStreamingProvider) Chat(ctx context.Context, req provider.ChatRequest) (*provider.ChatResponse, error) {
	return &provider.ChatResponse{Model: req.Model, Content: "ok"}, nil
}

func newStreamTestServer(t *testing.T, repo store.Repository) *httptest.Server {
	t.Helper()
	cfg := config.Config{
		DefaultProvider: "fake",
		DefaultModel:    "fake-1",
		RequestTimeout:  5 * time.Second,
	}
	registry := provider.Registry{
		"fake":      &provider.Fake{},
		"no-stream": nonStreamingProvider{},
	}
	srv := httptest.NewServer(New(cfg, registry, repo, nil).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func parseSSE(t *testing.T, body string) (chunks []map[string]any, sawDone bool) {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			sawDone = true
			break
		}
		var c map[string]any
		if err := json.Unmarshal([]byte(payload), &c); err != nil {
			t.Errorf("bad chunk JSON: %v (line=%q)", err, line)
			continue
		}
		chunks = append(chunks, c)
	}
	return
}

func TestStreamingChatCompletionsHappyPath(t *testing.T) {
	srv := newStreamTestServer(t, store.Noop{})

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"hello world"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	chunks, sawDone := parseSSE(t, string(body))
	if !sawDone {
		t.Errorf("expected data: [DONE]; body tail: %q", string(body[max(0, len(body)-80):]))
	}
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks (>=2 content + 1 finish), got %d", len(chunks))
	}

	var got string
	var sawFinish bool
	for _, c := range chunks {
		if c["object"] != "chat.completion.chunk" {
			t.Errorf("object = %v, want chat.completion.chunk", c["object"])
		}
		choices, _ := c["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		ch := choices[0].(map[string]any)
		if delta, ok := ch["delta"].(map[string]any); ok {
			if s, ok := delta["content"].(string); ok {
				got += s
			}
		}
		if fr, ok := ch["finish_reason"].(string); ok && fr == "stop" {
			sawFinish = true
		}
	}
	if !sawFinish {
		t.Errorf("expected a chunk with finish_reason=stop")
	}
	if !strings.Contains(got, "echo: hello world") {
		t.Errorf("reconstructed delta = %q, want it to contain 'echo: hello world'", got)
	}
}

func TestStreamingLogsToStore(t *testing.T) {
	repo, err := store.OpenSQLite(filepath.Join(t.TempDir(), "stream.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	srv := newStreamTestServer(t, repo)

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	entries, err := repo.List(context.Background(), 10, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.StatusCode != 200 {
		t.Errorf("status_code = %d, want 200", e.StatusCode)
	}
	if e.Provider != "fake" {
		t.Errorf("provider = %q", e.Provider)
	}
	if e.CompletionChars != len("echo: hello") {
		t.Errorf("completion_chars = %d, want %d (length of streamed content)", e.CompletionChars, len("echo: hello"))
	}
	if e.PromptTokens == 0 || e.CompletionTokens == 0 {
		t.Errorf("expected non-zero token counts in summary, got (%d,%d)", e.PromptTokens, e.CompletionTokens)
	}
	if e.ErrorText != "" {
		t.Errorf("expected no error_text on happy path, got %q", e.ErrorText)
	}
}

func TestStreamingRejectedForNonStreamingProvider(t *testing.T) {
	srv := newStreamTestServer(t, store.Noop{})

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"stream":true,"provider":"no-stream","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json (no SSE for non-streaming reject)", ct)
	}
}
