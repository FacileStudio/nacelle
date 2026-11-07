package nacelle

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// ruling is a call after the hooks and Approve had their say: the tool that
// will run, the call as it will run, and the note the model reads first.
type ruling struct {
	tool Tool
	call Call
	note string
}

// rule runs the BeforeToolCall hooks, then Approve, and returns the call as it
// will run, or the error the model reads when it will not run at all.
//
// Hooks fire before Approve so a deny holds even when the caller configured
// an approval gate that would have said yes: the hook is the policy, the
// approval is the person, and the policy outranks the exception.
func (s *ToolSink) rule(ctx context.Context, tool Tool, call Call) (ruling, error) {
	call, reason, denied := s.runBeforeHooks(ctx, call)
	ruled := ruling{tool: tool, call: call}
	if denied {
		return ruled, fmt.Errorf("nacelle: %q denied by hook: %s", call.Name, reason)
	}
	if s.Approve == nil {
		return ruled, nil
	}
	decision := s.Approve(ctx, call)
	if decision.Deny != "" {
		return ruled, fmt.Errorf("nacelle: %q was refused: %s", call.Name, decision.Deny)
	}
	if decision.Input != nil {
		ruled.call.Input = decision.Input
	}
	ruled.note = decision.Note
	if decision.Name == "" || decision.Name == call.Name {
		return ruled, nil
	}
	next, ok := s.Tools[decision.Name]
	if !ok {
		return ruled, fmt.Errorf("nacelle: %q was redirected to %q, which is not available", call.Name, decision.Name)
	}
	ruled.tool, ruled.call.Name = next, decision.Name
	return ruled, nil
}

// runBeforeHooks asks every BeforeToolCall hook in registration order. A hook
// that returns Input replaces the call's input for every later hook and for
// Approve. A panic out of a hook denies rather than waves the call through; a
// guard that crashed has stopped guarding.
func (s *ToolSink) runBeforeHooks(ctx context.Context, call Call) (Call, string, bool) {
	hooks := s.Hooks[BeforeToolCall]
	retry := len(hooks) > 0 && s.wasDenied(call.Name)
	for _, hook := range hooks {
		res := recoverHook(hook)(ctx, HookEvent{Point: BeforeToolCall, Call: call, Retry: retry})
		if res.Deny != "" {
			s.markDenied(call.Name)
			return call, res.Deny, true
		}
		if res.Input != nil {
			call.Input = res.Input
		}
	}
	return call, "", false
}

// runAfterHooks asks every AfterToolCall hook, in reverse registration order
// for cleanup symmetry, and appends what they inject to the result the model
// reads. Injection is the only effect here: a Deny this late has nothing left
// to stop and is ignored.
func (s *ToolSink) runAfterHooks(ctx context.Context, call Call, result string, toolErr error) string {
	hooks := s.Hooks[AfterToolCall]
	if len(hooks) == 0 {
		return result
	}

	ev := HookEvent{Point: AfterToolCall, Call: call, Result: result, Err: toolErr}
	injected := make([]string, 0, len(hooks))
	for _, hook := range slices.Backward(hooks) {
		res := recoverHook(hook)(ctx, ev)
		if res.Inject != "" {
			injected = append(injected, truncate(res.Inject, MaxInject))
		}
	}
	if len(injected) == 0 {
		return result
	}
	return result + "\n" + strings.Join(injected, "\n")
}

// wasDenied reports whether a hook already refused this tool name this run.
func (s *ToolSink) wasDenied(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.denied[name]
}

// markDenied remembers a refusal for the next call to wasDenied.
func (s *ToolSink) markDenied(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.denied == nil {
		s.denied = map[string]bool{}
	}
	s.denied[name] = true
}

// recoverHook wraps one hook so a panic becomes a denial instead of a crash
// of whichever goroutine ran the tool. On AfterToolCall there is no call to
// deny, so a panicking hook there is heard as silence.
func recoverHook(hook Hook) Hook {
	return func(ctx context.Context, ev HookEvent) (res HookResult) {
		defer recoverPanic(ev, &res)
		return hook(ctx, ev)
	}
}

// recoverPanic converts a recovered hook panic into the decision the point
// allows: a denial before the call, silence after it.
func recoverPanic(ev HookEvent, res *HookResult) {
	r := recover()
	if r == nil {
		return
	}
	if ev.Point == BeforeToolCall {
		*res = HookResult{Deny: fmt.Sprintf("hook watching %q panicked: %v", ev.Call.Name, r)}
	}
}

// truncate cuts s to at most n bytes without splitting a rune in half.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
