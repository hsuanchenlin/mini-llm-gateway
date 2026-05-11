package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"mini-llm-gateway/internal/auth"
	"mini-llm-gateway/internal/config"
	"mini-llm-gateway/internal/provider"
	"mini-llm-gateway/internal/rag"
	"mini-llm-gateway/internal/store"
	"mini-llm-gateway/web"
)

type Server struct {
	cfg       config.Config
	providers provider.Registry
	repo      store.Repository
	rag       *rag.Service // optional; nil disables RAG
}

// New constructs a Server. Pass nil for ragSvc to disable RAG (the rag
// endpoints will then return 503 and chat requests with rag=true will 400).
func New(cfg config.Config, providers provider.Registry, repo store.Repository, ragSvc *rag.Service) *Server {
	return &Server{cfg: cfg, providers: providers, repo: repo, rag: ragSvc}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	requireAuth := auth.RequireBearer(s.cfg.AuthToken)

	// Open endpoints — no auth so monitoring, browser UI loads, and bookmarks work.
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.Handle("GET /", http.FileServerFS(web.FS))

	// Protected endpoints — chat (cost-incurring) + all admin (sensitive).
	mux.Handle("POST /v1/chat/completions", requireAuth(http.HandlerFunc(s.handleChatCompletions)))
	mux.Handle("GET /admin/requests", requireAuth(http.HandlerFunc(s.handleAdminRequests)))
	mux.Handle("GET /admin/providers", requireAuth(http.HandlerFunc(s.handleAdminProviders)))
	mux.Handle("GET /admin/stats", requireAuth(http.HandlerFunc(s.handleAdminStats)))
	mux.Handle("POST /admin/documents", requireAuth(http.HandlerFunc(s.handleAdminCreateDocument)))
	mux.Handle("GET /admin/documents", requireAuth(http.HandlerFunc(s.handleAdminListDocuments)))
	mux.Handle("DELETE /admin/documents/{id}", requireAuth(http.HandlerFunc(s.handleAdminDeleteDocument)))
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
	RAG      bool               `json:"rag"`
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

	if req.RAG {
		if s.rag == nil {
			fail(http.StatusBadRequest, "rag_disabled", "RAG is not configured on this gateway (set GATEWAY_EMBEDDER)")
			return
		}
		var lastUser string
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				lastUser = req.Messages[i].Content
				break
			}
		}
		if lastUser != "" {
			hits, err := s.rag.Retrieve(ctx, lastUser, s.cfg.RAGTopK)
			if err != nil {
				log.Printf("rag: retrieve failed: %v", err)
			} else if len(hits) > 0 {
				sysContent := buildRAGContext(hits)
				req.Messages = append([]provider.Message{{Role: "system", Content: sysContent}}, req.Messages...)
				ids := make([]string, len(hits))
				for i, h := range hits {
					ids[i] = h.ID
				}
				entry.RagChunkIDs = strings.Join(ids, ",")
			}
		}
	}

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

// buildRAGContext formats retrieved chunks into a system-message preamble.
// Document IDs are kept in the output so debugging "which doc did this answer come from"
// is possible from the response alone.
func buildRAGContext(hits []rag.SearchHit) string {
	var sb strings.Builder
	sb.WriteString("Use the following context to answer the user's question. ")
	sb.WriteString("If the answer is not in the context, say so honestly.\n\nContext:\n")
	for _, h := range hits {
		fmt.Fprintf(&sb, "\n[doc=%s chunk=%d]\n%s\n", h.DocumentID, h.ChunkIndex, h.Text)
	}
	return sb.String()
}
