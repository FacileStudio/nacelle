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

// session is what every turn of one run needs and none of it changes: the
// request parameters, where the events leave, and whether reasoning is shown.
type session struct {
	thinking bool
	out      *emitter
	base     sdk.BetaMessageNewParams
	opts     []option.RequestOption
	sink     *nacelle.ToolSink
}

// Stream runs the conversation on the agent loop this package owns.
//
// It deliberately does not use the SDK's BetaToolRunnerStreaming. That runner
// blocks a turn's tools to completion before it streams the next turn, so a
// tool's streamed output can only arrive as a burst once the tool finishes.
// Owning the loop passes it the pause: tools run on this package's goroutines,
// the tool sink is drained on a live tick while they run, and KindToolOutput
// reaches the consumer as the line is produced.
func (b *Backend) Stream(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		state := &session{
			thinking: request.Thinking.Show,
			sink:     &nacelle.ToolSink{Approve: request.Approve, Hooks: request.Hooks},
			base:     b.params(request),
			opts:     append([]option.RequestOption(nil), b.options...),
		}
		state.out = &emitter{yield: yield, sink: state.sink}
		if isCompactRequested(ctx, b) {
			state.base.Betas = append(state.base.Betas, sdk.AnthropicBeta(BetaCompaction))
			state.opts = append(state.opts, option.WithHeaderAdd("anthropic-beta", BetaCompaction))
		}

		run, ok := b.loop(ctx, request, state)
		if !ok || !state.out.flushTools() {
			return
		}
		state.out.send(nacelle.Event{Kind: nacelle.KindDone, Usage: run.usage, Stop: run.stop})
	}
}

// loop drives the turns of one run, owning the conversation history the way
// the SDK runner used to.
//
// Each iteration streams one assistant turn, then decides by its stop reason:
// a tool_use turn hands its local calls to execution on the next pass, and a
// terminal turn ends the run. The pending batch is checked against the
// iteration cap before it runs, which is what turns a capped run into a
// StopIterations instead of a finished one.
func (b *Backend) loop(ctx context.Context, request nacelle.Request, s *session) (outcome, bool) {
	var run outcome
	history := toParams(request.Messages)
	var pending []*nacelle.ToolEvent
	iterations := 0

	for {
		if request.MaxIterations > 0 && iterations >= request.MaxIterations {
			run.stop = finalStop(len(pending) > 0, run.stop, iterations, request.MaxIterations)
			return run, true
		}
		if len(pending) > 0 {
			s.base.Messages = history
			results, ok := runCalls(ctx, pending, s, nacelle.ToolsByName(request.Tools))
			if !ok {
				return run, false
			}
			history = append(history, sdk.NewBetaUserMessage(results...))
		}

		iterations++
		s.base.Messages = history
		assistant, queued, ok := b.streamOne(ctx, s, &run)
		if !ok {
			return run, false
		}
		history = append(history, assistant.ToParam())
		if assistant.StopReason != sdk.BetaStopReasonToolUse {
			run.stop = finalStop(false, stopOf(assistant.StopReason), iterations, request.MaxIterations)
			return run, true
		}
		pending = queued
	}
}

// streamOne streams one assistant turn onto the event stream, adding what it
// cost to the run, and returns the turn's message and its queued local calls.
func (b *Backend) streamOne(ctx context.Context, s *session, run *outcome) (*sdk.BetaMessage, []*nacelle.ToolEvent, bool) {
	calls := newCallTracker(s.thinking)
	stream := b.client.Beta.Messages.NewStreaming(ctx, s.base, s.opts...)
	defer func() { _ = stream.Close() }()

	var assistant sdk.BetaMessage
	for stream.Next() {
		event := stream.Current()
		if err := assistant.Accumulate(event); err != nil {
			s.out.fail(err)
			return nil, nil, false
		}
		if !s.out.flushTools() {
			return nil, nil, false
		}
		if !s.out.sendAll(calls.consume(event)) {
			return nil, nil, false
		}
		if !s.out.sendAll(turnEnd(event, run)) {
			return nil, nil, false
		}
	}
	if err := stream.Err(); err != nil {
		s.out.fail(err)
		return nil, nil, false
	}
	if !s.out.sendAll(calls.finish()) {
		return nil, nil, false
	}
	return &assistant, calls.localCalls(), true
}
