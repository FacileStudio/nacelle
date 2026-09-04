package oairunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/respjson"
)

// Options customizes the OpenAI runner behavior.
type Options struct {
	// RequestOptions is called to build per-request options. If nil, a default
	// that only adds reasoning_effort is used.
	RequestOptions func(request nacelle.Request) []option.RequestOption

	// UsageOf extracts usage from a chunk. If nil, a default that reads the
	// standard OpenAI fields is used.
	UsageOf func(chunk oai.ChatCompletionChunk) (nacelle.Usage, bool)

	// DeltaEvents maps a delta to nacelle events. If nil, a default that emits
	// text and thinking (when enabled) is used.
	DeltaEvents func(delta oai.ChatCompletionChunkChoiceDelta, thinking bool) []nacelle.Event

	// StopOf maps a finish_reason to nacelle.Stop. If nil, a default that
	// handles the standard OpenAI values is used.
	StopOf func(reason string) nacelle.Stop

	// ObserveExtra is called for each chunk with the delta's extra fields, after
	// the standard observe logic. It can be used to accumulate provider-specific
	// data like reasoning_details.
	ObserveExtra func(delta oai.ChatCompletionChunkChoiceDelta)

	// FinishExtra is called when a turn finishes, allowing the backend to modify
	// the assistant message before it's added to the conversation. Return the
	// possibly-modified assistant message param.
	FinishExtra func(message oai.ChatCompletionMessage, assistant oai.ChatCompletionMessageParamUnion) oai.ChatCompletionMessageParamUnion
}

// Backend runs agents on an OpenAI-compatible API.
type Backend struct {
	Client       oai.Client
	Model        string
	Provider     map[string]any
	Options      Options
	RequestOptions func(request nacelle.Request) []option.RequestOption
}

var errStopped = errors.New("nacelle: the consumer stopped ranging")




type turnResult struct {
	assistant oai.ChatCompletionMessageParamUnion
	calls     []toolCall
	stop      nacelle.Stop
}

type toolCall struct {
	id        string
	name      string
	arguments string
}

type turnStream struct {
	accumulator oai.ChatCompletionAccumulator
	stop        nacelle.Stop
	usage       nacelle.Usage
	total       *nacelle.Usage
}

func (b *Backend) turn(
	ctx context.Context,
	messages []oai.ChatCompletionMessageParamUnion,
	request nacelle.Request,
	call callContext,
	total *nacelle.Usage,
) (*turnResult, error) {
	params := oai.ChatCompletionNewParams{
		Model:     b.Model,
		Messages:  messages,
		MaxTokens: oai.Int(request.MaxTokens),
	}
	if len(call.tools) > 0 {
		params.Tools = call.tools
	}

	// Allow custom request options
	var reqOpts []option.RequestOption
	if b.RequestOptions != nil {
		reqOpts = b.RequestOptions(request)
	}
	stream := b.Client.Chat.Completions.NewStreaming(ctx, params, reqOpts...)
	defer func() { _ = stream.Close() }()

	state := &turnStream{stop: nacelle.StopOther, total: total}
	for stream.Next() {
		chunk := stream.Current()
		state.observe(chunk)
		if !state.emit(chunk, request.Thinking.Show, call.out) {
			return nil, errStopped
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}

	return state.finish(call.out)
}

func (t *turnStream) observe(chunk oai.ChatCompletionChunk) {
	t.accumulator.AddChunk(chunk)
	if usage, ok := usageOf(chunk); ok {
		t.usage = usage
	}
	if len(chunk.Choices) == 0 {
		return
	}
	if reason := chunk.Choices[0].FinishReason; reason != "" {
		t.stop = stopOf(reason)
	}
}

func (t *turnStream) emit(chunk oai.ChatCompletionChunk, thinking bool, out *emitter) bool {
	if !out.flushTools() {
		return false
	}
	if len(chunk.Choices) == 0 {
		return true
	}
	return out.sendAll(deltaEvents(chunk.Choices[0].Delta, thinking))
}

func (t *turnStream) finish(out *emitter) (*turnResult, error) {
	*t.total = t.total.Add(t.usage)
	if !out.send(nacelle.Event{Kind: nacelle.KindTurn, Usage: t.usage, Stop: t.stop}) {
		return nil, errStopped
	}
	if len(t.accumulator.Choices) == 0 {
		return nil, fmt.Errorf("nacelle: the response carried no choices")
	}
	message := t.accumulator.Choices[0].Message

	calls := make([]toolCall, 0, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		calls = append(calls, toolCall{
			id:        call.ID,
			name:      call.Function.Name,
			arguments: call.Function.Arguments,
		})
	}

	assistant := message.ToParam()

	return &turnResult{
		assistant: assistant,
		calls:     calls,
		stop:      t.stop,
	}, nil
}

type callContext struct {
	tools  []oai.ChatCompletionToolUnionParam
	byName map[string]nacelle.Tool
	sink   *nacelle.ToolSink
	out    *emitter
}

func planCalls(calls []toolCall, byName map[string]nacelle.Tool) ([]toolCall, []int) {
	if len(calls) <= 1 {
		indices := make([]int, len(calls))
		for i := range indices {
			indices[i] = i
		}
		return calls, indices
	}
	invocations := make([]nacelle.Invocation, len(calls))
	for i, c := range calls {
		invocations[i] = nacelle.Invocation{ID: c.id, Name: c.name, Index: i}
	}
	planned := nacelle.PlanCalls(invocations, byName)
	order := make([]toolCall, len(planned))
	indices := make([]int, len(planned))
	for i, p := range planned {
		order[i] = calls[p.Index]
		indices[i] = p.Index
	}
	return order, indices
}

func runCalls(ctx context.Context, calls []toolCall, call callContext) ([]oai.ChatCompletionMessageParamUnion, error) {
	order, indices := planCalls(calls, call.byName)
	results := make([]oai.ChatCompletionMessageParamUnion, 0, len(order))
	for i, invocation := range order {
		origIndex := indices[i]
		if !call.out.send(invocation.event(origIndex)) {
			return nil, errStopped
		}
		results = append(results, runCall(ctx, invocation, origIndex, call))
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
		err := fmt.Errorf("nacelle: no tool named %q is available", invocation.name)
		call.sink.Report(nacelle.Event{
			Kind: nacelle.KindToolResult,
			Tool: &nacelle.ToolEvent{
				ID: invocation.id, Index: index, Name: invocation.name,
				Input: invocation.arguments, Result: err.Error(), Err: err,
			},
		})
		return oai.ToolMessage(fmt.Sprintf("no tool named %q is available", invocation.name), invocation.id)
	}

	result, err := nacelle.RunTool(ctx, tool, nacelle.Invocation{ID: invocation.id, Name: invocation.name, Index: index}, json.RawMessage(invocation.arguments), call.sink)
	if err != nil {
		result = "the tool failed: " + err.Error()
	}
	return oai.ToolMessage(result, invocation.id)
}

// Default implementations that match the original openai/google behavior.

func toolParams(tools []nacelle.Tool) []oai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	ordered := slices.Clone(tools)
	slices.SortStableFunc(ordered, func(a, b nacelle.Tool) int {
		return strings.Compare(a.Name(), b.Name())
	})

	params := make([]oai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range ordered {
		params = append(params, oai.ChatCompletionFunctionTool(oai.FunctionDefinitionParam{
			Name:        tool.Name(),
			Description: oai.String(tool.Description()),
			Parameters:  oai.FunctionParameters(tool.Schema()),
		}))
	}
	return params
}

func deltaEvents(delta oai.ChatCompletionChunkChoiceDelta, thinking bool) []nacelle.Event {
	var events []nacelle.Event

	if thinking {
		if raw, ok := extra(delta.JSON.ExtraFields, "reasoning"); ok {
			var text string
			if err := json.Unmarshal(raw, &text); err == nil && text != "" {
				events = append(events, nacelle.Event{Kind: nacelle.KindThinking, Text: text})
			}
		}
	}
	if delta.Content != "" {
		events = append(events, nacelle.Event{Kind: nacelle.KindText, Text: delta.Content})
	}
	return events
}

func usageOf(chunk oai.ChatCompletionChunk) (nacelle.Usage, bool) {
	if chunk.Usage.TotalTokens == 0 && chunk.Usage.PromptTokens == 0 {
		return nacelle.Usage{}, false
	}
	u := nacelle.Usage{
		InputTokens:     chunk.Usage.PromptTokens,
		OutputTokens:    chunk.Usage.CompletionTokens,
		CacheReadTokens: chunk.Usage.PromptTokensDetails.CachedTokens,
	}
	if raw, ok := extra(chunk.Usage.JSON.ExtraFields, "cost"); ok {
		var cost float64
		if err := json.Unmarshal(raw, &cost); err == nil {
			u.Cost = cost
		}
	}
	return u, true
}

func extra(fields map[string]respjson.Field, name string) (json.RawMessage, bool) {
	field, present := fields[name]
	if !present {
		return nil, false
	}
	raw := field.Raw()
	if raw == "" || raw == "null" {
		return nil, false
	}
	return json.RawMessage(raw), true
}

func stopOf(reason string) nacelle.Stop {
	switch reason {
	case "stop":
		return nacelle.StopEnd
	case "length":
		return nacelle.StopMaxTokens
	case "tool_calls", "function_call":
		return nacelle.StopTools
	case "content_filter":
		return nacelle.StopRefusal
	default:
		return nacelle.StopOther
	}
}

// converse runs the ask-and-answer loop until the model stops asking for
// tools, returning what the run cost and why it ended.
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
func answer(ctx context.Context, messages []oai.ChatCompletionMessageParamUnion, turn *turnResult, call callContext) ([]oai.ChatCompletionMessageParamUnion, error) {
	results, err := runCalls(ctx, turn.calls, call)
	if err != nil {
		return nil, err
	}
	return append(append(messages, turn.assistant), results...), nil
}

// Stream runs the conversation, yielding events.
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

// CountTokens reports how many tokens this request would use if sent as it is.
// The OpenAI-compatible schema has no server-side token-counting endpoint, so
// this backend returns an Unsupported error rather than a guess.
func (b *Backend) CountTokens(ctx context.Context, request nacelle.Request) (int64, error) {
	return 0, &nacelle.Unsupported{Backend: "oairunner", Feature: "token counting"}
}