package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Hooks fire before Approve so a deny holds even when the caller configured
// an approval gate that would have said yes — the hook is the policy, the
// approval is the person, and the policy outranks the exception. A panic out
// of a hook denies rather than waves the call through; a guard that crashed
// has stopped guarding.
func (s *ToolSink) runBeforeHooks(ctx context.Context, name string, input json.RawMessage) (bool, string) {
	hooks := s.Hooks[BeforeToolCall]
	if len(hooks) == 0 {
		return false, ""
	}

	ev := HookEvent{Point: BeforeToolCall, Tool: name, Input: string(input), Retry: s.wasDenied(name)}
	for _, hook := range hooks {
		res := recoverHook(hook)(ctx, ev)
		if res.Deny != "" {
			s.markDenied(name)
			return true, res.Deny
		}
	}
	return false, ""
}

// runAfterHooks asks every AfterToolCall hook, in reverse registration order
// for cleanup symmetry, and appends what they inject to the result the model
// reads. Injection is the only effect here: a Deny this late has nothing left
// to stop and is ignored.
func (s *ToolSink) runAfterHooks(ctx context.Context, name string, input json.RawMessage, result string, toolErr error) string {
	hooks := s.Hooks[AfterToolCall]
	if len(hooks) == 0 {
		return result
	}

	ev := HookEvent{Point: AfterToolCall, Tool: name, Input: string(input), Result: result, Err: toolErr}
	injected := make([]string, 0, len(hooks))
	for i := len(hooks) - 1; i >= 0; i-- {
		res := recoverHook(hooks[i])(ctx, ev)
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
		*res = HookResult{Deny: fmt.Sprintf("hook watching %q panicked: %v", ev.Tool, r)}
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
