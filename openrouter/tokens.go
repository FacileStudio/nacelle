package openrouter

import (
	"context"

	"github.com/FacileStudio/nacelle"
)

// CountTokens always refuses. The OpenAI chat-completions schema has no
// server-side token-counting endpoint, and OpenRouter fronts hundreds of
// models behind it with as many different tokenizers — a client-side estimate
// would be confidently wrong for most of them, which is worse than refusing:
// a caller budgeting a context window against a number this backend invented
// finds out it was fiction only when the real request still overflows.
func (b *Backend) CountTokens(context.Context, nacelle.Request) (int64, error) {
	return 0, &nacelle.Unsupported{Backend: b.Name(), Feature: "token counting"}
}
