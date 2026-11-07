package anthropic

import (
	"context"
	"iter"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// BetaCompaction is the beta header that turns on server-side compaction.
const BetaCompaction = "compact-2026-01-12"

// compactRequested reports whether this run asks for server-side compaction,
// either from Config.Compact or from a context built by WithCompact.
func compactRequested(ctx context.Context, b *Backend) bool {
	if b.compact {
		return true
	}
	v, _ := ctx.Value(compactKey{}).(bool)
	return v
}

// Stream runs the conversation on nacelle's agent loop.
func (b *Backend) Stream(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return nacelle.NewBackend(b).Stream(ctx, request)
}

// Turn streams one assistant turn.
//
// It deliberately does not use the SDK's BetaToolRunnerStreaming, which
// blocks a turn's tools to completion before it streams the next turn. The
// SDK's accumulator builds the turn message; the call tracker maps content
// blocks to events; remote MCP calls are closed here, because Anthropic ran
// them, and local calls are left to the loop.
func (b *Backend) Turn(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		params, opts := b.params(request), append([]option.RequestOption(nil), b.options...)
		if compactRequested(ctx, b) {
			params.Betas = append(params.Betas, sdk.AnthropicBeta(BetaCompaction))
			opts = append(opts, option.WithHeaderAdd("anthropic-beta", BetaCompaction))
		}
		calls := newCallTracker(request.Thinking.Show)
		assistant, ok := b.streamOne(ctx, params, opts, calls, yield)
		if !ok || !sendAll(yield, calls.finish()) {
			return
		}
		message := messageOf(assistant, calls)
		yield(nacelle.Event{Kind: nacelle.KindMessage, Message: &message}, nil)
	}
}

// streamOne streams the turn's raw events onto yield and returns the
// accumulated message, or false when the stream failed or the consumer
// stopped.
func (b *Backend) streamOne(
	ctx context.Context,
	params sdk.BetaMessageNewParams,
	opts []option.RequestOption,
	calls *callTracker,
	yield func(nacelle.Event, error) bool,
) (*sdk.BetaMessage, bool) {
	stream := b.client.Beta.Messages.NewStreaming(ctx, params, opts...)
	defer func() { _ = stream.Close() }()

	var assistant sdk.BetaMessage
	for stream.Next() {
		event := stream.Current()
		if err := assistant.Accumulate(event); err != nil {
			yield(nacelle.Event{}, classify(err))
			return nil, false
		}
		if !sendAll(yield, calls.consume(event)) || !sendAll(yield, turnEnd(event)) {
			return nil, false
		}
	}
	if err := stream.Err(); err != nil {
		yield(nacelle.Event{}, classify(err))
		return nil, false
	}
	return &assistant, true
}

// sendAll yields events in order, reporting whether the consumer is still
// ranging.
func sendAll(yield func(nacelle.Event, error) bool, events []nacelle.Event) bool {
	for _, event := range events {
		if !yield(event, nil) {
			return false
		}
	}
	return true
}
