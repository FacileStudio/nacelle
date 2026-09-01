package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
)

var errStopped = errors.New("nacelle/openai: the consumer stopped ranging")

// Stream runs the conversation, driving the tool loop itself.
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

func answer(ctx context.Context, messages []oai.ChatCompletionMessageParamUnion, turn *turnResult, call callContext) ([]oai.ChatCompletionMessageParamUnion, error) {
	results, err := runCalls(ctx, turn.calls, call)
	if err != nil {
		return nil, err
	}
	return append(append(messages, turn.assistant), results...), nil
}

type callContext struct {
	tools  []oai.ChatCompletionToolUnionParam
	byName map[string]nacelle.Tool
	sink   *nacelle.ToolSink
	out    *emitter
}

func runCalls(ctx context.Context, calls []toolCall, call callContext) ([]oai.ChatCompletionMessageParamUnion, error) {
	results := make([]oai.ChatCompletionMessageParamUnion, 0, len(calls))

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

func (c toolCall) event(index int) nacelle.Event {
	return nacelle.Event{
		Kind: nacelle.KindToolCall,
		Tool: &nacelle.ToolEvent{ID: c.id, Index: index, Name: c.name, Input: c.arguments},
	}
}

func runCall(ctx context.Context, invocation toolCall, index int, call callContext) oai.ChatCompletionMessageParamUnion {
	tool, known := call.byName[invocation.name]
	if !known {
		err := fmt.Errorf("nacelle/openai: no tool named %q is available", invocation.name)
		call.sink.Report(nacelle.Event{
			Kind: nacelle.KindToolResult,
			Tool: &nacelle.ToolEvent{
				ID: invocation.id, Index: index, Name: invocation.name,
				Input: invocation.arguments, Result: err.Error(), Err: err,
			},
		})
		return oai.ToolMessage(fmt.Sprintf("no tool named %q is available", invocation.name), invocation.id)
	}

	result, err := nacelle.RunTool(ctx, tool, nacelle.Invocation{ID: invocation.id, Index: index}, json.RawMessage(invocation.arguments), call.sink)
	if err != nil {
		result = "the tool failed: " + err.Error()
	}
	return oai.ToolMessage(result, invocation.id)
}
