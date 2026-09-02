package nacelle

// Trim drops the oldest messages from a conversation, keeping at most keep of
// the most recent ones, and reports how many were dropped.
//
// It never returns a slice whose first message carries a ToolResult part.
// Cutting there would keep an answer with no question: the ToolCall it
// answers lives in the message before it, which the cut just dropped, and a
// tool_result naming a call nothing sent is a request every provider this
// package talks to rejects. When the requested boundary lands inside a
// call/result pair, the cut advances past the whole pair rather than
// retreating to keep it: kept never exceeds keep, which is the one promise
// worth keeping for a caller trimming to fit a budget — dropping a little
// more than asked is a smaller surprise than trimming to N and getting more
// than N back.
//
// This is truncation, not summarization. What survives is dropped whole, not
// compressed — deciding what to preserve and how is a product opinion, and
// this package does not have one; see nacelle.go's own doc comment on why. A
// caller wanting a summary in place of what was dropped builds it from the
// dropped count and its own model call, using this as the mechanical half.
//
// The returned slice shares the underlying array with the input. A caller
// that keeps a reference to the original conversation must copy the result
// before mutating it; a caller that throws the original away does not.
func Trim(conversation []Message, keep int) (kept []Message, dropped int) {
	if keep <= 0 {
		return nil, len(conversation)
	}
	if keep >= len(conversation) {
		return conversation, 0
	}

	boundary := clearOfToolResults(conversation, len(conversation)-keep)
	return conversation[boundary:], boundary
}

// clearOfToolResults walks a proposed cut forward until it no longer lands on
// a message that opens with a ToolResult, or there is nothing left to walk
// into.
//
// The degenerate case — every remaining message is a tool result with no
// call in view — keeps nothing rather than emit a conversation a provider
// would refuse. It should not be reachable from a conversation this package
// itself ever produced: tui/conversation.go never leaves the newest end of a
// stored conversation on a dangling call, so the messages ahead of any
// ToolResult always include the ToolCall it answers. It is reachable from a
// hand-built one, and keeping nothing is still the correct answer for it.
func clearOfToolResults(conversation []Message, boundary int) int {
	for boundary < len(conversation) && opensWithToolResult(conversation[boundary]) {
		boundary++
	}
	return boundary
}

// opensWithToolResult reports whether a message's first part is a tool
// result — the one shape Trim must never leave at the start of what it kept.
func opensWithToolResult(message Message) bool {
	if len(message.Parts) == 0 {
		return false
	}
	_, ok := message.Parts[0].(ToolResult)
	return ok
}

// TrimCopy drops the oldest messages from a conversation like Trim, but
// returns a new slice with its own backing array. Use this when the
// original conversation must remain usable after the call — Trim shares
// the underlying array, so mutating the result also mutates the original.
func TrimCopy(conversation []Message, keep int) (kept []Message, dropped int) {
	kept, dropped = Trim(conversation, keep)
	if len(kept) > 0 {
		copied := make([]Message, len(kept))
		copy(copied, kept)
		return copied, dropped
	}
	return nil, dropped
}
