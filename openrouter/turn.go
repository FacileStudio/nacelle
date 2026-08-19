package openrouter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/FacileStudio/nacelle"

	"github.com/openai/openai-go/v3"
)

// turnResult is one assistant turn: what it said, what it wants run, and why
// it ended.
type turnResult struct {
	assistant openai.ChatCompletionMessageParamUnion
	calls     []toolCall
	stop      nacelle.Stop
}

// toolCall is one call the model asked for, reassembled from the stream.
type toolCall struct {
	id        string
	name      string
	arguments string
}

// turnStream is everything one streamed turn accumulates before it can be
// reported.
//
// It is a struct rather than four locals threaded through the chunk handler
// because the handler needs all of them and a function taking six parameters
// is a function nobody can call correctly. Keeping them together also makes
// the ordering constraint visible: the finish reason has to be folded in
// before the turn is emitted, since both can arrive on the same chunk.
type turnStream struct {
	accumulator      openai.ChatCompletionAccumulator
	reasoningDetails json.RawMessage
	stop             nacelle.Stop
	total            *nacelle.Usage
}

// turn streams one assistant turn, emitting its text and reasoning as it
// arrives, and returns what the model wants done next.
//
// The tool schema goes on every request, including the one that follows a tool
// result: OpenRouter validates it per call, and a follow-up that omits it is a
// different conversation to the router.
//
// It returns errStopped when the consumer has abandoned the sequence.
func (b *Backend) turn(
	ctx context.Context,
	messages []openai.ChatCompletionMessageParamUnion,
	request nacelle.Request,
	call callContext,
	total *nacelle.Usage,
) (*turnResult, error) {
	params := openai.ChatCompletionNewParams{
		Model:     b.model,
		Messages:  messages,
		MaxTokens: openai.Int(request.MaxTokens),
	}
	if len(call.tools) > 0 {
		params.Tools = call.tools
	}

	stream := b.client.Chat.Completions.NewStreaming(ctx, params, b.requestOptions(request)...)
	defer stream.Close()

	state := &turnStream{stop: nacelle.StopOther, total: total}
	for stream.Next() {
		chunk := stream.Current()
		state.observe(chunk)
		if !state.emit(chunk, request.Thinking, call.out) {
			return nil, errStopped
		}
	}
	if err := stream.Err(); err != nil {
		return nil, classify(err)
	}

	return state.finish()
}

// observe folds one chunk into the turn's running state, before any of it
// reaches the consumer.
//
// A stream that never carries a finish_reason leaves the turn on StopOther,
// which is the honest answer: the provider did not say why it stopped, so
// neither can this package.
//
// The empty-choices guard is not defensive padding: the usage chunk carries no
// choices at all, so indexing it would panic on every single run.
func (t *turnStream) observe(chunk openai.ChatCompletionChunk) {
	t.accumulator.AddChunk(chunk)
	if len(chunk.Choices) == 0 {
		return
	}
	if reason := chunk.Choices[0].FinishReason; reason != "" {
		t.stop = stopOf(reason)
	}
	if raw, ok := extra(chunk.Choices[0].Delta.JSON.ExtraFields, "reasoning_details"); ok {
		t.reasoningDetails = raw
	}
}

// emit reports one observed chunk, returning false when the consumer has
// stopped ranging.
//
// The turn is ended by the usage chunk rather than by the finish_reason one,
// because a KindTurn that carried no cost would break the promise that usage
// is reported per turn, always.
func (t *turnStream) emit(chunk openai.ChatCompletionChunk, thinking bool, out *emitter) bool {
	if !out.flushTools() {
		return false
	}
	if usage, ok := usageOf(chunk); ok {
		*t.total = t.total.Add(usage)
		if !out.send(nacelle.Event{Kind: nacelle.KindTurn, Usage: usage, Stop: t.stop}) {
			return false
		}
	}
	if len(chunk.Choices) == 0 {
		return true
	}
	return out.sendAll(deltaEvents(chunk.Choices[0].Delta, thinking))
}

// finish turns the accumulated message into the next turn's input.
func (t *turnStream) finish() (*turnResult, error) {
	if len(t.accumulator.Choices) == 0 {
		return nil, fmt.Errorf("nacelle/openrouter: the response carried no choices")
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

	return &turnResult{
		assistant: assistantMessage(message, t.reasoningDetails),
		calls:     calls,
		stop:      t.stop,
	}, nil
}

// assistantMessage rebuilds the model's turn for the next request.
//
// reasoning_details is echoed back exactly as it arrived. Reasoning models
// require the sequence of reasoning blocks to match what they produced, and a
// tool loop that drops them loses the model's train of thought at precisely
// the moment it is waiting on a tool result.
func assistantMessage(message openai.ChatCompletionMessage, reasoningDetails json.RawMessage) openai.ChatCompletionMessageParamUnion {
	assistant := message.ToParam()
	if len(reasoningDetails) > 0 && assistant.OfAssistant != nil {
		var decoded any
		if err := json.Unmarshal(reasoningDetails, &decoded); err == nil {
			assistant.OfAssistant.SetExtraFields(map[string]any{"reasoning_details": decoded})
		}
	}
	return assistant
}
