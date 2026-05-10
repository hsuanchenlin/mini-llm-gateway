package rag

import (
	"strings"
	"testing"
)

func TestChunkEmpty(t *testing.T) {
	if got := Chunk("", 100, 10); got != nil {
		t.Errorf("empty input → %v, want nil", got)
	}
}

func TestChunkShortFitsInOne(t *testing.T) {
	got := Chunk("hello", 100, 10)
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("short text → %v, want [hello]", got)
	}
}

func TestChunkExactSize(t *testing.T) {
	got := Chunk("abcdefghij", 10, 2)
	if len(got) != 1 || got[0] != "abcdefghij" {
		t.Errorf("exact-size → %v, want single chunk", got)
	}
}

func TestChunkSlidingWindow(t *testing.T) {
	text := "abcdefghijklmno" // 15 runes
	got := Chunk(text, 6, 2)
	// step = 4. Chunks at offsets 0..5, 4..9, 8..13, 12..14 (bounded)
	want := []string{"abcdef", "efghij", "ijklmn", "mno"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestChunkOverlapClampedWhenTooLarge(t *testing.T) {
	// overlap >= size should be clamped so the loop always advances
	got := Chunk("abcdef", 3, 5)
	if len(got) == 0 {
		t.Fatal("expected at least one chunk")
	}
	// Just verify we made progress and ended up covering the whole input
	if !strings.Contains(strings.Join(got, ""), "f") {
		t.Errorf("chunks didn't cover end of input: %v", got)
	}
}

func TestChunkUnicode(t *testing.T) {
	// 5 emoji = 5 runes regardless of byte length. With size 3, overlap 1,
	// step 2: chunks at offsets [0:3] and [2:5].
	got := Chunk("🍕🍔🍟🌭🥪", 3, 1)
	if len(got) != 2 || got[0] != "🍕🍔🍟" || got[1] != "🍟🌭🥪" {
		t.Errorf("got %v, want [🍕🍔🍟 🍟🌭🥪]", got)
	}
}
