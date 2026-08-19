package openrouter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/FacileStudio/nacelle"

	"github.com/openai/openai-go/v3"
)

// turnResult is one assistant turn: what it said, and what it wants run.
type turnResult struct {
	assistant openai.ChatCompletionMessageParamUnion
	calls     []toolCall
}

// toolCall is one call the model asked for, reassembled from the stream.
type toolCall struct {
	id        string
	name      string
	arguments string
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

	accumulator := openai.ChatCompletionAccumulator{}
	var reasoningDetails json.RawMessage

	for stream.Next() {
		chunk := stream.Current()
		accumulator.AddChunk(chunk)
		if !emitChunk(chunk, request.Thinking, call.out, total, &reasoningDetails) {
			return nil, errStopped
		}
	}
	if err := stream.Err(); err != nil {
		return nil, classify(err)
	}

	return finish(&accumulator, reasoningDetails)
}

// emitChunk reports one streamed chunk, returning false when the consumer has
// stopped ranging.
//
// The empty-choices guard is not defensive padding: the usage chunk carries no
// choices at all, so indexing it would panic on every single run.
func emitChunk(
	chunk openai.ChatCompletionChunk,
	thinking bool,
	out *emitter,
	total *nacelle.Usage,
	reasoningDetails *json.RawMessage,
) bool {
	if !out.flushTools() {
		return false
	}
	if usage, ok := usageOf(chunk); ok {
		*total = total.Add(usage)
		if !out.send(nacelle.Event{Kind: nacelle.KindTurn, Usage: usage}) {
			return false
		}
	}
	if len(chunk.Choices) == 0 {
		return true
	}

	delta := chunk.Choices[0].Delta
	if raw, ok := extra(delta.JSON.ExtraFields, "reasoning_details"); ok {
		*reasoningDetails = raw
	}
	return out.sendAll(deltaEvents(delta, thinking))
}

// finish turns the accumulated message into the next turn's input.
func finish(accumulator *openai.ChatCompletionAccumulator, reasoningDetails json.RawMessage) (*turnResult, error) {
	if len(accumulator.Choices) == 0 {
		return nil, fmt.Errorf("nacelle/openrouter: the response carried no choices")
	}
	message := accumulator.Choices[0].Message

	calls := make([]toolCall, 0, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		calls = append(calls, toolCall{
			id:        call.ID,
			name:      call.Function.Name,
			arguments: call.Function.Arguments,
		})
	}

	return &turnResult{
		assistant: assistantMessage(message, reasoningDetails),
		calls:     calls,
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
