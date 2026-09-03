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
