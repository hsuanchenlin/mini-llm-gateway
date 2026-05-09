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
	"mini-llm-gateway/internal/httpapi"
	"mini-llm-gateway/internal/provider"
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

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           httpapi.New(cfg, registry, repo).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("mini-llm-gateway listening on %s (providers=%v default=%s model=%s db=%s)",
			srv.Addr, registry.Names(), cfg.DefaultProvider, cfg.DefaultModel, cfg.DBPath)
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
