package nacelle

import "context"

// CountTokens reports how many tokens this conversation would use if sent as
// the next turn, without sending it.
//
// It counts the same request Stream would: the system prompt, the tools, the
// MCP servers, and the conversation together — not the bare messages alone.
// All of those are billed, and a count of the messages only would be an
// answer to a narrower question than the one a caller asking "will this fit"
// actually has.
func (a *Agent) CountTokens(ctx context.Context, conversation []Message) (int64, error) {
	request := a.request
	request.Messages = conversation
	return a.backend.CountTokens(ctx, request)
}
