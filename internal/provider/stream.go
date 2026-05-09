package provider

import "context"

// StreamSummary captures aggregate data the gateway records after a
// streaming response completes.
type StreamSummary struct {
	Model            string
	PromptTokens     int
	CompletionTokens int
}

// Streamer is implemented by providers that can stream chat completions
// chunk-by-chunk. onChunk is invoked once per content delta with the new
// text fragment; if it returns an error, Stream stops and returns that
// error.
//
// Streamer is intentionally a sibling of Provider, not a method on it,
// so non-streaming providers can stay simple.
type Streamer interface {
	Stream(ctx context.Context, req ChatRequest, onChunk func(delta string) error) (StreamSummary, error)
}
