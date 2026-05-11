package pricing

import (
	"math"
	"testing"
)

func TestUSDKnownModel(t *testing.T) {
	// gpt-4o-mini: $0.15 in, $0.60 out per million.
	// 1000 in + 500 out = (1000 * 0.15 + 500 * 0.60) / 1e6 = (150 + 300) / 1e6 = $0.00045
	got := USD("gpt-4o-mini", 1000, 500)
	want := 0.00045
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("USD(gpt-4o-mini, 1000, 500) = %v, want %v", got, want)
	}
}

func TestUSDUnknownModelReturnsZero(t *testing.T) {
	if got := USD("totally-made-up-model", 9999, 9999); got != 0 {
		t.Errorf("unknown model = %v, want 0", got)
	}
}

func TestUSDLocalModelIsFree(t *testing.T) {
	if got := USD("llama3.2:1b", 1000, 1000); got != 0 {
		t.Errorf("local model = %v, want 0 (free)", got)
	}
}

func TestUSDZeroTokens(t *testing.T) {
	if got := USD("gpt-4o", 0, 0); got != 0 {
		t.Errorf("zero tokens = %v, want 0", got)
	}
}

func TestKnown(t *testing.T) {
	if !Known("gpt-4o-mini") {
		t.Error("expected gpt-4o-mini to be known")
	}
	if Known("not-a-model") {
		t.Error("expected not-a-model to be unknown")
	}
}

func TestModels(t *testing.T) {
	got := Models()
	if len(got) < 5 {
		t.Errorf("expected at least 5 models in table, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("Models() not sorted: %s >= %s", got[i-1], got[i])
		}
	}
}
