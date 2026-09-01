package google

import (
	"context"

	"github.com/FacileStudio/nacelle"
)

// CountTokens reports how many tokens this request would use.
func (b *Backend) CountTokens(ctx context.Context, request nacelle.Request) (int64, error) {
	return 0, &nacelle.Unsupported{Backend: b.Name(), Feature: "token counting"}
}
