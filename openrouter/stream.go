package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"

	"github.com/FacileStudio/nacelle"

	"github.com/openai/openai-go/v3"
)

// Stream runs the conversation, driving the tool loop itself.
//
// The loop is hand-written because the OpenAI schema has no equivalent of the
// Anthropic SDK's runner. It is the same shape every time: ask, and if the
// answer is a set of tool calls, run them, append the results, and ask again.
func (b *Backend) Stream(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		sink := &nacelle.ToolSink{Approve: request.Approve, Hooks: request.Hooks}
		out := &emitter{yield: yield, sink: sink}

		total, stop, err := b.converse(ctx, request, out, sink)
		switch {
		case errors.Is(err, errStopped):
			return
		case err != nil:
			out.fail(err)
			return
		}
		if !out.flushTools() {
			return
		}
		out.send(nacelle.Event{Kind: nacelle.KindDone, Usage: total, Stop: stop})
	}
}

// errStopped means the consumer stopped ranging over the sequence.
//
// It is a sentinel rather than a third return value because every step of the
// loop can end that way, and threading an ok bool through each one is what
// turns a readable loop into a chain of guards.
var errStopped = errors.New("nacelle/openrouter: the consumer stopped ranging")

// converse runs the ask-and-answer loop until the model stops asking for
// tools, returning what the run cost and why it ended.
//
// The iteration counter is one-based because MaxIterations counts requests,
// not rounds: N permits N requests and the tool rounds between them, so the
// Nth turn's tools are the ones nobody asked for. Every reason to stop is
// decided by refuse before anything is executed, which is the whole point —
// see guard.go for why a turn's tools can be refused after the model has
// already asked for them.
//
// Reaching MaxIterations ends the run as StopIterations rather than as an
// error. The model was still working and the caller set the ceiling, so
// nothing went wrong; failing here would also throw away the accumulated
// usage, because a failed sequence never reaches the KindDone that carries it,
// and usage this package could not report is usage nobody can reconstruct.
func (b *Backend) converse(ctx context.Context, request nacelle.Request, out *emitter, sink *nacelle.ToolSink) (nacelle.Usage, nacelle.Stop, error) {
	messages := b.messages(request)
	call := callContext{
		tools:  toolParams(request.Tools),
		byName: nacelle.ToolsByName(request.Tools),
		sink:   sink,
		out:    out,
	}

	var total nacelle.Usage
	for iteration := 1; ; iteration++ {
		turn, err := b.turn(ctx, messages, request, call, &total)
		if err != nil {
			return total, nacelle.StopOther, err
		}
		if stop, refused := refuse(turn, iteration, request.MaxIterations); refused {
			return total, stop, announce(turn.calls, out)
		}

		messages, err = answer(ctx, messages, turn, call)
		if err != nil {
			return total, nacelle.StopOther, err
		}
	}
}

// answer appends the model's turn and the results of the tools it asked for,
// returning the conversation to send next.
func answer(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion, turn *turnResult, call callContext) ([]openai.ChatCompletionMessageParamUnion, error) {
	results, err := runCalls(ctx, turn.calls, call)
	if err != nil {
		return nil, err
	}
	return append(append(messages, turn.assistant), results...), nil
}

// callContext is what every turn of one run needs and none of it changes:
// the tool schema, the tools themselves, and where results go.
type callContext struct {
	tools  []openai.ChatCompletionToolUnionParam
	byName map[string]nacelle.Tool
	sink   *nacelle.ToolSink
	out    *emitter
}

// runCalls executes every tool the model asked for and builds the messages
// carrying their results.
//
// Each call is announced before it runs, which is the whole reason KindToolCall
// exists: this backend can spend a long time inside a tool, and a consumer with
// no event to show sits in silence wondering whether anything is happening.
//
// It returns errStopped when the consumer has abandoned the sequence.
func runCalls(ctx context.Context, calls []toolCall, call callContext) ([]openai.ChatCompletionMessageParamUnion, error) {
	results := make([]openai.ChatCompletionMessageParamUnion, 0, len(calls))

	for index, invocation := range calls {
		if !call.out.send(invocation.event(index)) {
			return nil, errStopped
		}
		results = append(results, runCall(ctx, invocation, index, call))
	}

	if !call.out.flushTools() {
		return nil, errStopped
	}
	return results, nil
}

// event announces the call before it runs. Index is the model's own ordering,
// which the results cannot carry back on their own: tools are reported as they
// finish, so a consumer pairing them by arrival gets whichever order the work
// happened to take.
func (c toolCall) event(index int) nacelle.Event {
	return nacelle.Event{
		Kind: nacelle.KindToolCall,
		Tool: &nacelle.ToolEvent{ID: c.id, Index: index, Name: c.name, Input: c.arguments},
	}
}

// runCall executes one tool and renders the message the model is told about it.
//
// A tool that fails still produces a result message. The model asked for it,
// the model is told what happened, and it decides whether the task can still
// be finished — dropping the message instead would leave a tool_call with no
// answer, which most providers reject outright on the next request.
//
// A call naming a tool that does not exist is reported to the sink as a failed
// result rather than answered in silence, so that every KindToolCall this
// backend emits is closed by a KindToolResult carrying the same ID.
func runCall(ctx context.Context, invocation toolCall, index int, call callContext) openai.ChatCompletionMessageParamUnion {
	tool, known := call.byName[invocation.name]
	if !known {
		err := fmt.Errorf("nacelle/openrouter: no tool named %q is available", invocation.name)
		call.sink.Report(nacelle.Event{
			Kind: nacelle.KindToolResult,
			Tool: &nacelle.ToolEvent{
				ID: invocation.id, Index: index, Name: invocation.name,
				Input: invocation.arguments, Result: err.Error(), Err: err,
			},
		})
		return openai.ToolMessage(fmt.Sprintf("no tool named %q is available", invocation.name), invocation.id)
	}

	result, err := nacelle.RunTool(ctx, tool, nacelle.Invocation{ID: invocation.id, Index: index}, json.RawMessage(invocation.arguments), call.sink)
	if err != nil {
		result = "the tool failed: " + err.Error()
	}
	return openai.ToolMessage(result, invocation.id)
}
