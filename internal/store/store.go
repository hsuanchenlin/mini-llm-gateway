package store

import (
	"context"
	"time"
)

// Entry is one logged request.
type Entry struct {
	ID               string
	Timestamp        time.Time
	Provider         string
	Model            string
	LatencyMs        int64
	StatusCode       int
	ErrorText        string
	PromptChars      int
	CompletionChars  int
	PromptTokens     int
	CompletionTokens int
	RagChunkIDs      string // comma-separated chunk UUIDs from the vector store; empty when RAG wasn't used
}

// Repository persists request logs and lets the admin endpoints read them back.
type Repository interface {
	Log(ctx context.Context, e Entry) error
	List(ctx context.Context, limit int, before time.Time) ([]Entry, error)
	Close() error
}

// Noop is a Repository that drops everything. Useful for tests that don't
// need persistence.
type Noop struct{}

func (Noop) Log(context.Context, Entry) error                      { return nil }
func (Noop) List(context.Context, int, time.Time) ([]Entry, error) { return nil, nil }
func (Noop) Close() error                                          { return nil }
