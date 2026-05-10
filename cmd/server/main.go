package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mini-llm-gateway/internal/config"
	"mini-llm-gateway/internal/embed"
	"mini-llm-gateway/internal/httpapi"
	"mini-llm-gateway/internal/provider"
	"mini-llm-gateway/internal/rag"
	"mini-llm-gateway/internal/store"
)

func main() {
	cfg := config.FromEnv()

	registry, err := provider.BuildRegistry(provider.Spec{
		Names:         cfg.Providers,
		OllamaBaseURL: cfg.OllamaBaseURL,
		OpenAIBaseURL: cfg.OpenAIBaseURL,
		OpenAIAPIKey:  cfg.OpenAIAPIKey,
		HTTPClient:    &http.Client{Timeout: cfg.RequestTimeout},
	})
	if err != nil {
		log.Fatalf("provider config: %v", err)
	}
	if registry.Get(cfg.DefaultProvider) == nil {
		log.Fatalf("provider config: GATEWAY_DEFAULT_PROVIDER=%q is not in GATEWAY_PROVIDERS=%v",
			cfg.DefaultProvider, cfg.Providers)
	}

	repo, err := store.OpenSQLite(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer repo.Close()

	ragSvc, err := buildRAGService(cfg, repo)
	if err != nil {
		log.Fatalf("rag: %v", err)
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           httpapi.New(cfg, registry, repo, ragSvc).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		ragStatus := "off"
		if ragSvc != nil {
			ragStatus = fmt.Sprintf("%s(embedder=%s,store=%s)", "on", cfg.Embedder, cfg.VectorStore)
		}
		log.Printf("mini-llm-gateway listening on %s (providers=%v default=%s model=%s db=%s rag=%s)",
			srv.Addr, registry.Names(), cfg.DefaultProvider, cfg.DefaultModel, cfg.DBPath, ragStatus)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

// buildRAGService returns nil (and no error) when RAG is disabled. It probes
// the embedder so we know its dim, then ensures the vector store collection
// exists at that dim before serving requests.
func buildRAGService(cfg config.Config, repo *store.SQLite) (*rag.Service, error) {
	emb, err := embed.Build(embed.Spec{
		Name:          cfg.Embedder,
		OllamaBaseURL: cfg.OllamaBaseURL,
		OllamaModel:   cfg.OllamaEmbedModel,
		OpenAIBaseURL: cfg.OpenAIBaseURL,
		OpenAIAPIKey:  cfg.OpenAIAPIKey,
		OpenAIModel:   cfg.OpenAIEmbedModel,
		HTTPClient:    &http.Client{Timeout: cfg.RequestTimeout},
	})
	if err != nil {
		return nil, fmt.Errorf("embedder: %w", err)
	}
	if emb == nil {
		return nil, nil // RAG disabled
	}

	probeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := emb.Probe(probeCtx); err != nil {
		return nil, fmt.Errorf("embedder probe: %w", err)
	}

	var vectors rag.VectorStore
	switch cfg.VectorStore {
	case "qdrant":
		vectors = &rag.Qdrant{
			BaseURL:    cfg.QdrantURL,
			Collection: cfg.QdrantCollection,
			Client:     &http.Client{Timeout: 30 * time.Second},
		}
	case "inmemory", "":
		vectors = rag.NewInMemoryStore()
	default:
		return nil, fmt.Errorf("unknown RAG_VECTOR_STORE %q (supported: qdrant, inmemory)", cfg.VectorStore)
	}
	if err := vectors.EnsureCollection(probeCtx, emb.Dim()); err != nil {
		return nil, fmt.Errorf("vector store: %w", err)
	}

	return &rag.Service{
		Embedder:  emb,
		Vectors:   vectors,
		Documents: repo,
		ChunkSize: cfg.RAGChunkSize,
		Overlap:   cfg.RAGOverlap,
	}, nil
}
