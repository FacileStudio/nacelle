package nacelle_test

import (
	"context"
	"iter"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// CountTokens has to count the same request Stream would send — the system
// prompt and the tools included — or a caller budgeting a context window
// against it is budgeting against a smaller request than the one that will
// actually go out.
func TestCountTokensBuildsTheSameRequestAsStream(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(context.Context, struct{}) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	backend := full()
	agent, err := nacelle.New(nacelle.Config{Backend: backend, System: "be useful", Tools: []nacelle.Tool{tool}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := agent.CountTokens(context.Background(), []nacelle.Message{nacelle.UserText("hi")}); err != nil {
		t.Fatalf("CountTokens: %v", err)
	}

	if !backend.called {
		t.Fatal("the backend's CountTokens was never reached")
	}
	if backend.received.System != "be useful" {
		t.Errorf("system = %q, want the configured prompt", backend.received.System)
	}
	if len(backend.received.Tools) != 1 {
		t.Errorf("tools = %v, want the one configured tool counted too", backend.received.Tools)
	}
	if len(backend.received.Messages) != 1 {
		t.Errorf("messages = %v, want the conversation passed through", backend.received.Messages)
	}
}

// The count the stub returns has to reach the caller unchanged — it is the
// number a real backend would have produced, and this is the only thing
// standing between it and the caller.
func TestCountTokensReturnsWhatTheBackendCounted(t *testing.T) {
	backend := &countingStub{count: 42}
	agent, err := nacelle.New(nacelle.Config{Backend: backend, System: "s"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := agent.CountTokens(context.Background(), []nacelle.Message{nacelle.UserText("hi")})
	if err != nil {
		t.Fatalf("CountTokens: %v", err)
	}
	if got != 42 {
		t.Errorf("count = %d, want 42", got)
	}
}

// countingStub is a backend whose CountTokens answers with a fixed number,
// for the one test that cares what the number is rather than what request
// produced it.
type countingStub struct{ count int64 }

func (countingStub) Name() string                       { return "counting-stub" }
func (countingStub) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }

func (countingStub) Stream(context.Context, nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) { yield(nacelle.Event{Kind: nacelle.KindDone}, nil) }
}

func (c countingStub) CountTokens(context.Context, nacelle.Request) (int64, error) {
	return c.count, nil
}
