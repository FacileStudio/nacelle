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
	// Setup agent with hooks
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

	// Create a simple tool for counting
	tool, _ := nacelle.NewTool("noop", "Does nothing", func(_ context.Context, _ struct{}) (string, error) { return "", nil })

	agent, _ := nacelle.New(nacelle.Config{
		Backend: &countingBackend{count: 100},
		System:  "test",
		Tools:   []nacelle.Tool{tool},
		Hooks:   hooks,
	})

	// Create a conversation that's too big
	conv := []nacelle.Message{
		nacelle.UserText("hello"),
		nacelle.UserText("world"),
		nacelle.UserText("this is a test"),
	}

	// Compact to small size
	_, count, err := agent.CompactConversation(context.Background(), conv, 10)
	if err != nil {
		t.Fatalf("CompactConversation: %v", err)
	}

	if beforeCount != 10 {
		t.Errorf("BeforeCompact input = %d, want 10", beforeCount)
	}
	if afterCount != count {
		t.Errorf("AfterCompact result = %d, got count %d", afterCount, count)
	}
}

// countingBackend is a stub that returns a fixed count
type countingBackend struct{ count int64 }

func (c *countingBackend) Name() string                       { return "counting-backend" }
func (c *countingBackend) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }
func (c *countingBackend) Stream(context.Context, nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) { yield(nacelle.Event{Kind: nacelle.KindDone}, nil) }
}
func (c *countingBackend) CountTokens(context.Context, nacelle.Request) (int64, error) {
	return c.count, nil
}
