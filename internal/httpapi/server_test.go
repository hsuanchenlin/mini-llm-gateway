package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mini-llm-gateway/internal/config"
	"mini-llm-gateway/internal/provider"
	"mini-llm-gateway/internal/store"
)

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Config{
		Port:            0,
		DefaultProvider: "fake",
		DefaultModel:    "fake-1",
		RequestTimeout:  5 * time.Second,
	}
	registry := provider.Registry{"fake": &provider.Fake{}}
	return New(cfg, registry, store.Noop{}, nil).Handler()
}

func TestHealth(t *testing.T) {
	h := newTestHandler(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
}

func TestChatCompletionsHappyPath(t *testing.T) {
	h := newTestHandler(t)
	body, _ := json.Marshal(map[string]any{
		"model": "fake-1",
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int              `json:"index"`
			Message      provider.Message `json:"message"`
			FinishReason string           `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(resp.ID, "chatcmpl-") {
		t.Errorf("id = %q, want chatcmpl- prefix", resp.ID)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", resp.Object)
	}
	if resp.Model != "fake-1" {
		t.Errorf("model = %q, want fake-1", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	c := resp.Choices[0]
	if c.Message.Role != "assistant" {
		t.Errorf("role = %q, want assistant", c.Message.Role)
	}
	if !strings.Contains(c.Message.Content, "ping") {
		t.Errorf("content = %q, want it to echo 'ping'", c.Message.Content)
	}
	if c.FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", c.FinishReason)
	}
	if resp.Usage.TotalTokens == 0 {
		t.Errorf("expected non-zero total_tokens")
	}
}

func TestChatCompletionsUsesDefaultModelWhenOmitted(t *testing.T) {
	h := newTestHandler(t)
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Model != "fake-1" {
		t.Errorf("model = %q, want fake-1 (default)", resp.Model)
	}
}

func TestChatCompletionsRejectsEmptyMessages(t *testing.T) {
	h := newTestHandler(t)
	body, _ := json.Marshal(map[string]any{"model": "fake-1", "messages": []any{}})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestChatCompletionsRejectsMalformedJSON(t *testing.T) {
	h := newTestHandler(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("not json")))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestChatCompletionsUnknownProvider(t *testing.T) {
	h := newTestHandler(t)
	body, _ := json.Marshal(map[string]any{
		"model":    "fake-1",
		"provider": "does-not-exist",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHealthRejectsPost(t *testing.T) {
	h := newTestHandler(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/health", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

// newAuthedTestHandler is a copy of newTestHandler but with AuthToken set.
func newAuthedTestHandler(t *testing.T, token string) http.Handler {
	t.Helper()
	cfg := config.Config{
		Port:            0,
		DefaultProvider: "fake",
		DefaultModel:    "fake-1",
		RequestTimeout:  5 * time.Second,
		AuthToken:       token,
	}
	registry := provider.Registry{"fake": &provider.Fake{}}
	return New(cfg, registry, store.Noop{}, nil).Handler()
}

func TestAuthWhenTokenSet_HealthAndUIStayOpen(t *testing.T) {
	h := newAuthedTestHandler(t, "secret")
	for _, path := range []string{"/health", "/", "/style.css"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200 (open endpoint)", path, rr.Code)
		}
	}
}

func TestAuthWhenTokenSet_ChatRequiresAuth(t *testing.T) {
	h := newAuthedTestHandler(t, "secret")
	body := bytes.NewReader([]byte(`{"messages":[{"role":"user","content":"hi"}]}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no-auth chat status = %d, want 401", rr.Code)
	}
}

func TestAuthWhenTokenSet_AdminRequiresAuth(t *testing.T) {
	h := newAuthedTestHandler(t, "secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin/providers", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no-auth admin status = %d, want 401", rr.Code)
	}
}

func TestAuthWhenTokenSet_CorrectTokenPasses(t *testing.T) {
	h := newAuthedTestHandler(t, "secret")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/providers", nil)
	req.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("authed admin status = %d, want 200", rr.Code)
	}
}
