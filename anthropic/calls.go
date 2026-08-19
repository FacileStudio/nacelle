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
// a call within the turn that asked for it. The registry it files finished
// calls into outlives it, because the runner executes a turn's tools at the
// start of the next one.
type callTracker struct {
	open    map[int64]*nacelle.ToolEvent
	pending *invocations
	next    int
}

func newCallTracker(pending *invocations) *callTracker {
	return &callTracker{open: map[int64]*nacelle.ToolEvent{}, pending: pending}
}

// consume maps one raw stream event onto zero or more nacelle events.
func (c *callTracker) consume(event sdk.BetaRawMessageStreamEventUnion) []nacelle.Event {
	switch event.Type {
	case "content_block_start":
		c.start(event)
	case "content_block_delta":
		return c.delta(event)
	case "content_block_stop":
		return c.stop(event)
	}
	return nil
}

// start records a tool call whose arguments have not arrived yet.
//
// The position is taken here rather than at the stop because content blocks
// open in the order the model wrote them, which is the order Index promises,
// and it counts calls rather than blocks so that prose or reasoning ahead of
// the first call does not shift it.
func (c *callTracker) start(event sdk.BetaRawMessageStreamEventUnion) {
	if !isToolUse(event.ContentBlock.Type) {
		return
	}
	c.open[event.Index] = &nacelle.ToolEvent{
		ID:    event.ContentBlock.ID,
		Index: c.next,
		Name:  event.ContentBlock.Name,
	}
	c.next++
}

// delta turns a content fragment into an event, or files it against the tool
// call it belongs to. The index is what keeps two tools requested in the same
// turn from mixing their arguments.
func (c *callTracker) delta(event sdk.BetaRawMessageStreamEventUnion) []nacelle.Event {
	switch event.Delta.Type {
	case "text_delta":
		return []nacelle.Event{{Kind: nacelle.KindText, Text: event.Delta.Text}}
	case "thinking_delta":
		return []nacelle.Event{{Kind: nacelle.KindThinking, Text: event.Delta.Thinking}}
	case "input_json_delta":
		if call, ok := c.open[event.Index]; ok {
			call.Input += event.Delta.PartialJSON
		}
	}
	return nil
}

// stop closes a tool call, files it so the execution can find its id, and
// reports it whole.
//
// Filing happens at the close because that is the first moment the arguments
// are complete, and the arguments are half of what identifies the call to the
// handler that will run it.
func (c *callTracker) stop(event sdk.BetaRawMessageStreamEventUnion) []nacelle.Event {
	call, ok := c.open[event.Index]
	if !ok {
		return nil
	}
	delete(c.open, event.Index)
	c.pending.record(call)
	return []nacelle.Event{{Kind: nacelle.KindToolCall, Tool: call}}
}

// isToolUse reports whether a content block is the model calling a tool.
//
// Both spellings count. A tool this process runs arrives as tool_use and one
// reached over MCP as mcp_tool_use, and a consumer has no reason to care which
// side of the request executed it.
func isToolUse(blockType string) bool {
	return blockType == "tool_use" || blockType == "mcp_tool_use"
}

// usageOf converts the API's per-turn accounting into ours.
func usageOf(usage sdk.BetaMessageDeltaUsage) nacelle.Usage {
	return nacelle.Usage{
		InputTokens:         usage.InputTokens,
		OutputTokens:        usage.OutputTokens,
		CacheReadTokens:     usage.CacheReadInputTokens,
		CacheCreationTokens: usage.CacheCreationInputTokens,
	}
}

// stopOf names why a turn ended.
//
// Mapping rather than passing the API's string through is what lets a
// consumer act on the answer being incomplete without learning the provider's
// vocabulary, and it is why the unrecognised case is StopOther rather than a
// new name invented per reason: a stop reason this package has never seen is
// still not an ending anyone should present as a finished answer.
//
// stop_sequence joins end_turn because the caller chose the marker the model
// stopped at, so the text before it is the answer that was asked for.
// compaction and pause_turn land in StopOther deliberately: neither says the
// answer is whole, and neither is something a caller can act on today.
func stopOf(reason sdk.BetaStopReason) nacelle.Stop {
	switch reason {
	case sdk.BetaStopReasonEndTurn, sdk.BetaStopReasonStopSequence:
		return nacelle.StopEnd
	case sdk.BetaStopReasonToolUse:
		return nacelle.StopTools
	case sdk.BetaStopReasonMaxTokens:
		return nacelle.StopMaxTokens
	case sdk.BetaStopReasonModelContextWindowExceeded:
		return nacelle.StopContext
	case sdk.BetaStopReasonRefusal:
		return nacelle.StopRefusal
	default:
		return nacelle.StopOther
	}
}
