package httpapi

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"mini-llm-gateway/internal/config"
	"mini-llm-gateway/internal/provider"
	"mini-llm-gateway/internal/store"
)

func newStatsTestServer(t *testing.T) (http.Handler, *store.SQLite) {
	t.Helper()
	repo, err := store.OpenSQLite(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	cfg := config.Config{
		DefaultProvider: "fake",
		DefaultModel:    "fake-1",
		RequestTimeout:  5 * time.Second,
	}
	return New(cfg, provider.Registry{"fake": &provider.Fake{}}, repo, nil).Handler(), repo
}

func seed(t *testing.T, repo *store.SQLite, model string, in, out, status int, ts time.Time) {
	t.Helper()
	err := repo.Log(context.Background(), store.Entry{
		ID:               model + "-" + ts.Format("150405.000000"),
		Timestamp:        ts,
		Provider:         "p",
		Model:            model,
		StatusCode:       status,
		PromptTokens:     in,
		CompletionTokens: out,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestAdminStatsComputesUSDPerModel(t *testing.T) {
	h, repo := newStatsTestServer(t)
	now := time.Now().UTC()

	// gpt-4o-mini: 1000 in @ $0.15/M + 500 out @ $0.60/M = $0.00045
	seed(t, repo, "gpt-4o-mini", 1000, 500, 200, now)
	// llama3.2:1b: free
	seed(t, repo, "llama3.2:1b", 5000, 2000, 200, now)
	// gpt-4o: failed request, should be excluded
	seed(t, repo, "gpt-4o", 100, 50, 500, now)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin/stats", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var resp adminStatsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := 0.00045
	if math.Abs(resp.TotalUSD-want) > 1e-9 {
		t.Errorf("TotalUSD = %v, want %v", resp.TotalUSD, want)
	}
	if math.Abs(resp.TodayUSD-want) > 1e-9 {
		t.Errorf("TodayUSD = %v, want %v (single-day data)", resp.TodayUSD, want)
	}
	if len(resp.ByModel) != 2 {
		t.Fatalf("ByModel = %d, want 2 (failed request excluded)", len(resp.ByModel))
	}
	for _, m := range resp.ByModel {
		switch m.Model {
		case "gpt-4o-mini":
			if !m.PricingKnown {
				t.Errorf("gpt-4o-mini should be pricing_known=true")
			}
			if math.Abs(m.USD-want) > 1e-9 {
				t.Errorf("gpt-4o-mini.USD = %v, want %v", m.USD, want)
			}
		case "llama3.2:1b":
			if m.USD != 0 {
				t.Errorf("local model USD = %v, want 0", m.USD)
			}
		}
	}
}

func TestAdminStatsClampsWindowDays(t *testing.T) {
	h, _ := newStatsTestServer(t)
	cases := []struct {
		query    string
		wantDays int
	}{
		{"", 30},
		{"?days=7", 7},
		{"?days=0", 30},      // invalid → default
		{"?days=999", 30},    // exceeds cap → default
		{"?days=abc", 30},    // not a number → default
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin/stats"+c.query, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d for %q", rr.Code, c.query)
		}
		var resp adminStatsResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp.WindowDays != c.wantDays {
			t.Errorf("?days=%q → window_days=%d, want %d", c.query, resp.WindowDays, c.wantDays)
		}
	}
}

func TestAdminRequestsIncludesUSDPerEntry(t *testing.T) {
	h, repo := newStatsTestServer(t)
	now := time.Now().Add(-time.Minute)
	seed(t, repo, "gpt-4o-mini", 1000, 500, 200, now)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin/requests?limit=10", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp adminRequestsResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Requests) != 1 {
		t.Fatalf("requests = %d", len(resp.Requests))
	}
	want := 0.00045
	if math.Abs(resp.Requests[0].USD-want) > 1e-9 {
		t.Errorf("entry.USD = %v, want %v", resp.Requests[0].USD, want)
	}
}
