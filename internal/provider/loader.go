package provider

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Spec describes how to build a Registry. cmd/server populates this from
// config.Config; tests can build it directly.
type Spec struct {
	Names         []string
	OllamaBaseURL string
	OpenAIBaseURL string
	OpenAIAPIKey  string
	HTTPClient    *http.Client
}

// BuildRegistry constructs the named providers, validating that each has
// the configuration it needs. Unknown names return an error rather than
// being silently ignored.
func BuildRegistry(spec Spec) (Registry, error) {
	if len(spec.Names) == 0 {
		return nil, fmt.Errorf("provider: at least one provider must be configured (set GATEWAY_PROVIDERS)")
	}
	client := spec.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	r := Registry{}
	for _, raw := range spec.Names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, dup := r[name]; dup {
			return nil, fmt.Errorf("provider %q: listed twice in GATEWAY_PROVIDERS", name)
		}
		switch name {
		case "fake":
			r[name] = &Fake{}
		case "ollama":
			if spec.OllamaBaseURL == "" {
				return nil, fmt.Errorf("provider ollama: OLLAMA_BASE_URL is required")
			}
			r[name] = &Ollama{BaseURL: spec.OllamaBaseURL, Client: client}
		case "openai":
			if spec.OpenAIBaseURL == "" {
				return nil, fmt.Errorf("provider openai: OPENAI_BASE_URL is required")
			}
			if spec.OpenAIAPIKey == "" {
				return nil, fmt.Errorf("provider openai: OPENAI_API_KEY is required (set it to any non-empty string for local servers without auth)")
			}
			r[name] = &OpenAI{BaseURL: spec.OpenAIBaseURL, APIKey: spec.OpenAIAPIKey, Client: client}
		default:
			return nil, fmt.Errorf("provider: unknown provider %q (supported: fake, ollama, openai)", name)
		}
	}
	if len(r) == 0 {
		return nil, fmt.Errorf("provider: no providers built (check GATEWAY_PROVIDERS)")
	}
	return r, nil
}
