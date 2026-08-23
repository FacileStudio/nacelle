package nacelle_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

// hookTool builds the one tool every hook test runs, so a test reads as "what
// did the hook decide" rather than "how is a tool built".
func hookTool(t *testing.T) nacelle.Tool {
	t.Helper()
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, _ searchInput) (string, error) {
		return "found it", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	return tool
}

func runHookTool(t *testing.T, hooks map[nacelle.HookPoint][]nacelle.Hook) (*nacelle.ToolSink, string, error) {
	t.Helper()
	sink := &nacelle.ToolSink{Hooks: hooks}
	result, err := nacelle.RunTool(context.Background(), hookTool(t),
		nacelle.Invocation{ID: "call_1"}, json.RawMessage(`{"query":"x"}`), sink)
	return sink, result, err
}

func TestBeforeHookDenialStopsTheTool(t *testing.T) {
	denied := false
	sink, result, err := runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: {func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
			denied = true
			if ev.Point != nacelle.BeforeToolCall || ev.Tool != "search" || ev.Input != `{"query":"x"}` {
				t.Errorf("event = %+v", ev)
			}
			return nacelle.HookResult{Deny: "not on my machine"}
		}},
	})

	if !denied {
		t.Fatal("the before-hook never ran")
	}
	if result != "" || err == nil || !strings.Contains(err.Error(), "not on my machine") {
		t.Fatalf("RunTool = %q, %v; want the deny reason back", result, err)
	}

	events := sink.Drain()
	if len(events) != 1 || !events[0].Tool.Refused {
		t.Fatalf("drained %+v; want one refused event", events)
	}
}

func TestAfterHookInjectionReachesTheModel(t *testing.T) {
	sink, result, err := runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.AfterToolCall: {func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
			if ev.Result != "found it" || ev.Err != nil {
				t.Errorf("event = %+v; want the finished call", ev)
			}
			return nacelle.HookResult{Inject: "reminder: cite sources"}
		}},
	})

	if err != nil {
		t.Fatalf("RunTool: %v", err)
	}
	if !strings.Contains(result, "found it") || !strings.Contains(result, "reminder: cite sources") {
		t.Fatalf("result = %q; want the tool's answer with the injection appended", result)
	}
	if drained := sink.Drain(); len(drained) == 1 && drained[0].Tool.Result != result {
		t.Errorf("reported %q, returned %q", drained[0].Tool.Result, result)
	}
}

// A guard that crashed must not wave the request through.
func TestPanickingBeforeHookDenies(t *testing.T) {
	_, _, err := runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: {func(context.Context, nacelle.HookEvent) nacelle.HookResult {
			panic("guard exploded")
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("err = %v; want the panic turned into a denial", err)
	}
}

func TestDenialMarksTheNextCallAsARetry(t *testing.T) {
	var saw []bool
	hooks := map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: {func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
			saw = append(saw, ev.Retry)
			return nacelle.HookResult{Deny: "no"}
		}},
	}
	sink := &nacelle.ToolSink{Hooks: hooks}
	tool := hookTool(t)

	for range 2 {
		_, _ = nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink)
	}
	if len(saw) != 2 || saw[0] || !saw[1] {
		t.Fatalf("retry flags = %v; want [false true]", saw)
	}
}

// After-hooks run in reverse registration order so paired work unwinds the
// way defer does.
func TestAfterHooksRunWithCleanupSymmetry(t *testing.T) {
	var order []string
	runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.AfterToolCall: {
			func(context.Context, nacelle.HookEvent) nacelle.HookResult {
				order = append(order, "first")
				return nacelle.HookResult{}
			},
			func(context.Context, nacelle.HookEvent) nacelle.HookResult {
				order = append(order, "second")
				return nacelle.HookResult{}
			},
		},
	})
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("order = %v; want reverse registration", order)
	}
}

func TestInjectIsTruncatedToMaxInject(t *testing.T) {
	_, result, _ := runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.AfterToolCall: {func(context.Context, nacelle.HookEvent) nacelle.HookResult {
			return nacelle.HookResult{Inject: strings.Repeat("x", nacelle.MaxInject+500)}
		}},
	})
	injected := strings.TrimPrefix(result, "found it\n")
	if len(injected) > nacelle.MaxInject {
		t.Fatalf("injected %d bytes, want at most %d", len(injected), nacelle.MaxInject)
	}
}

func TestWithTimeoutFailsClosedOnABeforeHook(t *testing.T) {
	slow := nacelle.WithTimeout(20*time.Millisecond, func(ctx context.Context, _ nacelle.HookEvent) nacelle.HookResult {
		select {
		case <-time.After(2 * time.Second):
			return nacelle.HookResult{}
		case <-ctx.Done():
			return nacelle.HookResult{}
		}
	})
	started := time.Now()
	_, _, err := runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: {slow},
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v; want a timeout denial", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("RunTool took %s; the timeout should have cut in far earlier", elapsed)
	}
}

func TestAsyncHookDoesNotBlockAndCannotDecide(t *testing.T) {
	fired := make(chan struct{})
	async := nacelle.Async(func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
		close(fired)
		if ev.Point == nacelle.BeforeToolCall {
			t.Error("an async hook ran on BeforeToolCall")
		}
		return nacelle.HookResult{Deny: "too late"}
	})
	sink := &nacelle.ToolSink{Hooks: map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.AfterToolCall: {async},
	}}
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, _ searchInput) (string, error) {
		return "found it", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	result, err := nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink)
	if err != nil || result != "found it" {
		t.Fatalf("RunTool = %q, %v; an async hook must not change either", result, err)
	}
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("the async hook never ran")
	}
}

// Tools run concurrently and share one sink; the denial memory behind Retry
// is exactly the state two parallel calls could race on.
func TestConcurrentRunsShareTheSinkSafely(t *testing.T) {
	hooks := map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: {func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
			_ = ev.Retry
			return nacelle.HookResult{}
		}},
		nacelle.AfterToolCall: {func(context.Context, nacelle.HookEvent) nacelle.HookResult {
			return nacelle.HookResult{Inject: "audited"}
		}},
	}
	sink := &nacelle.ToolSink{Hooks: hooks}
	tool := hookTool(t)

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = nacelle.RunTool(context.Background(), tool,
				nacelle.Invocation{ID: string(rune('a' + i))}, json.RawMessage(`{"query":"x"}`), sink)
		}()
	}
	wg.Wait()

	if race := sink.Drain(); len(race) != 16 {
		t.Fatalf("drained %d results, want 16", len(race))
	}
}
