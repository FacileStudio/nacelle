package anthropic

import (
	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// callTracker assembles tool calls from the stream.
//
// A tool call arrives in pieces: the name and id on content_block_start, then
// the arguments as JSON fragments, then a stop. Reporting it at the start
// would mean reporting a call whose input is still empty, so it is held until
// the block closes and emitted whole.
//
// A tracker covers one turn, which is what makes its counter the position of
// a call within the turn that asked for it. It sorts the turn's calls by who
// will run them: a local one is queued for the registry, because the runner
// executes a turn's tools at the start of the next one, while an MCP one is
// held here until its result block arrives on this same turn.
type callTracker struct {
	open     map[int64]*openCall
	remote   map[string]*nacelle.ToolEvent
	queued   []*nacelle.ToolEvent
	pending  *invocations
	thinking bool
	next     int
}

// openCall is a content block still streaming, and which side of the request
// is going to execute it.
type openCall struct {
	event  *nacelle.ToolEvent
	remote bool
}

func newCallTracker(pending *invocations, thinking bool) *callTracker {
	return &callTracker{
		open:     map[int64]*openCall{},
		remote:   map[string]*nacelle.ToolEvent{},
		pending:  pending,
		thinking: thinking,
	}
}

// consume maps one raw stream event onto zero or more nacelle events.
func (c *callTracker) consume(event sdk.BetaRawMessageStreamEventUnion) []nacelle.Event {
	switch event.Type {
	case "content_block_start":
		return c.start(event)
	case "content_block_delta":
		return c.delta(event)
	case "content_block_stop":
		return c.stop(event)
	case "message_stop":
		return c.finish()
	}
	return nil
}

// start records a tool call whose arguments have not arrived yet, or reports
// an MCP tool's result, which arrives whole rather than in fragments.
//
// The position is taken here rather than at the stop because content blocks
// open in the order the model wrote them, which is the order Index promises,
// and it counts calls rather than blocks so that prose or reasoning ahead of
// the first call does not shift it.
//
// A fallback block discards the turn's queue because the runner discards the
// same calls: they belong to the attempt that refused, the fallback middleware
// strips them from the replayed history, and an entry for a call nobody will
// ever execute is the stale key that mispairs a later result. Discarded is not
// forgotten — see discard, which closes them.
func (c *callTracker) start(event sdk.BetaRawMessageStreamEventUnion) []nacelle.Event {
	block := event.ContentBlock
	switch {
	case block.Type == "fallback":
		return c.discard()
	case block.Type == "mcp_tool_result":
		return c.remoteResult(block)
	case isToolUse(block.Type):
		c.open[event.Index] = &openCall{
			event:  &nacelle.ToolEvent{ID: block.ID, Index: c.next, Name: block.Name},
			remote: block.Type == "mcp_tool_use",
		}
		c.next++
	}
	return nil
}

// delta turns a content fragment into an event, or files it against the tool
// call it belongs to. The index is what keeps two tools requested in the same
// turn from mixing their arguments.
//
// Reasoning is dropped unless the run asked for it. nacelle.Config documents
// Thinking as streaming the model's reasoning as KindThinking events and
// leaves it off, so forwarding it to a consumer that never opted in is not
// generosity — it is a backend inventing a promise, and the OpenRouter one
// gates the same deltas the same way.
func (c *callTracker) delta(event sdk.BetaRawMessageStreamEventUnion) []nacelle.Event {
	switch event.Delta.Type {
	case "text_delta":
		return []nacelle.Event{{Kind: nacelle.KindText, Text: event.Delta.Text}}
	case "thinking_delta":
		if !c.thinking {
			return nil
		}
		return []nacelle.Event{{Kind: nacelle.KindThinking, Text: event.Delta.Thinking}}
	case "input_json_delta":
		if call, found := c.open[event.Index]; found {
			call.event.Input += event.Delta.PartialJSON
		}
	}
	return nil
}

// stop closes a tool call, files it against whoever will answer it, and
// reports it whole.
//
// Filing happens at the close because that is the first moment the arguments
// are complete, and the arguments are half of what identifies the call to the
// handler that will run it. Only a local call reaches the registry: an MCP
// tool runs on Anthropic's side, so no execution will ever come looking for
// it, and it waits here for the result block instead.
func (c *callTracker) stop(event sdk.BetaRawMessageStreamEventUnion) []nacelle.Event {
	open, found := c.open[event.Index]
	if !found {
		return nil
	}
	delete(c.open, event.Index)
	if open.remote {
		c.remote[open.event.ID] = open.event
	} else {
		c.queued = append(c.queued, open.event)
	}
	return []nacelle.Event{{Kind: nacelle.KindToolCall, Tool: open.event}}
}

// finish hands the turn's local calls to the registry and closes any MCP call
// whose result never arrived.
//
// The handoff is at the end of the turn rather than at each call because that
// is the moment the batch is final — a fallback block can still have discarded
// it — and because replacing the registry whole is what stops a turn's entries
// outliving the turn.
func (c *callTracker) finish() []nacelle.Event {
	c.pending.reset(c.queued)
	return c.orphans()
}

// isToolUse reports whether a content block is the model calling a tool.
//
// Both spellings count. A tool this process runs arrives as tool_use and one
// reached over MCP as mcp_tool_use, and a consumer has no reason to care which
// side of the request executed it — which is exactly why both must also end up
// closed by a KindToolResult, whoever ran them.
func isToolUse(blockType string) bool {
	return blockType == "tool_use" || blockType == "mcp_tool_use"
}
