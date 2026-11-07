package nacelle_test

import (
	"context"
	"fmt"
	"strings"
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

func runHookTool(t *testing.T, hooks map[nacelle.HookPoint][]nacelle.Hook) ran {
	t.Helper()
	return runTool(t, context.Background(), nacelle.Request{Hooks: hooks}, hookTool(t), `{"query":"x"}`)
}

func TestBeforeHookDenialStopsTheTool(t *testing.T) {
	seen := nacelle.HookEvent{}
	hooks := map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: {func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
			seen = ev
			return nacelle.HookResult{Deny: "not on my machine"}
		}},
	}
	got := runHookTool(t, hooks)

	if seen.Point != nacelle.BeforeToolCall || seen.Call.Name != "search" || string(seen.Call.Input) != `{"query":"x"}` {
		t.Errorf("event = %+v", seen)
	}
	assertDenied(t, got, "not on my machine")
}

// assertDenied checks the full shape of a hook refusal: the tool produced
// nothing, the error names the reason, and the event the model sees is a
// refused call.
func assertDenied(t *testing.T, r ran, reason string) {
	t.Helper()
	if r.err == nil || !strings.Contains(r.err.Error(), reason) || r.result != r.err.Error() {
		t.Fatalf("ran = %q, %v; want the deny reason back", r.result, r.err)
	}
	events := r.toolEvents()
	if len(events) != 1 || !events[0].Tool.Refused {
		t.Fatalf("saw %+v; want one refused event", events)
	}
}

// checkFinishedCall asserts the event an after-hook sees carries the tool's
// answer and no error. A helper because the closure it replaces was the one
// thing pushing this test past the nesting cap.
func checkFinishedCall(t *testing.T, ev nacelle.HookEvent) {
	t.Helper()
	if ev.Result != "found it" || ev.Err != nil {
		t.Errorf("event = %+v; want the finished call", ev)
	}
}

func TestAfterHookInjectionReachesTheModel(t *testing.T) {
	got := runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.AfterToolCall: {func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
			checkFinishedCall(t, ev)
			return nacelle.HookResult{Inject: "reminder: cite sources"}
		}},
	})

	if got.err != nil {
		t.Fatalf("ran: %v", got.err)
	}
	if !strings.Contains(got.result, "found it") || !strings.Contains(got.result, "reminder: cite sources") {
		t.Fatalf("result = %q; want the tool's answer with the injection appended", got.result)
	}
	if got.tool.Result != got.result {
		t.Errorf("reported %q, the model read %q", got.tool.Result, got.result)
	}
}

// A guard that crashed must not wave the request through.
func TestPanickingBeforeHookDenies(t *testing.T) {
	err := runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: {func(context.Context, nacelle.HookEvent) nacelle.HookResult {
			panic("guard exploded")
		}},
	}).err
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
	tool := hookTool(t)
	call := toolStep(tool.Spec().Name, `{"query":"x"}`)
	runSteps(t, context.Background(), nacelle.Request{Hooks: hooks}, tool, call, call, textStep("done"))
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
	result := runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.AfterToolCall: {func(context.Context, nacelle.HookEvent) nacelle.HookResult {
			return nacelle.HookResult{Inject: strings.Repeat("x", nacelle.MaxInject+500)}
		}},
	}).result
	injected := strings.TrimPrefix(result, "found it\n")
	if len(injected) > nacelle.MaxInject {
		t.Fatalf("injected %d bytes, want at most %d", len(injected), nacelle.MaxInject)
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
	got := runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{nacelle.AfterToolCall: {async}})
	if got.err != nil || got.result != "found it" {
		t.Fatalf("ran = %q, %v; an async hook must not change either", got.result, got.err)
	}
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("the async hook never ran")
	}
}

// denyNever reads Retry and allows the call: touching the field from many
// goroutines at once is what -race is watching for here.
func denyNever(ev nacelle.HookEvent) nacelle.HookResult {
	retry := ev.Retry
	_ = retry
	return nacelle.HookResult{}
}

// Read-only calls run concurrently and share one sink; the denial memory
// behind Retry is exactly the state two parallel calls could race on.
func TestConcurrentRunsShareTheSinkSafely(t *testing.T) {
	hooks := map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: {func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
			return denyNever(ev)
		}},
		nacelle.AfterToolCall: {func(context.Context, nacelle.HookEvent) nacelle.HookResult {
			return nacelle.HookResult{Inject: "audited"}
		}},
	}
	tool, err := nacelle.NewToolWithOptions("search", "Find things", func(_ context.Context, _ searchInput) (string, error) {
		return "found it", nil
	}, nacelle.ToolOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("NewToolWithOptions: %v", err)
	}
	inputs := make([]string, 16)
	for i := range inputs {
		inputs[i] = fmt.Sprintf(`{"query":"%c"}`, 'a'+i)
	}
	got := runSteps(t, context.Background(), nacelle.Request{Hooks: hooks}, tool, toolStep(tool.Spec().Name, inputs...), textStep("done"))
	if race := got.toolEvents(); len(race) != 16 {
		t.Fatalf("saw %d results, want 16", len(race))
	}
}
