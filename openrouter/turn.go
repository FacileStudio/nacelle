package openrouter

import (
	"context"
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
// It is a struct rather than five locals threaded through the chunk handler
// because the handler needs all of them and a function taking six parameters
// is a function nobody can call correctly. Keeping them together also makes
// the ordering constraint visible: the finish reason and the usage have to be
// folded in before the turn is emitted, and any chunk can carry either.
type turnStream struct {
	accumulator openai.ChatCompletionAccumulator
	reasoning   details
	stop        nacelle.Stop
	usage       nacelle.Usage
	total       *nacelle.Usage
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

// observe folds one chunk into the turn's running state, before any of it
// reaches the consumer.
//
// A stream that never carries a finish_reason leaves the turn on StopOther,
// which is the honest answer: the provider did not say why it stopped, so
// neither can this package.
//
// Usage is latched rather than added, because the two providers that get this
// wrong get it wrong in opposite directions: one sends the accounting once in
// a chunk of its own, the other repeats a running total on every chunk. Adding
// would triple the second one's numbers, so the last figure seen wins and the
// turn is billed for it exactly once, in finish.
//
// The empty-choices guard is not defensive padding: the usage chunk carries no
// choices at all, so indexing it would panic on every single run.
func (t *turnStream) observe(chunk openai.ChatCompletionChunk) {
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
	if raw, ok := extra(chunk.Choices[0].Delta.JSON.ExtraFields, "reasoning_details"); ok {
		t.reasoning.add(raw)
	}
}

// emit reports one observed chunk, returning false when the consumer has
// stopped ranging. It carries the answer as it arrives and nothing else: what
// the turn cost and why it ended are known only once the stream is over, so
// they are reported by finish.
func (t *turnStream) emit(chunk openai.ChatCompletionChunk, thinking bool, out *emitter) bool {
	if !out.flushTools() {
		return false
	}
	if len(chunk.Choices) == 0 {
		return true
	}
	return out.sendAll(deltaEvents(chunk.Choices[0].Delta, thinking))
}

// finish ends the turn and turns the accumulated message into the next turn's
// input.
//
// The KindTurn goes out here, unconditionally, because the promise is that
// usage is reported per turn always — and a stream with no usage chunk at all
// used to produce no KindTurn, which dropped the turn's stop reason with it. A
// turn that cost nothing reportable is a turn with a zero usage, not a missing
// event. It is emitted before the response is inspected for the same reason:
// the generation was billed whether or not it came back usable.
func (t *turnStream) finish(out *emitter) (*turnResult, error) {
	*t.total = t.total.Add(t.usage)
	if !out.send(nacelle.Event{Kind: nacelle.KindTurn, Usage: t.usage, Stop: t.stop}) {
		return nil, errStopped
	}
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
		assistant: assistantMessage(message, t.reasoning.blocks),
		calls:     calls,
		stop:      t.stop,
	}, nil
}

// assistantMessage rebuilds the model's turn for the next request.
//
// The reasoning goes back with it. A tool call is the model pausing mid-thought
// to wait for something it cannot look up itself, and the turn that follows is
// meant to be the same thought continuing; a loop that replays the turn without
// the reasoning has quietly asked the model to start again from what it said
// out loud. OpenRouter's rule is that the blocks must match what the model
// produced, in order and unmodified, which is why they are reassembled in
// details rather than taken off the last chunk that happened to carry them.
//
// The blocks are set as an extra field because the OpenAI schema has no place
// for them. They are handed over as decoded objects rather than raw JSON so
// that the SDK encodes them once, on its own terms, instead of this package
// marshalling a string that then has to survive being embedded in another
// document.
func assistantMessage(message openai.ChatCompletionMessage, blocks []map[string]any) openai.ChatCompletionMessageParamUnion {
	assistant := message.ToParam()
	if len(blocks) > 0 && assistant.OfAssistant != nil {
		assistant.OfAssistant.SetExtraFields(map[string]any{"reasoning_details": blocks})
	}
	return assistant
}
