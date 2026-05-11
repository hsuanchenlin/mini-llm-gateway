package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"mini-llm-gateway/internal/pricing"
	"mini-llm-gateway/internal/store"
)

type adminEntry struct {
	ID               string `json:"id"`
	Timestamp        string `json:"ts"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	LatencyMs        int64  `json:"latency_ms"`
	StatusCode       int    `json:"status_code"`
	ErrorText        string `json:"error,omitempty"`
	PromptChars      int    `json:"prompt_chars"`
	CompletionChars  int    `json:"completion_chars"`
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	RagChunkIDs      string  `json:"rag_chunk_ids,omitempty"`
	USD              float64 `json:"usd"`
}

type adminRequestsResponse struct {
	Requests   []adminEntry `json:"requests"`
	NextBefore string       `json:"next_before,omitempty"`
}

func (s *Server) handleAdminRequests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be a positive integer")
			return
		}
		limit = n
		if limit > 500 {
			limit = 500
		}
	}

	before := time.Now()
	if v := q.Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "before must be RFC3339")
			return
		}
		before = t
	}

	entries, err := s.repo.List(r.Context(), limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	out := adminRequestsResponse{Requests: make([]adminEntry, 0, len(entries))}
	for _, e := range entries {
		out.Requests = append(out.Requests, adminEntry{
			ID:               e.ID,
			Timestamp:        e.Timestamp.UTC().Format(time.RFC3339Nano),
			Provider:         e.Provider,
			Model:            e.Model,
			LatencyMs:        e.LatencyMs,
			StatusCode:       e.StatusCode,
			ErrorText:        e.ErrorText,
			PromptChars:      e.PromptChars,
			CompletionChars:  e.CompletionChars,
			PromptTokens:     e.PromptTokens,
			CompletionTokens: e.CompletionTokens,
			RagChunkIDs:      e.RagChunkIDs,
			USD:              pricing.USD(e.Model, e.PromptTokens, e.CompletionTokens),
		})
	}

	if len(entries) == limit && len(entries) > 0 {
		oldest := entries[len(entries)-1].Timestamp
		out.NextBefore = oldest.UTC().Format(time.RFC3339Nano)
	}

	writeJSON(w, http.StatusOK, out)
}

type adminProviderInfo struct {
	Name string `json:"name"`
}

type adminProvidersResponse struct {
	DefaultProvider string              `json:"default_provider"`
	DefaultModel    string              `json:"default_model"`
	Providers       []adminProviderInfo `json:"providers"`
}

func (s *Server) handleAdminProviders(w http.ResponseWriter, r *http.Request) {
	names := s.providers.Names()
	sort.Strings(names)
	infos := make([]adminProviderInfo, 0, len(names))
	for _, n := range names {
		infos = append(infos, adminProviderInfo{Name: n})
	}
	writeJSON(w, http.StatusOK, adminProvidersResponse{
		DefaultProvider: s.cfg.DefaultProvider,
		DefaultModel:    s.cfg.DefaultModel,
		Providers:       infos,
	})
}

type adminDocument struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ChunkCount int    `json:"chunk_count"`
	CreatedAt  string `json:"created_at"`
}

func toAdminDoc(d store.Document) adminDocument {
	return adminDocument{
		ID:         d.ID,
		Title:      d.Title,
		ChunkCount: d.ChunkCount,
		CreatedAt:  d.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (s *Server) handleAdminCreateDocument(w http.ResponseWriter, r *http.Request) {
	if s.rag == nil {
		writeError(w, http.StatusServiceUnavailable, "rag_disabled", "RAG is not configured on this gateway")
		return
	}
	var body struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode JSON body")
		return
	}
	if body.Title == "" || body.Body == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "title and body are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
	defer cancel()
	doc, err := s.rag.Ingest(ctx, body.Title, body.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ingest_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"document": toAdminDoc(doc)})
}

func (s *Server) handleAdminListDocuments(w http.ResponseWriter, r *http.Request) {
	if s.rag == nil {
		writeError(w, http.StatusServiceUnavailable, "rag_disabled", "RAG is not configured on this gateway")
		return
	}
	docs, err := s.rag.ListDocuments(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	out := make([]adminDocument, 0, len(docs))
	for _, d := range docs {
		out = append(out, toAdminDoc(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": out})
}

type modelStat struct {
	Model            string  `json:"model"`
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	USD              float64 `json:"usd"`
	PricingKnown     bool    `json:"pricing_known"`
}

type dayStat struct {
	Date             string  `json:"date"`
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	USD              float64 `json:"usd"`
}

type adminStatsResponse struct {
	TotalUSD   float64     `json:"total_usd"`
	TodayUSD   float64     `json:"today_usd"`
	ByModel    []modelStat `json:"by_model"`
	ByDay      []dayStat   `json:"by_day"`
	WindowDays int         `json:"window_days"`
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	windowDays := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 365 {
			windowDays = n
		}
	}

	allTime, err := s.repo.StatsByModel(r.Context(), time.Time{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	startOfToday := time.Now().UTC().Truncate(24 * time.Hour)
	todayByModel, err := s.repo.StatsByModel(r.Context(), startOfToday)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	days, err := s.repo.StatsByDay(r.Context(), windowDays)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	resp := adminStatsResponse{WindowDays: windowDays}
	resp.ByModel = make([]modelStat, 0, len(allTime))
	for _, m := range allTime {
		usd := pricing.USD(m.Model, m.PromptTokens, m.CompletionTokens)
		resp.ByModel = append(resp.ByModel, modelStat{
			Model:            m.Model,
			Requests:         m.Requests,
			PromptTokens:     m.PromptTokens,
			CompletionTokens: m.CompletionTokens,
			USD:              usd,
			PricingKnown:     pricing.Known(m.Model),
		})
		resp.TotalUSD += usd
	}
	for _, m := range todayByModel {
		resp.TodayUSD += pricing.USD(m.Model, m.PromptTokens, m.CompletionTokens)
	}

	resp.ByDay = make([]dayStat, 0, len(days))
	for _, d := range days {
		// Per-day USD requires per-day-per-model totals which we don't query
		// (would need a third aggregate). For now, the per-day chart shows
		// request/token volume; the per-model + total $ figures are accurate.
		resp.ByDay = append(resp.ByDay, dayStat{
			Date:             d.Date,
			Requests:         d.Requests,
			PromptTokens:     d.PromptTokens,
			CompletionTokens: d.CompletionTokens,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminDeleteDocument(w http.ResponseWriter, r *http.Request) {
	if s.rag == nil {
		writeError(w, http.StatusServiceUnavailable, "rag_disabled", "RAG is not configured on this gateway")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "id required")
		return
	}
	err := s.rag.Delete(r.Context(), id)
	if errors.Is(err, store.ErrDocumentNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "document not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
