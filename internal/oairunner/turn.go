package oairunner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// turnStream is one turn in flight: the accumulator that builds its message,
// the stop and usage the chunks have reported, and the reasoning shown.
type turnStream struct {
	accumulator oai.ChatCompletionAccumulator
	backend     *Backend
	stop        nacelle.Stop
	usage       nacelle.Usage
	thinking    bool
	reasoning   strings.Builder
}

// requestOptions returns the per-request options the provider asked for.
func (b *Backend) requestOptions(request nacelle.Request) []option.RequestOption {
	if b.RequestOptions != nil {
		return b.RequestOptions(request)
	}
	if b.Options.RequestOptions != nil {
		return b.Options.RequestOptions(request)
	}
	return nil
}

// turn streams one request, yielding its deltas as they arrive, and returns
// the events that close it: the calls, the turn's cost and its message.
func (b *Backend) turn(
	ctx context.Context,
	messages []oai.ChatCompletionMessageParamUnion,
	request nacelle.Request,
	yield func(nacelle.Event, error) bool,
) ([]nacelle.Event, error) {
	params := oai.ChatCompletionNewParams{
		Model:     b.Model,
		Messages:  messages,
		MaxTokens: oai.Int(request.MaxTokens),
	}
	if tools := toolParams(request.Tools); len(tools) > 0 {
		params.Tools = tools
	}
	stream := b.Client.Chat.Completions.NewStreaming(ctx, params, b.requestOptions(request)...)
	defer func() { _ = stream.Close() }()

	state := &turnStream{backend: b, stop: nacelle.StopOther, thinking: request.Thinking.Show}
	for stream.Next() {
		if !state.forward(stream.Current(), yield) {
			return nil, nil
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return state.finish()
}

// forward folds a chunk into the turn and yields the deltas it carries.
func (t *turnStream) forward(chunk oai.ChatCompletionChunk, yield func(nacelle.Event, error) bool) bool {
	t.observe(chunk)
	if len(chunk.Choices) == 0 {
		return true
	}
	for _, event := range deltaEvents(chunk.Choices[0].Delta, t.thinking) {
		if event.Kind == nacelle.KindThinking {
			t.reasoning.WriteString(event.Text)
		}
		if !yield(event, nil) {
			return false
		}
	}
	return true
}

// observe folds a chunk into the accumulator, usage and stop reason.
func (t *turnStream) observe(chunk oai.ChatCompletionChunk) {
	t.accumulator.AddChunk(chunk)
	if t.backend.Options.ObserveChunk != nil {
		t.backend.Options.ObserveChunk(chunk)
	}
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

// finish seals the turn: a KindToolCall per call, the turn's cost, and the
// assistant message. A provider can close a turn with a tool-call entry that
// carries no id, name or arguments; replaying it sends tool_calls: [{}] and
// the provider answers 400, so the empty entry is dropped.
func (t *turnStream) finish() ([]nacelle.Event, error) {
	if len(t.accumulator.Choices) == 0 {
		return nil, fmt.Errorf("nacelle: the response carried no choices")
	}
	message := t.accumulator.Choices[0].Message
	calls := toolCallsOf(message)

	assistant := nacelle.Message{Role: nacelle.RoleAssistant}
	if t.reasoning.Len() > 0 {
		assistant.Parts = append(assistant.Parts, nacelle.Reasoning{Text: t.reasoning.String()})
	}
	if message.Content != "" {
		assistant.Parts = append(assistant.Parts, nacelle.Text{Text: message.Content})
	}
	events := make([]nacelle.Event, 0, len(calls)+2)
	for index, call := range calls {
		assistant.Parts = append(assistant.Parts, call)
		events = append(events, toolCallEvent(call, index))
	}
	assistant.Parts = append(assistant.Parts, t.native()...)
	return append(events,
		nacelle.Event{Kind: nacelle.KindTurn, Usage: t.usage, Stop: t.stop},
		nacelle.Event{Kind: nacelle.KindMessage, Message: &assistant},
	), nil
}

// native is the provider's extra fields for this turn, as a Native part.
func (t *turnStream) native() []nacelle.Part {
	if t.backend.Options.Extra == nil {
		return nil
	}
	extra := t.backend.Options.Extra()
	if len(extra) == 0 {
		return nil
	}
	data, err := json.Marshal(extra)
	if err != nil {
		return nil
	}
	return []nacelle.Part{nacelle.Native{Provider: t.backend.Options.Name, Data: data}}
}
