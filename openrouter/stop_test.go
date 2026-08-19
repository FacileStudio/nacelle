package openrouter

import (
	"context"
	"testing"

	"github.com/FacileStudio/nacelle"
)

const truncated = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","content":"half an ans"},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}

data: {"id":"g","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13,"cost":0.00002}}

data: [DONE]

`

const unnamedReason = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{},"finish_reason":"the_router_invented_this"}]}

data: {"id":"g","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}

data: [DONE]

`

// An answer cut off by the token ceiling is a well-formed response with a
// normal ending, so without the finish_reason a consumer presents half a
// sentence as the finished article. Both the turn and the run have to say so.
func TestATruncatedAnswerEndsOnAReasonThatIsNotComplete(t *testing.T) {
	backend, _ := serve(t, truncated)
	events := collect(t, backend, nacelle.Request{System: "s"})

	turns := kinds(events, nacelle.KindTurn)
	if len(turns) != 1 || turns[0].Stop != nacelle.StopMaxTokens {
		t.Fatalf("turns = %+v, want one stopped by the output ceiling", turns)
	}

	done := kinds(events, nacelle.KindDone)
	if len(done) != 1 {
		t.Fatalf("saw %d done events, want 1", len(done))
	}
	if done[0].Stop.Complete() {
		t.Errorf("done stop = %q, want a reason a consumer cannot mistake for a finished answer", done[0].Stop)
	}
}

// OpenRouter fronts hundreds of providers and the SDK types finish_reason as a
// bare string, so a value this package has no name for is a question of when,
// not if. Calling it StopEnd would claim the answer is whole on exactly the
// runs where nobody knows that.
func TestAFinishReasonThisPackageCannotNameIsNotTreatedAsAFinishedAnswer(t *testing.T) {
	backend, _ := serve(t, unnamedReason)
	events := collect(t, backend, nacelle.Request{System: "s"})

	done := kinds(events, nacelle.KindDone)
	if len(done) != 1 || done[0].Stop != nacelle.StopOther {
		t.Fatalf("done = %+v, want a single run ended on StopOther", done)
	}
}

// Hitting the iteration ceiling is the caller's own limit, not a failure, and
// ending the run as a stream error used to throw away the accumulated usage
// with it: the total is computed and then never reaches a KindDone. Comparing
// runs on cost is why this package exists, so the total has to survive.
func TestReachingMaxIterationsEndsCleanlyAndKeepsTheUsage(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, in searchInput) (string, error) {
		return "result for " + in.Query, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	backend, _ := serve(t, withToolCalls)
	events := collect(t, backend, nacelle.Request{
		System:        "s",
		Tools:         []nacelle.Tool{tool},
		MaxIterations: 1,
	})

	done := kinds(events, nacelle.KindDone)
	if len(done) != 1 {
		t.Fatalf("saw %d done events, want the run to end cleanly", len(done))
	}
	if done[0].Stop != nacelle.StopIterations {
		t.Errorf("done stop = %q, want StopIterations", done[0].Stop)
	}
	if done[0].Usage.Total() == 0 || done[0].Usage.Cost == 0 {
		t.Errorf("done usage = %+v, want the accumulated total, not a discarded one", done[0].Usage)
	}
}
