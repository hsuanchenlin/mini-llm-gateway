package config

import (
	"reflect"
	"testing"
)

func TestSplitCSV(t *testing.T) {
	cases := map[string][]string{
		"":                       {},
		"fake":                   {"fake"},
		"fake,ollama,openai":     {"fake", "ollama", "openai"},
		" fake , ollama ":        {"fake", "ollama"},
		",,fake,,":               {"fake"},
	}
	for in, want := range cases {
		got := splitCSV(in)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("splitCSV(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("GATEWAY_PORT", "")
	t.Setenv("GATEWAY_DEFAULT_PROVIDER", "")
	t.Setenv("GATEWAY_DEFAULT_MODEL", "")
	t.Setenv("GATEWAY_REQUEST_TIMEOUT_SECONDS", "")
	t.Setenv("GATEWAY_PROVIDERS", "")
	t.Setenv("OLLAMA_BASE_URL", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")

	cfg := FromEnv()
	if cfg.Port != 8090 {
		t.Errorf("Port = %d, want 8090", cfg.Port)
	}
	if cfg.DefaultProvider != "fake" {
		t.Errorf("DefaultProvider = %q, want fake", cfg.DefaultProvider)
	}
	if !reflect.DeepEqual(cfg.Providers, []string{"fake"}) {
		t.Errorf("Providers = %v, want [fake]", cfg.Providers)
	}
	if cfg.OllamaBaseURL != "http://localhost:11434" {
		t.Errorf("OllamaBaseURL = %q", cfg.OllamaBaseURL)
	}
	if cfg.OpenAIBaseURL != "https://api.openai.com" {
		t.Errorf("OpenAIBaseURL = %q", cfg.OpenAIBaseURL)
	}
	if cfg.OpenAIAPIKey != "" {
		t.Errorf("OpenAIAPIKey should default empty, got %q", cfg.OpenAIAPIKey)
	}
	if cfg.DBPath != "mini-llm-gateway.db" {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
}
