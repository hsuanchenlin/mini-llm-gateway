// Package pricing computes approximate USD cost per request based on model
// and token counts. Numbers are kept in a hardcoded table — they're meant
// for portfolio-level cost visibility, not for billing customers. Update
// when published prices change.
package pricing

import "sort"

// USDPerMillion gives input/output cost in USD per million tokens.
type USDPerMillion struct {
	Input  float64
	Output float64
}

// table maps model name → pricing. Local models (Ollama) are zero. Anything
// not in this table is treated as zero — the request still gets logged, just
// with $0 attributed.
var table = map[string]USDPerMillion{
	// OpenAI
	"gpt-4o":                     {Input: 2.50, Output: 10.00},
	"gpt-4o-mini":                {Input: 0.15, Output: 0.60},
	"gpt-4-turbo":                {Input: 10.00, Output: 30.00},
	"gpt-3.5-turbo":              {Input: 0.50, Output: 1.50},
	"text-embedding-3-small":     {Input: 0.02, Output: 0},
	"text-embedding-3-large":     {Input: 0.13, Output: 0},
	// Anthropic (via Anthropic API or any OpenAI-compatible proxy)
	"claude-opus-4-7":            {Input: 15.00, Output: 75.00},
	"claude-sonnet-4-6":          {Input: 3.00, Output: 15.00},
	"claude-haiku-4-5":           {Input: 1.00, Output: 5.00},
	"claude-3-5-sonnet-20241022": {Input: 3.00, Output: 15.00},
	"claude-3-5-haiku-20241022":  {Input: 1.00, Output: 5.00},
	"claude-3-haiku-20240307":    {Input: 0.25, Output: 1.25},
	// Local (free)
	"llama3.2:1b":      {},
	"llama3.2:3b":      {},
	"llama3.1:8b":      {},
	"qwen2.5:7b":       {},
	"nomic-embed-text": {},
	// Test
	"fake-1": {},
}

// USD returns the USD cost for a request. Returns 0 if the model is unknown
// or pricing is zero. Token counts in are billed at Input rate; out at Output.
func USD(model string, promptTokens, completionTokens int) float64 {
	p, ok := table[model]
	if !ok {
		return 0
	}
	return (float64(promptTokens)*p.Input + float64(completionTokens)*p.Output) / 1_000_000
}

// Known returns whether the model has a pricing entry (vs. defaulting to $0).
// Used by the admin endpoint to flag "unknown model" in stats responses.
func Known(model string) bool {
	_, ok := table[model]
	return ok
}

// Models returns a sorted list of priced model names.
func Models() []string {
	names := make([]string, 0, len(table))
	for n := range table {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
