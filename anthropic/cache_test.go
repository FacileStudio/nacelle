package anthropic

import (
	"context"
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// The prefix a run resends on every tool iteration is the whole reason this
// backend is affordable: a cache read bills at a tenth of a plain input token,
// and the runner replays the system prompt and every tool schema each time
// round the loop.
func TestTheStablePrefixIsCachedWhenSomethingWillReadIt(t *testing.T) {
	amortising := map[string]nacelle.Request{
		"a run with tools":       {System: "s", MaxTokens: 1, Tools: []nacelle.Tool{echoTool(t)}},
		"a conversation resumed": {System: "s", MaxTokens: 1, Messages: []nacelle.Message{nacelle.UserText("again")}},
	}
	for name, request := range amortising {
		if got := New(Config{}).params(request).CacheControl; got != sdk.NewBetaCacheControlEphemeralParam() {
			t.Errorf("%s: cache control = %#v, want an ephemeral breakpoint", name, got)
		}
	}
}

// A Config with no tools and no history makes exactly one API call, so the
// breakpoint writes a cache entry nothing will ever read and bills 1.25x the
// input for it. Charging every one-shot caller a quarter extra to save this
// package a condition is not a trade-off worth defending.
func TestAOneShotRunIsNotChargedForACacheNobodyReads(t *testing.T) {
	params := New(Config{}).params(nacelle.Request{System: "s", MaxTokens: 1})

	if params.CacheControl != (sdk.BetaCacheControlEphemeralParam{}) {
		t.Errorf("cache control = %#v, want none; a one-shot run pays 1.25x for an entry with no read", params.CacheControl)
	}
}

// Tools render at the front of the request, so they are the front of every
// cached prefix. Two agents given the same tools in a different order must not
// quietly miss each other's cache.
func TestToolsAreOrderedSoTheCachedPrefixIsStable(t *testing.T) {
	type input struct {
		Query string `json:"query" jsonschema:"required,description=What to look for"`
	}
	build := func(name string) nacelle.Tool {
		tool, err := nacelle.NewTool(name, "Find things", func(_ context.Context, in input) (string, error) {
			return in.Query, nil
		})
		if err != nil {
			t.Fatalf("NewTool: %v", err)
		}
		return tool
	}
	alpha, beta := build("alpha"), build("beta")

	forwards := adapt([]nacelle.Tool{alpha, beta}, &nacelle.ToolSink{}, newInvocations())
	backwards := adapt([]nacelle.Tool{beta, alpha}, &nacelle.ToolSink{}, newInvocations())

	for position := range forwards {
		if forwards[position].Name() != backwards[position].Name() {
			t.Fatalf("tool %d = %q and %q, want the same name from either order",
				position, forwards[position].Name(), backwards[position].Name())
		}
	}
	if forwards[0].Name() != "alpha" {
		t.Errorf("first tool = %q, want alpha", forwards[0].Name())
	}
}
