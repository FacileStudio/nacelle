package openrouter

import (
	"context"
	"errors"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// The OpenAI schema has no token-counting endpoint, and OpenRouter's model
// diversity rules out an honest local estimate — Capabilities has to say so
// before anyone calls CountTokens and finds out the hard way.
func TestCapabilitiesSaysTokenCountingIsUnsupported(t *testing.T) {
	backend, err := New(Config{Model: "test/model", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if backend.Capabilities().TokenCounting {
		t.Error("TokenCounting = true, want false — this backend has nothing to count with")
	}
}

// A caller that calls CountTokens anyway, without checking Capabilities
// first, still has to get a typed refusal rather than a made-up number.
func TestCountTokensRefusesRatherThanGuessing(t *testing.T) {
	backend, err := New(Config{Model: "test/model", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = backend.CountTokens(context.Background(), nacelle.Request{})

	var unsupported *nacelle.Unsupported
	if !errors.As(err, &unsupported) {
		t.Fatalf("CountTokens error = %v, want an *Unsupported", err)
	}
	if unsupported.Backend != "openrouter" {
		t.Errorf("backend = %q, want openrouter", unsupported.Backend)
	}
}
