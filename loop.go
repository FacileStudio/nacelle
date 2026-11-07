package nacelle

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"sync"
)

// looped is a Provider driven through the agent loop.
type looped struct{ Provider }

// run is one agent run: the transcript so far, the usage so far, and the
// consumer it reports to.
type run struct {
	provider Provider
	request  Request
	sink     *sink
	yield    func(Event, error) bool
	total    Usage
	indices  map[string]int
}

// Stream runs the agent loop over the provider.
func (l looped) Stream(ctx context.Context, request Request) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		r := &run{provider: l.Provider, request: request, sink: newSink(request), yield: yield}
		if stop, ok := r.loop(ctx); ok {
			yield(Event{Kind: KindDone, Usage: r.total, Stop: stop}, nil)
		}
	}
}

// loop asks for turns and answers their calls until the run ends, returning
// why it ended, or false when the consumer stopped or the provider failed.
func (r *run) loop(ctx context.Context) (Stop, bool) {
	transcript := slices.Clone(r.request.Messages)
	for iteration := 1; ; iteration++ {
		assistant, stop, ok := r.turn(ctx, transcript)
		if !ok {
			return "", false
		}
		transcript = append(transcript, assistant)
		calls := r.localCalls(assistant)
		if final, ended := ending(len(calls), stop, iteration, r.request.MaxIterations); ended {
			return final, true
		}
		results, ok := r.execute(ctx, calls)
		if !ok || !r.commit(results) {
			return "", false
		}
		transcript = append(transcript, results)
		if r.sink.hasYielded() {
			return StopYield, true
		}
	}
}

// ending decides whether the run stops after a turn, before its calls run.
//
// A turn that ended for any reason but tools ends the run on that reason,
// even if it asked for tools: a refusal or a truncated answer is not a
// request to act on. A tool turn on the last permitted iteration ends on
// StopIterations with its calls unanswered.
func ending(calls int, stop Stop, iteration, limit int) (Stop, bool) {
	switch {
	case calls == 0 && stop == StopTools:
		return StopOther, true
	case calls == 0, stop != StopTools:
		return stop, true
	case limit > 0 && iteration >= limit:
		return StopIterations, true
	}
	return "", false
}

// turn streams one provider turn to the consumer and returns the assistant
// message it committed, with its Finish recorded.
func (r *run) turn(ctx context.Context, transcript []Message) (Message, Stop, bool) {
	request := r.request
	request.Messages = transcript
	r.indices = map[string]int{}
	var message *Message
	stop := StopOther
	for event, err := range r.provider.Turn(ctx, request) {
		if err != nil {
			r.yield(Event{}, err)
			return Message{}, "", false
		}
		switch event.Kind {
		case KindMessage:
			message = event.Message
			continue
		case KindTurn:
			r.total = r.total.Add(event.Usage)
			stop = event.Stop
		case KindToolCall:
			r.indices[event.Tool.ID] = event.Tool.Index
		}
		if !r.yield(event, nil) {
			return Message{}, "", false
		}
	}
	if message == nil {
		r.yield(Event{}, fmt.Errorf("nacelle: %s ended a turn without its message", r.provider.Name()))
		return Message{}, "", false
	}
	committed := Message{Role: RoleAssistant, Parts: append(slices.Clone(message.Parts), Finish{Stop: stop})}
	return committed, stop, r.commit(committed)
}

// commit hands the consumer a message that is now part of the transcript.
func (r *run) commit(message Message) bool {
	return r.yield(Event{Kind: KindMessage, Message: &message}, nil)
}

// localCalls are the calls in an assistant message this run has to answer.
func (r *run) localCalls(message Message) []Call {
	var calls []Call
	for _, part := range message.Parts {
		call, ok := part.(ToolCall)
		if !ok || !call.Finished {
			continue
		}
		index, seen := r.indices[call.ID]
		if !seen {
			index = len(calls)
		}
		calls = append(calls, Call{ID: call.ID, Name: call.Name, Index: index, Input: call.Input})
	}
	return calls
}

// execute runs a round of calls, releasing their events as they land, and
// returns the user message carrying every result in the model's order.
func (r *run) execute(ctx context.Context, calls []Call) (Message, bool) {
	results := make([]Part, len(calls))
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		readers, writers := plan(calls, r.sink.tools)
		var group sync.WaitGroup
		for _, position := range readers {
			group.Go(func() { results[position] = r.sink.call(ctx, calls[position]) })
		}
		group.Wait()
		for _, position := range writers {
			results[position] = r.sink.call(ctx, calls[position])
		}
	}()
	for {
		select {
		case <-finished:
			return Message{Role: RoleUser, Parts: results}, r.flush()
		case <-r.sink.wake:
			if !r.flush() {
				return Message{}, false
			}
		}
	}
}

// flush hands the consumer every tool event parked since the last flush.
func (r *run) flush() bool {
	for _, event := range r.sink.drain() {
		if !r.yield(event, nil) {
			return false
		}
	}
	return true
}
