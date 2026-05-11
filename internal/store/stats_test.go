package store

import (
	"context"
	"testing"
	"time"
)

func seedRequest(t *testing.T, repo *SQLite, model string, in, out, status int, ts time.Time) {
	t.Helper()
	err := repo.Log(context.Background(), Entry{
		ID:               model + "-" + ts.Format("20060102T150405.000000"),
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

func TestStatsByModelAggregates(t *testing.T) {
	repo := newTestRepo(t)
	now := time.Now()

	seedRequest(t, repo, "gpt-4o", 100, 50, 200, now)
	seedRequest(t, repo, "gpt-4o", 200, 100, 200, now.Add(time.Second))
	seedRequest(t, repo, "llama3.2:1b", 500, 250, 200, now.Add(2*time.Second))
	seedRequest(t, repo, "gpt-4o", 0, 0, 500, now.Add(3*time.Second)) // failed; excluded

	stats, err := repo.StatsByModel(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	byModel := map[string]ModelStat{}
	for _, s := range stats {
		byModel[s.Model] = s
	}
	if g := byModel["gpt-4o"]; g.Requests != 2 || g.PromptTokens != 300 || g.CompletionTokens != 150 {
		t.Errorf("gpt-4o = %+v, want requests=2 in=300 out=150 (failed request excluded)", g)
	}
	if g := byModel["llama3.2:1b"]; g.Requests != 1 || g.PromptTokens != 500 {
		t.Errorf("llama3.2:1b = %+v", g)
	}
}

func TestStatsByDayBuckets(t *testing.T) {
	repo := newTestRepo(t)
	now := time.Now().UTC()

	seedRequest(t, repo, "gpt-4o", 100, 50, 200, now)
	seedRequest(t, repo, "gpt-4o", 200, 100, 200, now.Add(time.Hour))
	seedRequest(t, repo, "gpt-4o", 50, 25, 200, now.AddDate(0, 0, -1))

	stats, err := repo.StatsByDay(context.Background(), 7)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(stats) < 2 {
		t.Fatalf("got %d days, want at least 2", len(stats))
	}
	// Today bucket should hold 2 requests (300 in / 150 out)
	today := now.Format("2006-01-02")
	for _, s := range stats {
		if s.Date == today {
			if s.Requests != 2 || s.PromptTokens != 300 {
				t.Errorf("today = %+v, want requests=2 in=300", s)
			}
			return
		}
	}
	t.Errorf("today (%s) not in stats: %+v", today, stats)
}

func TestStatsByDayExcludesOldEntries(t *testing.T) {
	repo := newTestRepo(t)
	old := time.Now().AddDate(0, 0, -60)
	seedRequest(t, repo, "x", 1, 1, 200, old)

	stats, err := repo.StatsByDay(context.Background(), 7)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, s := range stats {
		if s.Date == old.UTC().Format("2006-01-02") {
			t.Errorf("expected day %s to be excluded (60d old), got %+v", s.Date, s)
		}
	}
}
