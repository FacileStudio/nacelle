package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// drainInterval is how often the tool sink is drained while its calls run.
//
// A tool emits output into the sink from its own goroutine; the consumer's
// events only move when this goroutine drains. Polling is the boundary between
// "a line appears within one interval of being produced" and blocking until the
// tool finishes, which is the burst everything here exists to avoid.
const drainInterval = 20 * time.Millisecond

// runCalls executes a turn's local calls in parallel and live-drains their
// output onto the event stream, returning the tool_result blocks in the order
// the model asked for them.
//
// Each call runs on its own goroutine and feeds the shared sink, so the
// consumer sees a KindToolOutput the moment it is produced rather than at the
// end of the whole batch. Results are collected by call index directly from the
// tool's return, so a fast tool finishing early cannot reorder the message
// rebuilt for the model.
func runCalls(
	ctx context.Context,
	calls []*nacelle.ToolEvent,
	s *session,
	byName map[string]nacelle.Tool,
) ([]sdk.BetaContentBlockParamUnion, bool) {
	results := make([]sdk.BetaContentBlockParamUnion, len(calls))
	var wg sync.WaitGroup
	for position, call := range calls {
		wg.Add(1)
		go func(position int, call *nacelle.ToolEvent) {
			defer wg.Done()
			results[position] = executeOne(ctx, call, byName, s.sink)
		}(position, call)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for {
		select {
		case <-done:
			return results, s.out.flushTools()
		case <-time.After(drainInterval):
			if !s.out.flushTools() {
				return nil, false
			}
		}
	}
}

// executeOne runs one local call and builds the tool_result block the model
// will read, reporting the outcome to the sink as it goes.
//
// A call naming no local tool is answered as a failure, matching how the SDK's
// runner answered an unknown name, so the model gets a result it can act on
// rather than a call that never resolves.
func executeOne(
	ctx context.Context,
	call *nacelle.ToolEvent,
	byName map[string]nacelle.Tool,
	sink *nacelle.ToolSink,
) sdk.BetaContentBlockParamUnion {
	tool, known := byName[call.Name]
	if !known {
		err := fmt.Errorf("nacelle: no tool named %q is available", call.Name)
		sink.Report(failedEvent(call, err))
		return sdk.NewBetaToolResultBlock(call.ID, err.Error(), true)
	}

	result, err := nacelle.RunTool(ctx, tool, nacelle.Invocation{
		ID: call.ID, Name: call.Name, Index: call.Index,
	}, json.RawMessage(call.Input), sink)
	if err != nil {
		return sdk.NewBetaToolResultBlock(call.ID, err.Error(), true)
	}
	return sdk.NewBetaToolResultBlock(call.ID, result, false)
}

// failedEvent is the result reported for a call nothing could answer: the tool
// does not exist, which is a failure of this run, not of the model's ask.
func failedEvent(call *nacelle.ToolEvent, err error) nacelle.Event {
	return nacelle.Event{Kind: nacelle.KindToolResult, Tool: &nacelle.ToolEvent{
		ID: call.ID, Index: call.Index, Name: call.Name,
		Input: call.Input, Result: err.Error(), Err: err,
	}}
}
