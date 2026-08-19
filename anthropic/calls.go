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
type callTracker struct {
	open map[int64]*nacelle.ToolEvent
}

func newCallTracker() *callTracker {
	return &callTracker{open: map[int64]*nacelle.ToolEvent{}}
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
func (c *callTracker) start(event sdk.BetaRawMessageStreamEventUnion) {
	if !isToolUse(event.ContentBlock.Type) {
		return
	}
	c.open[event.Index] = &nacelle.ToolEvent{
		ID:   event.ContentBlock.ID,
		Name: event.ContentBlock.Name,
	}
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

// stop closes a tool call and reports it whole.
func (c *callTracker) stop(event sdk.BetaRawMessageStreamEventUnion) []nacelle.Event {
	call, ok := c.open[event.Index]
	if !ok {
		return nil
	}
	delete(c.open, event.Index)
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
