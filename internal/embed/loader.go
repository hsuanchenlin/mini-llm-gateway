package embed

import (
	"fmt"
	"net/http"
	"time"
)

// Spec describes how to build an Embedder. Built from env config in main().
type Spec struct {
	Name          string // "fake", "openai", or "ollama"; empty disables RAG
	OllamaBaseURL string
	OllamaModel   string
	OpenAIBaseURL string
	OpenAIAPIKey  string
	OpenAIModel   string
	HTTPClient    *http.Client
}

// Build returns the named embedder. Returns (nil, nil) when Name is empty,
// signalling "RAG disabled" — this lets the gateway run without an embedder
// configured.
func Build(spec Spec) (Embedder, error) {
	if spec.Name == "" {
		return nil, nil
	}
	client := spec.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	switch spec.Name {
	case "fake":
		return Fake{}, nil
	case "ollama":
		if spec.OllamaBaseURL == "" {
			return nil, fmt.Errorf("embed ollama: OLLAMA_BASE_URL is required")
		}
		if spec.OllamaModel == "" {
			return nil, fmt.Errorf("embed ollama: OLLAMA_EMBED_MODEL is required")
		}
		return &Ollama{BaseURL: spec.OllamaBaseURL, Model: spec.OllamaModel, Client: client}, nil
	case "openai":
		if spec.OpenAIBaseURL == "" {
			return nil, fmt.Errorf("embed openai: OPENAI_BASE_URL is required")
		}
		if spec.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("embed openai: OPENAI_API_KEY is required")
		}
		if spec.OpenAIModel == "" {
			return nil, fmt.Errorf("embed openai: OPENAI_EMBED_MODEL is required")
		}
		return &OpenAI{
			BaseURL: spec.OpenAIBaseURL,
			APIKey:  spec.OpenAIAPIKey,
			Model:   spec.OpenAIModel,
			Client:  client,
		}, nil
	default:
		return nil, fmt.Errorf("embed: unknown embedder %q (supported: fake, openai, ollama)", spec.Name)
	}
}
