package anthropic

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/FacileStudio/nacelle"
)

// invocations pairs a tool call the stream reported with the execution the
// SDK's runner later starts for it.
//
// The two halves arrive through different doors and the SDK does not join
// them: the id and the position come off the stream, while
// sdk.BetaTool.Execute is handed the tool's name and its arguments and
// nothing else. The tool_use id is never passed down, so a result reported
// from inside Execute has no id to carry unless it is looked up here. Name
// and arguments are therefore not a heuristic key, they are the whole of what
// the SDK makes reachable at that point.
//
// The registry holds one turn and no more: the calls of the turn that just
// closed, and only the ones the runner is going to execute. That is not
// tidiness. A stale entry is not a bounded memory cost, it is a live key — a
// later call with the same name and the same arguments pops it and ships a
// different call's id and index, silently, looking exactly like the swapped
// result the id was added to prevent. Two producers of stale entries occur on
// runs that carry on: an mcp_tool_use, which Anthropic executes and the runner
// never sees, and the tool_use blocks before a fallback block, which the
// runner deliberately skips. Neither can survive its turn here, because a
// turn's batch replaces the previous one whole and neither is ever queued.
type invocations struct {
	mu      sync.Mutex
	pending []invocation
}

// invocation is one recorded call, the bytes the model wrote for it, and
// whether an execution has already claimed it.
type invocation struct {
	call      nacelle.Invocation
	name      string
	arguments string
	raw       string
	claimed   bool
}

// anyArguments asks claim for the first unclaimed call of a name whatever it
// was called with. canonical never produces it, so it cannot collide with a
// real spelling.
const anyArguments = ""

// newInvocations builds the registry for one run.
func newInvocations() *invocations {
	return &invocations{}
}

// reset makes one turn's calls the only ones a lookup can find.
//
// It runs when the turn closes, which is both the last moment before the
// runner executes that turn and the first moment the previous turn's entries
// are certainly dead: the runner hands us every event of a turn synchronously
// and only then runs the tools that turn asked for, so every take against the
// older batch has already happened. Replacing rather than appending is what
// makes a cross-turn mispairing structurally impossible rather than unlikely.
func (i *invocations) reset(calls []*nacelle.ToolEvent) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.pending = make([]invocation, 0, len(calls))
	for _, call := range calls {
		i.pending = append(i.pending, invocation{
			call:      nacelle.Invocation{ID: call.ID, Index: call.Index},
			name:      call.Name,
			arguments: canonical([]byte(call.Input)),
			raw:       call.Input,
		})
	}
}

// take claims the call these arguments belong to and returns the bytes the
// model actually wrote for it. It is safe to call from several goroutines
// because the runner executes a turn's tools in parallel.
//
// Arguments are matched first because they are the only thing that tells two
// calls to one tool apart, and the runner does not say which block it started
// an execution for. When they do not match, the first unclaimed call of that
// name is taken instead: within one turn that is either the right call or a
// sibling nothing could distinguish from it, and it is never another turn's.
// The fallback is what turns the two spellings canonical cannot reproduce from
// a silent mispairing into a logged, correct pairing.
//
// A miss is logged rather than swallowed. It cannot be logged through the
// caller's own logger — nacelle.Request does not carry one — so it goes to the
// default, which is also what nacelle.Config falls back to.
func (i *invocations) take(name string, input json.RawMessage) (nacelle.Invocation, json.RawMessage) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if claimed, found := i.claim(name, canonical(input)); found {
		return claimed.call, json.RawMessage(claimed.raw)
	}
	claimed, found := i.claim(name, anyArguments)
	if !found {
		slog.Warn("nacelle/anthropic: a tool result cannot name the call it answers",
			"tool", name, "arguments", string(input))
		return nacelle.Invocation{}, input
	}
	slog.Warn("nacelle/anthropic: a tool call was paired by position because its arguments did not match the stream",
		"tool", name, "arguments", string(input))
	return claimed.call, json.RawMessage(claimed.raw)
}

// claim takes the first unclaimed call of this name, requiring the arguments
// to match unless asked for anyArguments.
//
// Claiming rather than reading is what handles a model calling one tool twice
// in a turn with the same arguments: the ids are handed out in the order the
// model asked for them, which is an arbitrary pairing between two requests
// that are the same request, and still one distinct id and index each.
func (i *invocations) claim(name, arguments string) (invocation, bool) {
	for position := range i.pending {
		entry := &i.pending[position]
		switch {
		case entry.claimed, entry.name != name:
			continue
		case arguments != anyArguments && entry.arguments != arguments:
			continue
		}
		entry.claimed = true
		return *entry, true
	}
	return invocation{}, false
}

// canonical is the arguments in the one spelling both sides of the pairing
// usually produce.
//
// They start out differing. The stream carries the model's own bytes, while
// the runner decodes the accumulated block and re-encodes it before calling
// Execute, which sorts object keys and drops whitespace. Sending both through
// encoding/json settles that, since the same decoded value encodes the same
// way. A call that carried no arguments is an empty string on the stream side
// and an empty object on the runner's, which is one thing spelled twice.
//
// Two shapes it does not settle, both measured. An object with a duplicate key
// decodes here to the last value and in the runner to the first, so
// {"a":1,"a":2} becomes {"a":2} on one side and {"a":1} on the other. Invalid
// or truncated JSON is returned unchanged here and reaches the runner as {}.
// Neither is worth reproducing bug for bug — take's positional fallback pairs
// both correctly, and says so in the log.
func canonical(input []byte) string {
	if len(input) == 0 {
		return "{}"
	}
	var decoded any
	if err := json.Unmarshal(input, &decoded); err != nil {
		return string(input)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return string(input)
	}
	return string(encoded)
}
