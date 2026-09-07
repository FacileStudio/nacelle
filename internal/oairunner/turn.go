// Package oairunner runs agents on OpenAI-compatible APIs.
package oairunner

import (
	"context"
	"fmt"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type turnResult struct {
	assistant oai.ChatCompletionMessageParamUnion
	calls     []toolCall
	stop      nacelle.Stop
}

type turnStream struct {
	accumulator oai.ChatCompletionAccumulator
	backend     *Backend
	stop        nacelle.Stop
	usage       nacelle.Usage
	total       *nacelle.Usage
}

func (b *Backend) requestOptions(request nacelle.Request) []option.RequestOption {
	if b.RequestOptions != nil {
		return b.RequestOptions(request)
	}
	if b.Options.RequestOptions != nil {
		return b.Options.RequestOptions(request)
	}
	return nil
}

func (b *Backend) turn(
	ctx context.Context,
	messages []oai.ChatCompletionMessageParamUnion,
	request nacelle.Request,
	call callContext,
	total *nacelle.Usage,
) (*turnResult, error) {
	if b.Options.ResetTurn != nil {
		b.Options.ResetTurn()
	}
	params := oai.ChatCompletionNewParams{
		Model:     b.Model,
		Messages:  messages,
		MaxTokens: oai.Int(request.MaxTokens),
	}
	if len(call.tools) > 0 {
		params.Tools = call.tools
	}
	stream := b.Client.Chat.Completions.NewStreaming(ctx, params, b.requestOptions(request)...)
	defer func() { _ = stream.Close() }()

	state := &turnStream{backend: b, stop: nacelle.StopOther, total: total}
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
	if t.backend.Options.FinishExtra != nil {
		assistant = t.backend.Options.FinishExtra(message, assistant)
	}

	return &turnResult{
		assistant: assistant,
		calls:     calls,
		stop:      t.stop,
	}, nil
}
