package nacelle_test

import (
	"context"
	"fmt"
	"iter"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// TestCompactConversation fires hooks and returns trimmed conversation
func TestCompactConversationHooks(t *testing.T) {
	var beforeCount, afterCount int64
	hooks := map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeCompact: {func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
			fmt.Sscanf(ev.Input, "%d", &beforeCount)
			return nacelle.HookResult{}
		}},
		nacelle.AfterCompact: {func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
			fmt.Sscanf(ev.Result, "%d", &afterCount)
			return nacelle.HookResult{}
		}},
	}
	agent, _ := nacelle.New(nacelle.Config{
		Backend: &countingBackend{count: 100},
		System:  "test",
		Hooks:   hooks,
	})
	conv := []nacelle.Message{
		nacelle.UserText("hello"),
		nacelle.UserText("world"),
		nacelle.UserText("this is a test"),
	}
	_, count, err := agent.CompactConversation(context.Background(), conv, 10)
	if err != nil {
		t.Fatalf("CompactConversation: %v", err)
	}
	if beforeCount != 10 || afterCount != count {
		t.Errorf("hooks = (%d, %d), want (10, %d)", beforeCount, afterCount, count)
	}
}

func TestCompactConversationReusesLastCount(t *testing.T) {
	backend := &countingBackend{count: 5}
	agent, _ := nacelle.New(nacelle.Config{Backend: backend, System: "test"})
	conv := []nacelle.Message{
		nacelle.UserText("one"),
		nacelle.UserText("two"),
	}
	kept, count, err := agent.CompactConversation(context.Background(), conv, 10)
	if err != nil {
		t.Fatalf("CompactConversation: %v", err)
	}
	if len(kept) != 2 || count != 5 {
		t.Errorf("kept=%d count=%d, want 2 and 5", len(kept), count)
	}
	if backend.calls != 2 {
		t.Errorf("CountTokens called %d times, want 2 (no duplicate probe)", backend.calls)
	}
}

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
	backend := &countingBackend{count: 42}
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

// countingBackend is a stub that returns a fixed count
type countingBackend struct {
	count int64
	calls int
}

func (c *countingBackend) Name() string                       { return "counting-backend" }
func (c *countingBackend) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }
func (c *countingBackend) Stream(context.Context, nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) { yield(nacelle.Event{Kind: nacelle.KindDone}, nil) }
}
func (c *countingBackend) CountTokens(context.Context, nacelle.Request) (int64, error) {
	c.calls++
	return c.count, nil
}
