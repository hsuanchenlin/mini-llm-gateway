package embed

import (
	"strings"
	"testing"
)

func TestBuildDisabled(t *testing.T) {
	e, err := Build(Spec{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if e != nil {
		t.Errorf("expected nil embedder when Name is empty, got %T", e)
	}
}

func TestBuildFake(t *testing.T) {
	e, err := Build(Spec{Name: "fake"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := e.(Fake); !ok {
		t.Errorf("expected Fake, got %T", e)
	}
}

func TestBuildOllamaRequiresConfig(t *testing.T) {
	if _, err := Build(Spec{Name: "ollama"}); err == nil {
		t.Error("expected error when OLLAMA_BASE_URL missing")
	}
	if _, err := Build(Spec{Name: "ollama", OllamaBaseURL: "http://x"}); err == nil {
		t.Error("expected error when OLLAMA_EMBED_MODEL missing")
	}
}

func TestBuildOpenAIRequiresKey(t *testing.T) {
	_, err := Build(Spec{
		Name:          "openai",
		OpenAIBaseURL: "https://api.openai.com",
		OpenAIModel:   "x",
	})
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY missing")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("error should mention OPENAI_API_KEY, got %q", err.Error())
	}
}

func TestBuildUnknown(t *testing.T) {
	if _, err := Build(Spec{Name: "made-up"}); err == nil {
		t.Error("expected error for unknown embedder")
	}
}
