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
		sink := &nacelle.ToolSink{}
		out := &emitter{yield: yield, sink: sink}

		total, err := b.converse(ctx, request, out, sink)
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
		out.send(nacelle.Event{Kind: nacelle.KindDone, Usage: total})
	}
}

// errStopped means the consumer stopped ranging over the sequence.
//
// It is a sentinel rather than a third return value because every step of the
// loop can end that way, and threading an ok bool through each one is what
// turns a readable loop into a chain of guards.
var errStopped = errors.New("nacelle/openrouter: the consumer stopped ranging")

// converse runs the ask-and-answer loop until the model stops asking for
// tools, returning what the run cost.
func (b *Backend) converse(ctx context.Context, request nacelle.Request, out *emitter, sink *nacelle.ToolSink) (nacelle.Usage, error) {
	messages := b.messages(request)
	call := callContext{
		tools:  toolParams(request.Tools),
		byName: nacelle.ToolsByName(request.Tools),
		sink:   sink,
		out:    out,
	}

	var total nacelle.Usage
	for iteration := 0; ; iteration++ {
		if request.MaxIterations > 0 && iteration >= request.MaxIterations {
			return total, fmt.Errorf("nacelle/openrouter: stopped after %d tool iterations", request.MaxIterations)
		}

		turn, err := b.turn(ctx, messages, request, call, &total)
		if err != nil {
			return total, err
		}
		if len(turn.calls) == 0 {
			return total, nil
		}

		messages, err = answer(ctx, messages, turn, call)
		if err != nil {
			return total, err
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
// A tool that fails still produces a result message. The model asked for it,
// the model is told what happened, and it decides whether the task can still
// be finished — dropping the message instead would leave a tool_call with no
// answer, which most providers reject outright on the next request.
//
// It returns errStopped when the consumer has abandoned the sequence.
func runCalls(ctx context.Context, calls []toolCall, call callContext) ([]openai.ChatCompletionMessageParamUnion, error) {
	results := make([]openai.ChatCompletionMessageParamUnion, 0, len(calls))

	for _, invocation := range calls {
		tool, known := call.byName[invocation.name]
		if !known {
			results = append(results, openai.ToolMessage(fmt.Sprintf("no tool named %q is available", invocation.name), invocation.id))
			continue
		}

		result, err := nacelle.RunTool(ctx, tool, invocation.id, json.RawMessage(invocation.arguments), call.sink)
		if err != nil {
			result = "the tool failed: " + err.Error()
		}
		results = append(results, openai.ToolMessage(result, invocation.id))
	}

	if !call.out.flushTools() {
		return nil, errStopped
	}
	return results, nil
}
