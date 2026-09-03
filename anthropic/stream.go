package anthropic

import (
	"context"
	"iter"
	"strings"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const BetaCompaction = "compact-2026-01-12"

func isCompactRequested(ctx context.Context, b *Backend) bool {
	if b.compact {
		return true
	}
	if v, ok := ctx.Value(compactKey{}).(bool); ok && v {
		return true
	}
	if v, ok := ctx.Value("compact").(bool); ok && v {
		return true
	}
	if v, ok := ctx.Value("compact-2026-01-12").(bool); ok && v {
		return true
	}
	if v, ok := ctx.Value("anthropic-beta").(string); ok && strings.Contains(v, BetaCompaction) {
		return true
	}
	return false
}

// session is what every turn of one run needs and none of it changes: where
// the calls of the turn just closed are filed, whether the consumer asked for
// reasoning, and the one door events leave by.
//
// It is a struct rather than three more parameters threaded through each turn
// because a function taking six arguments is a function nobody calls
// correctly, and because it keeps the run-scoped state visibly run-scoped.
type session struct {
	pending  *invocations
	thinking bool
	out      *emitter
}

// Stream runs the conversation on the SDK's streaming tool runner.
func (b *Backend) Stream(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		sink := &nacelle.ToolSink{Approve: request.Approve, Hooks: request.Hooks}
		state := &session{
			pending:  newInvocations(),
			thinking: request.Thinking.Show,
			out:      &emitter{yield: yield, sink: sink},
		}

		params := b.params(request)
		opts := make([]option.RequestOption, len(b.options))
		copy(opts, b.options)
		if isCompactRequested(ctx, b) {
			params.Betas = append(params.Betas, sdk.AnthropicBeta(BetaCompaction))
			opts = append(opts, option.WithHeaderAdd("anthropic-beta", BetaCompaction))
		}
		runner := b.client.Beta.Messages.NewToolRunnerStreaming(adapt(request.Tools, sink, state.pending), params, opts...)

		if run, ok := runTurns(ctx, runner, state); ok {
			state.out.send(nacelle.Event{Kind: nacelle.KindDone, Usage: run.usage, Stop: run.stop})
		}
	}
}

// runTurns drives the runner to completion, returning what the run cost, why
// it ended, and whether it finished well enough to report a KindDone.
func runTurns(ctx context.Context, runner *sdk.BetaToolRunnerStreaming, state *session) (outcome, bool) {
	var run outcome

	for turn, err := range runner.AllStreaming(ctx) {
		if err != nil {
			state.out.fail(err)
			return run, false
		}
		if !streamTurn(turn, state, &run) {
			return run, false
		}
	}

	if err := runner.Err(); err != nil {
		state.out.fail(err)
		return run, false
	}
	run.stop = finalStop(runner, run.stop)
	return run, state.out.flushTools()
}

// streamTurn maps one assistant turn onto the event stream, adding what it
// cost to the run. It reports whether the consumer is still ranging.
func streamTurn(turn iter.Seq2[sdk.BetaRawMessageStreamEventUnion, error], state *session, run *outcome) bool {
	calls := newCallTracker(state.pending, state.thinking)

	for event, err := range turn {
		if err != nil {
			state.out.fail(err)
			return false
		}
		if !state.out.flushTools() {
			return false
		}
		if !state.out.sendAll(calls.consume(event)) {
			return false
		}
		if !state.out.sendAll(turnEnd(event, run)) {
			return false
		}
	}

	return state.out.flushTools()
}
