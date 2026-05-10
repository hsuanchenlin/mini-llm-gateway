package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Document is one ingested knowledge-base item. Chunks live in the vector
// store; the body is kept here so the admin UI can show the original text.
type Document struct {
	ID         string
	Title      string
	Body       string
	ChunkCount int
	CreatedAt  time.Time
}

// ErrDocumentNotFound is returned by GetDocument and DeleteDocument when no
// row matches the supplied id.
var ErrDocumentNotFound = errors.New("store: document not found")

func (s *SQLite) SaveDocument(ctx context.Context, d Document) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO documents (id, title, body, chunk_count, created_ms)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   title=excluded.title,
		   body=excluded.body,
		   chunk_count=excluded.chunk_count,
		   created_ms=excluded.created_ms`,
		d.ID, d.Title, d.Body, d.ChunkCount, d.CreatedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("store: save document: %w", err)
	}
	return nil
}

func (s *SQLite) GetDocument(ctx context.Context, id string) (Document, error) {
	var d Document
	var ms int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, body, chunk_count, created_ms FROM documents WHERE id = ?`,
		id,
	).Scan(&d.ID, &d.Title, &d.Body, &d.ChunkCount, &ms)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("store: get document: %w", err)
	}
	d.CreatedAt = time.UnixMilli(ms).UTC()
	return d, nil
}

func (s *SQLite) ListDocuments(ctx context.Context, limit int) ([]Document, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, body, chunk_count, created_ms
		 FROM documents
		 ORDER BY created_ms DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list documents: %w", err)
	}
	defer rows.Close()

	var out []Document
	for rows.Next() {
		var d Document
		var ms int64
		if err := rows.Scan(&d.ID, &d.Title, &d.Body, &d.ChunkCount, &ms); err != nil {
			return nil, fmt.Errorf("store: scan document: %w", err)
		}
		d.CreatedAt = time.UnixMilli(ms).UTC()
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: rows: %w", err)
	}
	return out, nil
}

func (s *SQLite) DeleteDocument(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete document: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDocumentNotFound
	}
	return nil
}
