package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSaveAndGetDocument(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	d := Document{
		ID:         "doc-abc",
		Title:      "Onboarding",
		Body:       "Welcome to the team.",
		ChunkCount: 1,
		CreatedAt:  time.Now().Truncate(time.Millisecond),
	}
	if err := repo.SaveDocument(ctx, d); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.GetDocument(ctx, "doc-abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != d.Title || got.Body != d.Body || got.ChunkCount != d.ChunkCount {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, d)
	}
}

func TestGetDocumentNotFound(t *testing.T) {
	repo := newTestRepo(t)
	_, err := repo.GetDocument(context.Background(), "nope")
	if !errors.Is(err, ErrDocumentNotFound) {
		t.Errorf("err = %v, want ErrDocumentNotFound", err)
	}
}

func TestListDocumentsNewestFirst(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	for i, id := range []string{"a", "b", "c"} {
		err := repo.SaveDocument(ctx, Document{
			ID:        id,
			Title:     id,
			Body:      "body " + id,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	got, err := repo.ListDocuments(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
	if got[0].ID != "c" || got[2].ID != "a" {
		t.Errorf("order = [%s,%s,%s], want [c,b,a]", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestDeleteDocument(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	_ = repo.SaveDocument(ctx, Document{ID: "x", Title: "x", Body: "x", CreatedAt: time.Now()})
	if err := repo.DeleteDocument(ctx, "x"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetDocument(ctx, "x"); !errors.Is(err, ErrDocumentNotFound) {
		t.Errorf("expected not-found after delete, got %v", err)
	}
	if err := repo.DeleteDocument(ctx, "nope"); !errors.Is(err, ErrDocumentNotFound) {
		t.Errorf("delete missing = %v, want ErrDocumentNotFound", err)
	}
}
