package anthropic

import (
	"fmt"
	"maps"
	"slices"

	"github.com/FacileStudio/nacelle"
)

// closed is the result that ends a call nothing is ever going to answer.
//
// It exists because the contract a consumer builds on is that a KindToolCall
// is followed by a KindToolResult carrying the same id — a spinner started per
// call and closed per result hangs forever otherwise — and that contract does
// not say the answer has to be good. So the call is closed with everything
// known about it and an error saying why there is no result, which is strictly
// more than the consumer had before.
//
// discarded marks the closes on the discard path, and only that path: a call
// that never ran at all is not history a consumer should replay, where an
// orphaned one really was issued and only its answer went missing.
func closed(call *nacelle.ToolEvent, err error, discarded bool) nacelle.Event {
	return nacelle.Event{Kind: nacelle.KindToolResult, Tool: &nacelle.ToolEvent{
		ID: call.ID, Index: call.Index, Name: call.Name, Input: call.Input,
		Result: err.Error(), Err: err, Discarded: discarded,
	}}
}

// discard closes the calls a fallback block invalidated.
//
// The runner skips every tool_use block before the last fallback block: they
// belong to the attempt that refused, and the fallback middleware strips them
// from the replayed history, so answering them would orphan the result. They
// were still streamed and still reported, which is why they are closed here
// rather than quietly forgotten — and closed as Discarded, so a consumer
// rebuilding a conversation from the stream drops them the same way the
// runner already does, instead of replaying a call and an error that never
// really happened.
func (c *callTracker) discard() []nacelle.Event {
	events := make([]nacelle.Event, 0, len(c.queued))
	for _, call := range c.queued {
		events = append(events, closed(call, fmt.Errorf(
			"nacelle/anthropic: %q was asked for by an attempt that was refused and retried", call.Name), true))
	}
	c.queued = nil
	return events
}

// orphans closes every MCP call the turn ended without answering.
//
// A result block that never arrives is not hypothetical: a server can fail in
// a way the API reports as nothing at all. Sorted by index so the batch reads
// in the order the model asked, which is the order everything else is sorted
// in. Unlike discard, these calls really were issued — only the answer is
// missing — so they are closed but not Discarded, and stay in a replayed
// conversation as a call the model made that came back with an error.
func (c *callTracker) orphans() []nacelle.Event {
	waiting := slices.SortedFunc(maps.Values(c.remote), func(a, b *nacelle.ToolEvent) int {
		return a.Index - b.Index
	})
	clear(c.remote)

	events := make([]nacelle.Event, 0, len(waiting))
	for _, call := range waiting {
		events = append(events, closed(call, fmt.Errorf(
			"nacelle/anthropic: the MCP server returned no result for %q", call.Name), false))
	}
	return events
}
