package provider

import (
	"strings"
	"testing"
)

func TestBuildRegistryFakeOnly(t *testing.T) {
	r, err := BuildRegistry(Spec{Names: []string{"fake"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := r["fake"].(*Fake); !ok {
		t.Errorf("expected *Fake, got %T", r["fake"])
	}
}

func TestBuildRegistryAllThree(t *testing.T) {
	r, err := BuildRegistry(Spec{
		Names:         []string{"fake", "ollama", "openai"},
		OllamaBaseURL: "http://localhost:11434",
		OpenAIBaseURL: "https://api.openai.com",
		OpenAIAPIKey:  "sk-x",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := r["ollama"].(*Ollama); !ok {
		t.Errorf("expected *Ollama, got %T", r["ollama"])
	}
	if oa, ok := r["openai"].(*OpenAI); !ok {
		t.Errorf("expected *OpenAI, got %T", r["openai"])
	} else if oa.APIKey != "sk-x" {
		t.Errorf("APIKey not propagated")
	}
	names := r.Names()
	if len(names) != 3 {
		t.Errorf("expected 3 providers, got %v", names)
	}
}

func TestBuildRegistryEmptyList(t *testing.T) {
	if _, err := BuildRegistry(Spec{Names: nil}); err == nil {
		t.Error("expected error for empty Names")
	}
}

func TestBuildRegistryUnknown(t *testing.T) {
	_, err := BuildRegistry(Spec{Names: []string{"made-up"}})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestBuildRegistryOllamaRequiresBaseURL(t *testing.T) {
	if _, err := BuildRegistry(Spec{Names: []string{"ollama"}}); err == nil {
		t.Error("expected error when OLLAMA_BASE_URL missing")
	}
}

func TestBuildRegistryOpenAIRequiresKey(t *testing.T) {
	_, err := BuildRegistry(Spec{
		Names:         []string{"openai"},
		OpenAIBaseURL: "https://api.openai.com",
	})
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY missing")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("error should mention OPENAI_API_KEY, got %q", err.Error())
	}
}

func TestBuildRegistryRejectsDuplicate(t *testing.T) {
	_, err := BuildRegistry(Spec{Names: []string{"fake", "fake"}})
	if err == nil {
		t.Error("expected error for duplicate provider")
	}
}
