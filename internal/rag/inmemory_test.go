package rag

import (
	"context"
	"testing"
)

func TestInMemoryStoreUpsertAndSearch(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	_ = s.EnsureCollection(ctx, 3)

	err := s.Upsert(ctx, []Point{
		{ID: "a", Vector: []float32{1, 0, 0}, DocumentID: "d1", ChunkIndex: 0, Text: "x-axis"},
		{ID: "b", Vector: []float32{0, 1, 0}, DocumentID: "d1", ChunkIndex: 1, Text: "y-axis"},
		{ID: "c", Vector: []float32{0, 0, 1}, DocumentID: "d2", ChunkIndex: 0, Text: "z-axis"},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	hits, err := s.Search(ctx, []float32{0.9, 0.1, 0}, 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].ID != "a" {
		t.Errorf("nearest = %s, want a (closest to x-axis-leaning query)", hits[0].ID)
	}
	if hits[0].Text != "x-axis" {
		t.Errorf("payload not preserved: %q", hits[0].Text)
	}
}

func TestInMemoryStoreDeleteByDocumentID(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	_ = s.Upsert(ctx, []Point{
		{ID: "a", Vector: []float32{1, 0, 0}, DocumentID: "d1"},
		{ID: "b", Vector: []float32{0, 1, 0}, DocumentID: "d1"},
		{ID: "c", Vector: []float32{0, 0, 1}, DocumentID: "d2"},
	})
	if err := s.DeleteByDocumentID(ctx, "d1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	hits, _ := s.Search(ctx, []float32{1, 1, 1}, 10)
	if len(hits) != 1 || hits[0].ID != "c" {
		t.Errorf("after delete, hits = %+v, want only [c]", hits)
	}
}

func TestCosineSim(t *testing.T) {
	cases := []struct {
		a, b []float32
		want float32
	}{
		{[]float32{1, 0}, []float32{1, 0}, 1.0},
		{[]float32{1, 0}, []float32{-1, 0}, -1.0},
		{[]float32{1, 0}, []float32{0, 1}, 0.0},
	}
	for _, c := range cases {
		got := cosineSim(c.a, c.b)
		if abs32(got-c.want) > 0.001 {
			t.Errorf("cosineSim(%v, %v) = %f, want %f", c.a, c.b, got, c.want)
		}
	}
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}
