package anthropic

import (
	"encoding/json"
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
// Calls the runner never executes — an MCP tool, which runs on Anthropic's
// side, or a turn cut short by a refusal — are recorded and never taken. They
// are a handful of small entries per run and the map dies with the run, which
// is cheaper than tracking block types to avoid them.
type invocations struct {
	mu      sync.Mutex
	pending map[string][]nacelle.Invocation
}

// newInvocations builds the registry for one run.
func newInvocations() *invocations {
	return &invocations{pending: map[string][]nacelle.Invocation{}}
}

// record files a call the stream has finished assembling.
//
// It is called from the goroutine that pulls the stream, and always before
// the runner starts the turn that executes the call: the runner hands us every
// event of a turn synchronously and only then runs the tools that turn asked
// for. The lock is for take, which does run concurrently.
func (i *invocations) record(call *nacelle.ToolEvent) {
	i.mu.Lock()
	defer i.mu.Unlock()
	slot := key(call.Name, []byte(call.Input))
	i.pending[slot] = append(i.pending[slot], nacelle.Invocation{ID: call.ID, Index: call.Index})
}

// take claims the call these arguments belong to, and is safe to call from
// several goroutines because the runner executes a turn's tools in parallel.
//
// Claiming rather than reading is what handles a model calling one tool twice
// in a turn: the two calls have different arguments, so they are different
// keys and each resolves exactly. Two calls with byte-identical arguments do
// share a key, and then the ids are handed out in the order the model asked
// for them — an arbitrary pairing between two requests that are the same
// request, and still one distinct id and index per execution.
//
// An unmatched call yields the zero Invocation, which is what a result
// carried before any of this existed.
func (i *invocations) take(name string, input json.RawMessage) nacelle.Invocation {
	i.mu.Lock()
	defer i.mu.Unlock()
	slot := key(name, input)
	queued := i.pending[slot]
	if len(queued) == 0 {
		return nacelle.Invocation{}
	}
	i.pending[slot] = queued[1:]
	return queued[0]
}

// key identifies a call by everything Execute is told about it.
//
// The name alone is not enough: a model that calls one tool twice in a turn
// would collide on it and pair the results to the wrong calls, which is
// exactly the mistake the id exists to prevent.
func key(name string, input []byte) string {
	return name + "\x00" + canonical(input)
}

// canonical is the arguments in the one spelling both sides of the pairing
// produce.
//
// They start out differing. The stream carries the model's own bytes, while
// the runner decodes the accumulated block and re-encodes it before calling
// Execute, which sorts object keys and drops whitespace. Sending both through
// encoding/json settles that, since the same decoded value encodes the same
// way. A call that carried no arguments is an empty string on the stream side
// and an empty object on the runner's, which is one thing spelled twice.
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
