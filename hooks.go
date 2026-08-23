package nacelle

import (
	"context"
	"fmt"
	"time"
)

// HookPoint names one moment in a run where hooks fire. The set is closed:
// two points cover the uses that must always happen — gating a tool before
// it runs, reacting after — and every further point is an API promise held
// forever, so none is added until a consumer needs it.
type HookPoint string

const (
	// BeforeToolCall fires before a local tool runs. A hook that denies
	// stops the call: the tool never executes and the model reads the deny
	// reason as the refusal. Deny is final — it holds regardless of any
	// interactive approval the caller configured, which is what makes a
	// hook a guarantee rather than a suggestion.
	BeforeToolCall HookPoint = "before_tool_call"

	// AfterToolCall fires after a local tool finished, successfully or
	// not. A hook here cannot undo the call; what it returns as Inject is
	// appended to the result the model reads.
	AfterToolCall HookPoint = "after_tool_call"
)

// MaxInject caps one hook's Inject, in bytes, before it reaches the model.
//
// Injected text rides in the context window for the rest of the conversation,
// so an unbounded print is an unbounded bill. Claude Code caps additionalContext
// at the same size; the number has survived contact with real sessions.
const MaxInject = 10000

// HookEvent is what a hook is told about the moment it fired.
//
// Input is raw JSON exactly as the model produced it, decoded by nobody here
// for the same reason ToolEvent.Input is: the core does not know any tool's
// schema. Retry reports that an earlier hook already denied this same tool
// name during this run, so a hook enforcing a policy can stand down rather
// than deny-loop a model that keeps retrying.
type HookEvent struct {
	// Point is which moment fired. A hook registered at one point can be
	// handed to another by mistake; reading this first is cheaper than
	// reasoning about a Result that will never arrive.
	Point HookPoint

	// Tool is the name of the tool about to run, or just finished.
	Tool string

	// Input is the raw JSON the model sent, on both points.
	Input string

	// Result is what the tool returned, on AfterToolCall only.
	Result string

	// Err is non-nil when the tool failed, on AfterToolCall only. The run
	// continues either way; a failed tool is reported to the model.
	Err error

	// Retry is true when this tool name was already denied by a hook
	// earlier in this run.
	Retry bool
}

// HookResult is what a hook decides. Both fields zero means allow, say
// nothing — the common case, and the reason the struct returns rather than
// the hook returning two values: a future decision kind should not move
// every hook's signature.
type HookResult struct {
	// Deny, when non-empty, blocks a BeforeToolCall. The string is the
	// reason the model reads in place of a tool result. On AfterToolCall
	// it is too late to block anything and a Deny is ignored.
	Deny string

	// Inject is text appended to what the model sees. On BeforeToolCall
	// there is no result yet to append to, so Inject there is ignored;
	// injection belongs on AfterToolCall.
	//
	// Truncated to MaxInject bytes. The cut is silent because the
	// alternative — refusing the whole injection over a long tail —
	// punishes the useful first 9,999 characters for the last one.
	Inject string
}

// Hook is one consumer decision at one point of the run. It holds its own
// state by closing over it: a hook that allows a thing once is a closure
// over a bool, not an object registered with this package.
//
// A hook runs in the tool's hot path — between the model asking and the
// tool running — so slow work belongs behind WithTimeout or Async. A panic
// out of a hook is recovered and, on BeforeToolCall, denies the call: a
// guard that crashed must not wave the request through.
type Hook func(ctx context.Context, ev HookEvent) HookResult

// WithTimeout wraps a hook so it cannot hang the run, and cancels the
// context it handed out when it does: a wrapper that only returns while the
// work keeps going is not a timeout but an orphaned goroutine per call —
// for the execHook case, an orphaned process per call.
//
// A hook that exceeds d is treated as having denied a BeforeToolCall — fail
// closed, since the only hooks worth timing out are guards — and as having
// said nothing otherwise.
func WithTimeout(d time.Duration, h Hook) Hook {
	return func(ctx context.Context, ev HookEvent) HookResult {
		run := make(chan HookResult, 1)
		work, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			run <- h(work, ev)
			cancel()
		}()
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case res := <-run:
			return res
		case <-timer.C:
			cancel()
			if ev.Point == BeforeToolCall {
				return HookResult{Deny: fmt.Sprintf("hook watching %q timed out after %s", ev.Tool, d)}
			}
			return HookResult{}
		}
	}
}

// Async wraps a hook so it runs detached from the run: the stream does not
// wait for it, and its Deny and Inject are dropped, because by the time an
// asynchronous answer arrives there is no result left to amend. It exists
// for audit and metrics, the hooks whose output nobody reads mid-run.
func Async(h Hook) Hook {
	return func(ctx context.Context, ev HookEvent) HookResult {
		go h(context.WithoutCancel(ctx), ev)
		return HookResult{}
	}
}
