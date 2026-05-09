package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestRepo(t *testing.T) *SQLite {
	t.Helper()
	repo, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func TestSQLiteLogAndList(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)

	now := time.Now().Truncate(time.Millisecond)
	for i, p := range []string{"fake", "ollama", "openai"} {
		err := repo.Log(ctx, Entry{
			ID:               p + "-id",
			Timestamp:        now.Add(time.Duration(i) * time.Second),
			Provider:         p,
			Model:            p + "-1",
			LatencyMs:        int64(10 * (i + 1)),
			StatusCode:       200,
			PromptChars:      5,
			CompletionChars:  10,
			PromptTokens:     1,
			CompletionTokens: 2,
		})
		if err != nil {
			t.Fatalf("log %s: %v", p, err)
		}
	}

	got, err := repo.List(ctx, 10, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	if got[0].Provider != "openai" || got[1].Provider != "ollama" || got[2].Provider != "fake" {
		t.Errorf("order = [%s %s %s], want newest-first openai/ollama/fake",
			got[0].Provider, got[1].Provider, got[2].Provider)
	}
	if got[0].LatencyMs != 30 {
		t.Errorf("openai latency = %d, want 30", got[0].LatencyMs)
	}
	if got[0].PromptTokens != 1 || got[0].CompletionTokens != 2 {
		t.Errorf("token counts not round-tripped")
	}
}

func TestSQLiteListBeforeCursor(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)

	base := time.Now().Truncate(time.Millisecond)
	for i := 0; i < 5; i++ {
		err := repo.Log(ctx, Entry{
			ID:         string(rune('a' + i)),
			Timestamp:  base.Add(time.Duration(i) * time.Second),
			Provider:   "fake",
			Model:      "fake-1",
			StatusCode: 200,
		})
		if err != nil {
			t.Fatalf("log: %v", err)
		}
	}

	got, err := repo.List(ctx, 10, base.Add(3*time.Second))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
	if got[0].ID != "c" || got[1].ID != "b" || got[2].ID != "a" {
		t.Errorf("ids = [%s %s %s], want [c b a]", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestSQLiteListLimit(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)

	base := time.Now()
	for i := 0; i < 5; i++ {
		_ = repo.Log(ctx, Entry{
			ID:        string(rune('a' + i)),
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Provider:  "fake",
			Model:     "fake-1",
		})
	}
	got, _ := repo.List(ctx, 2, time.Now().Add(time.Hour))
	if len(got) != 2 {
		t.Errorf("limit=2 returned %d", len(got))
	}
}

func TestSQLiteOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.db")
	r1, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := r1.Log(context.Background(), Entry{ID: "a", Timestamp: time.Now(), Provider: "fake", Model: "fake-1", StatusCode: 200}); err != nil {
		t.Fatalf("log: %v", err)
	}
	if err := r1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	r2, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { _ = r2.Close() })
	got, err := r2.List(context.Background(), 10, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("after reopen got %d entries: %+v", len(got), got)
	}
}

func TestNoopRepository(t *testing.T) {
	var n Noop
	if err := n.Log(context.Background(), Entry{}); err != nil {
		t.Errorf("Noop.Log err: %v", err)
	}
	got, err := n.List(context.Background(), 10, time.Now())
	if err != nil || got != nil {
		t.Errorf("Noop.List = (%v, %v), want (nil, nil)", got, err)
	}
	if err := n.Close(); err != nil {
		t.Errorf("Noop.Close err: %v", err)
	}
}
