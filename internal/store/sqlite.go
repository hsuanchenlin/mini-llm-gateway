package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// SQLite is a Repository backed by a SQLite database file.
type SQLite struct {
	db *sql.DB
}

// OpenSQLite opens (or creates) the SQLite file at path and applies migrations.
// WAL mode is enabled so reads from /admin/requests don't block writes from
// /v1/chat/completions.
func OpenSQLite(path string) (*SQLite, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	if err := applyMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLite{db: db}, nil
}

func applyMigrations(db *sql.DB) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("store: read migrations dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		body, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("store: read %s: %w", e.Name(), err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			return fmt.Errorf("store: apply %s: %w", e.Name(), err)
		}
	}
	return nil
}

func (s *SQLite) Close() error { return s.db.Close() }

func (s *SQLite) Log(ctx context.Context, e Entry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO requests
		 (id, ts_ms, provider, model, latency_ms, status_code, error_text,
		  prompt_chars, completion_chars, prompt_tokens, completion_tokens)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID,
		e.Timestamp.UnixMilli(),
		e.Provider,
		e.Model,
		e.LatencyMs,
		e.StatusCode,
		e.ErrorText,
		e.PromptChars,
		e.CompletionChars,
		e.PromptTokens,
		e.CompletionTokens,
	)
	if err != nil {
		return fmt.Errorf("store: insert: %w", err)
	}
	return nil
}

func (s *SQLite) List(ctx context.Context, limit int, before time.Time) ([]Entry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ts_ms, provider, model, latency_ms, status_code, error_text,
		        prompt_chars, completion_chars, prompt_tokens, completion_tokens
		 FROM requests
		 WHERE ts_ms < ?
		 ORDER BY ts_ms DESC
		 LIMIT ?`,
		before.UnixMilli(), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: query: %w", err)
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		var tsMs int64
		if err := rows.Scan(
			&e.ID, &tsMs, &e.Provider, &e.Model, &e.LatencyMs, &e.StatusCode,
			&e.ErrorText, &e.PromptChars, &e.CompletionChars,
			&e.PromptTokens, &e.CompletionTokens,
		); err != nil {
			return nil, fmt.Errorf("store: scan: %w", err)
		}
		e.Timestamp = time.UnixMilli(tsMs).UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: rows: %w", err)
	}
	return out, nil
}
