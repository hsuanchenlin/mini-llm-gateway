package provider

import (
	"context"
	"strings"
	"testing"
)

func TestFakeProviderEchoesLastUserMessage(t *testing.T) {
	f := &Fake{}
	resp, err := f.Chat(context.Background(), ChatRequest{
		Model: "fake-1",
		Messages: []Message{
			{Role: "system", Content: "you are a test bot"},
			{Role: "user", Content: "hello there"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Content, "hello there") {
		t.Errorf("expected reply to echo last user message, got %q", resp.Content)
	}
	if resp.Model != "fake-1" {
		t.Errorf("expected model passthrough, got %q", resp.Model)
	}
	if resp.CompletionTokens == 0 {
		t.Errorf("expected non-zero completion token count")
	}
	if resp.PromptTokens == 0 {
		t.Errorf("expected non-zero prompt token count")
	}
}

func TestFakeProviderUsesOverrideReply(t *testing.T) {
	f := &Fake{Reply: "static reply"}
	resp, err := f.Chat(context.Background(), ChatRequest{Model: "fake-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "static reply" {
		t.Errorf("expected override reply, got %q", resp.Content)
	}
}

func TestFakeProviderRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &Fake{}
	if _, err := f.Chat(ctx, ChatRequest{}); err == nil {
		t.Errorf("expected error from canceled context")
	}
}

func TestFakeStreamReconstructsContent(t *testing.T) {
	f := &Fake{}
	var got string
	chunks := 0
	summary, err := f.Stream(context.Background(), ChatRequest{
		Model:    "fake-1",
		Messages: []Message{{Role: "user", Content: "hello world"}},
	}, func(delta string) error {
		got += delta
		chunks++
		return nil
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if chunks < 2 {
		t.Errorf("expected at least 2 chunks, got %d", chunks)
	}
	if got != "echo: hello world" {
		t.Errorf("reconstructed = %q, want %q", got, "echo: hello world")
	}
	if summary.Model != "fake-1" {
		t.Errorf("summary.Model = %q", summary.Model)
	}
	if summary.CompletionTokens != 3 {
		t.Errorf("summary.CompletionTokens = %d, want 3 (echo:, hello, world)", summary.CompletionTokens)
	}
}

func TestFakeStreamPropagatesOnChunkError(t *testing.T) {
	f := &Fake{}
	wantErr := strings.Repeat("x", 0) // sentinel-style; just any non-nil error works
	_ = wantErr
	_, err := f.Stream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "a b c"}},
	}, func(delta string) error {
		return context.Canceled
	})
	if err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestFakeStreamRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &Fake{}
	_, err := f.Stream(ctx, ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}, func(string) error {
		t.Errorf("onChunk should not be called when ctx canceled")
		return nil
	})
	if err == nil {
		t.Errorf("expected error from canceled ctx")
	}
}

func TestRegistryGetAndNames(t *testing.T) {
	r := Registry{"fake": &Fake{}}
	if got := r.Get("fake"); got == nil {
		t.Errorf("expected fake provider, got nil")
	}
	if got := r.Get("nope"); got != nil {
		t.Errorf("expected nil for missing provider, got %v", got)
	}
	names := r.Names()
	if len(names) != 1 || names[0] != "fake" {
		t.Errorf("Names() = %v, want [fake]", names)
	}
}
