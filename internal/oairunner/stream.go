// Package oairunner runs agents on OpenAI-compatible APIs.
package oairunner

import (
	"context"
	"errors"
	"iter"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Options customizes the OpenAI runner behavior.
type Options struct {
	RequestOptions func(request nacelle.Request) []option.RequestOption
	ClassifyError  func(err error) error
	ResetTurn      func()
	ObserveChunk   func(chunk oai.ChatCompletionChunk)
	FinishExtra    func(message oai.ChatCompletionMessage, assistant oai.ChatCompletionMessageParamUnion) oai.ChatCompletionMessageParamUnion
}

// Backend runs agents on an OpenAI-compatible API.
type Backend struct {
	Client         oai.Client
	Model          string
	Provider       map[string]any
	Options        Options
	RequestOptions func(request nacelle.Request) []option.RequestOption
}

var errStopped = errors.New("nacelle: the consumer stopped ranging")

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
			if b.Options.ClassifyError != nil {
				err = b.Options.ClassifyError(err)
			}
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
func (b *Backend) CountTokens(ctx context.Context, request nacelle.Request) (int64, error) {
	return 0, &nacelle.Unsupported{Backend: "oairunner", Feature: "token counting"}
}
