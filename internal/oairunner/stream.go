// Package oairunner runs agents on OpenAI-compatible APIs.
package oairunner

import (
	"context"
	"iter"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Options customizes the OpenAI runner behavior.
//
// ObserveChunk sees every raw chunk of a turn, and Extra, called once the
// turn has ended, returns the fields the provider wants sent back on that
// assistant message; they travel in the transcript as a Native part named
// Name and are set on the message whenever it is replayed to this provider.
type Options struct {
	RequestOptions func(request nacelle.Request) []option.RequestOption
	ClassifyError  func(err error) error
	ObserveChunk   func(chunk oai.ChatCompletionChunk)
	Extra          func() map[string]any
	Name           string
	Documents      bool
}

// Backend runs one turn at a time on an OpenAI-compatible API.
type Backend struct {
	Client         oai.Client
	Model          string
	Options        Options
	RequestOptions func(request nacelle.Request) []option.RequestOption
}

// Turn streams one assistant turn.
func (b *Backend) Turn(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		messages, err := b.messages(request)
		if err != nil {
			yield(nacelle.Event{}, b.classify(err))
			return
		}
		events, err := b.turn(ctx, messages, request, yield)
		if err != nil {
			yield(nacelle.Event{}, b.classify(err))
			return
		}
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}
}

// classify maps a failure through the provider's own classifier.
func (b *Backend) classify(err error) error {
	if b.Options.ClassifyError != nil {
		return b.Options.ClassifyError(err)
	}
	return err
}

// CountTokens reports how many tokens this request would use if sent as it is.
func (b *Backend) CountTokens(ctx context.Context, request nacelle.Request) (int64, error) {
	return 0, &nacelle.Unsupported{Backend: "oairunner", Feature: "token counting"}
}
