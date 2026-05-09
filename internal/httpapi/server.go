package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"mini-llm-gateway/internal/config"
	"mini-llm-gateway/internal/provider"
	"mini-llm-gateway/internal/store"
	"mini-llm-gateway/web"
)

type Server struct {
	cfg       config.Config
	providers provider.Registry
	repo      store.Repository
}

func New(cfg config.Config, providers provider.Registry, repo store.Repository) *Server {
	return &Server{cfg: cfg, providers: providers, repo: repo}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("GET /admin/requests", s.handleAdminRequests)
	mux.HandleFunc("GET /admin/providers", s.handleAdminProviders)
	mux.Handle("GET /", http.FileServerFS(web.FS))
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// chatRequest mirrors the OpenAI chat completion request, plus a "provider"
// extension so a single endpoint can route across configured backends.
type chatRequest struct {
	Model    string             `json:"model"`
	Messages []provider.Message `json:"messages"`
	Stream   bool               `json:"stream"`
	Provider string             `json:"provider"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

type chatChoice struct {
	Index        int              `json:"index"`
	Message      provider.Message `json:"message"`
	FinishReason string           `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	id := "chatcmpl-" + randomID()

	entry := store.Entry{ID: id, Timestamp: started}
	defer func() {
		entry.LatencyMs = time.Since(started).Milliseconds()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.repo.Log(ctx, entry); err != nil {
			log.Printf("store: log: %v", err)
		}
	}()

	fail := func(status int, code, msg string) {
		entry.StatusCode = status
		entry.ErrorText = msg
		writeError(w, status, code, msg)
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(http.StatusBadRequest, "invalid_request", "could not decode JSON body")
		return
	}
	if len(req.Messages) == 0 {
		fail(http.StatusBadRequest, "invalid_request", "messages must not be empty")
		return
	}

	providerName := req.Provider
	if providerName == "" {
		providerName = s.cfg.DefaultProvider
	}
	entry.Provider = providerName
	p := s.providers.Get(providerName)
	if p == nil {
		fail(http.StatusBadRequest, "invalid_request", "unknown provider: "+providerName)
		return
	}

	model := req.Model
	if model == "" {
		model = s.cfg.DefaultModel
	}
	entry.Model = model

	for _, m := range req.Messages {
		entry.PromptChars += len(m.Content)
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
	defer cancel()

	if req.Stream {
		s.streamChat(w, ctx, p, providerName, model, id, started, req.Messages, &entry, fail)
		return
	}

	resp, err := p.Chat(ctx, provider.ChatRequest{
		Model:    model,
		Messages: req.Messages,
	})
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		fail(status, "provider_error", err.Error())
		return
	}

	entry.Model = resp.Model
	entry.CompletionChars = len(resp.Content)
	entry.PromptTokens = resp.PromptTokens
	entry.CompletionTokens = resp.CompletionTokens
	entry.StatusCode = http.StatusOK

	writeJSON(w, http.StatusOK, chatResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: started.Unix(),
		Model:   resp.Model,
		Choices: []chatChoice{{
			Index:        0,
			Message:      provider.Message{Role: "assistant", Content: resp.Content},
			FinishReason: "stop",
		}},
		Usage: chatUsage{
			PromptTokens:     resp.PromptTokens,
			CompletionTokens: resp.CompletionTokens,
			TotalTokens:      resp.PromptTokens + resp.CompletionTokens,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"type": code, "message": msg},
	})
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
