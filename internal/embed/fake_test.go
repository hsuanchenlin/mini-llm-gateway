package embed

import (
	"context"
	"math"
	"testing"
)

func TestFakeEmbedDeterministic(t *testing.T) {
	a, _ := Fake{}.Embed(context.Background(), []string{"hello"})
	b, _ := Fake{}.Embed(context.Background(), []string{"hello"})
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("len = (%d,%d), want (1,1)", len(a), len(b))
	}
	for i := range a[0] {
		if a[0][i] != b[0][i] {
			t.Errorf("vector mismatch at index %d: %f vs %f", i, a[0][i], b[0][i])
		}
	}
}

func TestFakeEmbedShape(t *testing.T) {
	vecs, err := Fake{}.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("got %d vectors, want 3", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != (Fake{}).Dim() {
			t.Errorf("vector %d dim = %d, want %d", i, len(v), (Fake{}).Dim())
		}
		var norm float32
		for _, x := range v {
			norm += x * x
		}
		if math.Abs(float64(norm)-1) > 0.01 {
			t.Errorf("vector %d not unit-norm: %f", i, norm)
		}
	}
}

func TestFakeEmbedRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Fake{}).Embed(ctx, []string{"x"}); err == nil {
		t.Errorf("expected error from canceled ctx")
	}
}
