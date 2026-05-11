package store

import (
	"context"
	"fmt"
	"time"
)

// ModelStat is one model's aggregate usage across the request log.
type ModelStat struct {
	Model            string
	Requests         int
	PromptTokens     int
	CompletionTokens int
}

// DayStat is one day's aggregate usage (UTC).
type DayStat struct {
	Date             string // YYYY-MM-DD (UTC)
	Requests         int
	PromptTokens     int
	CompletionTokens int
}

// StatsByModel returns one row per model seen in the request log since the
// given timestamp. Pass time.Time{} (zero value) for "all time".
//
// Rows where status_code < 200 or >= 300 are excluded so failed requests
// don't inflate token totals (they wouldn't have generated tokens anyway).
func (s *SQLite) StatsByModel(ctx context.Context, since time.Time) ([]ModelStat, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT model,
		        COUNT(*) AS requests,
		        COALESCE(SUM(prompt_tokens), 0) AS in_tokens,
		        COALESCE(SUM(completion_tokens), 0) AS out_tokens
		 FROM requests
		 WHERE model != ''
		   AND status_code >= 200 AND status_code < 300
		   AND ts_ms >= ?
		 GROUP BY model
		 ORDER BY in_tokens + out_tokens DESC`,
		since.UnixMilli(),
	)
	if err != nil {
		return nil, fmt.Errorf("store: query stats by model: %w", err)
	}
	defer rows.Close()

	var out []ModelStat
	for rows.Next() {
		var m ModelStat
		if err := rows.Scan(&m.Model, &m.Requests, &m.PromptTokens, &m.CompletionTokens); err != nil {
			return nil, fmt.Errorf("store: scan model stat: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// StatsByDay returns one row per UTC day in the last `days` days, including
// days with zero requests omitted (caller can fill gaps client-side if needed).
func (s *SQLite) StatsByDay(ctx context.Context, days int) ([]DayStat, error) {
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	rows, err := s.db.QueryContext(ctx,
		`SELECT strftime('%Y-%m-%d', ts_ms / 1000, 'unixepoch') AS day,
		        COUNT(*) AS requests,
		        COALESCE(SUM(prompt_tokens), 0) AS in_tokens,
		        COALESCE(SUM(completion_tokens), 0) AS out_tokens
		 FROM requests
		 WHERE ts_ms >= ?
		 GROUP BY day
		 ORDER BY day ASC`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("store: query stats by day: %w", err)
	}
	defer rows.Close()

	var out []DayStat
	for rows.Next() {
		var d DayStat
		if err := rows.Scan(&d.Date, &d.Requests, &d.PromptTokens, &d.CompletionTokens); err != nil {
			return nil, fmt.Errorf("store: scan day stat: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
