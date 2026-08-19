package nacelle

import (
	"context"
	"fmt"
	"iter"
)

// Stream runs the conversation and yields what happens as it happens.
//
// The sequence ends after a KindDone event, or early with a non-nil error. A
// consumer that stops ranging cancels the run: the underlying request is torn
// down with the context, so abandoning the loop is a supported way to stop an
// agent rather than a leak.
//
// Tool failures are not stream errors. A tool that returns an error is
// reported as a KindToolResult carrying it and handed back to the model, which
// is better placed than the caller to decide whether the task can still be
// finished. An error out of this sequence means the run itself failed.
func (a *Agent) Stream(ctx context.Context, conversation []Message) iter.Seq2[Event, error] {
	if err := validateRoles(conversation); err != nil {
		return func(yield func(Event, error) bool) { yield(Event{}, err) }
	}

	request := a.request
	request.Messages = conversation
	return a.backend.Stream(ctx, request)
}

// validateRoles refuses a conversation where a part sits on the wrong side of
// it, before either backend ever sees the request.
//
// A ToolCall is the model's own move and belongs only to RoleAssistant; a
// ToolResult answers one and belongs only to RoleUser. Left unchecked, the two
// backends disagree about what a mismatch does: the Anthropic converter sends
// it and lets the API reject the request, while the OpenRouter converter drops
// it in silence. Checking once, centrally, is what the roadmap's own warning
// against the Crush trap — maintaining the same union check in more than one
// place — argues for, and it keeps this package's "fails rather than
// degrading" promise for the one part of the union a caller can get wrong.
func validateRoles(messages []Message) error {
	for i, message := range messages {
		if err := partsMatchRole(message); err != nil {
			return fmt.Errorf("nacelle: message %d: %w", i, err)
		}
	}
	return nil
}

// partsMatchRole checks one message's parts against its own role.
func partsMatchRole(message Message) error {
	for _, part := range message.Parts {
		switch part.(type) {
		case ToolCall:
			if message.Role != RoleAssistant {
				return fmt.Errorf("a ToolCall belongs on an assistant message, not %s", message.Role)
			}
		case ToolResult:
			if message.Role != RoleUser {
				return fmt.Errorf("a ToolResult belongs on a user message, not %s", message.Role)
			}
		}
	}
	return nil
}
