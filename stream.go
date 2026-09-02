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
//
// The conversation is passed by reference and may be compacted by the backend
// if it exceeds the budget. The caller should use the returned conversation
// from any compaction hook for subsequent turns.
func (a *Agent) Stream(ctx context.Context, conversation []Message) iter.Seq2[Event, error] {
	if err := validateRoles(conversation); err != nil {
		return func(yield func(Event, error) bool) { yield(Event{}, err) }
	}

	request := a.request
	request.Messages = conversation
	return a.backend.Stream(ctx, request)
}

// CompactConversation trims a conversation to fit within a token budget,
// firing BeforeCompact and AfterCompact hooks if registered in the agent's
// hook configuration. It returns the compacted conversation and the number
// of tokens it would use.
//
// This is a convenience for consumers that want to run compaction explicitly
// (e.g., pre-flight in a UI) without delegating to the backend. The hooks
// receive the pre- and post-compaction token counts via HookEvent.Input and
// HookEvent.Result as stringified numbers.
func (a *Agent) CompactConversation(ctx context.Context, conversation []Message, maxTokens int64) ([]Message, int64, error) {
	// Fire BeforeCompact hook
	if hooks := a.request.Hooks[BeforeCompact]; len(hooks) > 0 {
		ev := HookEvent{
			Point: BeforeCompact,
			Input: fmt.Sprintf("%d", maxTokens),
		}
		for _, hook := range hooks {
			hook(ctx, ev)
		}
	}

	// Binary search for the largest prefix that fits
	lo, hi := 0, len(conversation)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		kept, _ := Trim(conversation, mid)
		count, err := a.CountTokens(ctx, kept)
		if err != nil {
			return nil, 0, err
		}
		if count <= maxTokens {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	kept, _ := Trim(conversation, lo)
	count, err := a.CountTokens(ctx, kept)
	if err != nil {
		return nil, 0, err
	}

	// Fire AfterCompact hook
	if hooks := a.request.Hooks[AfterCompact]; len(hooks) > 0 {
		ev := HookEvent{
			Point:  AfterCompact,
			Input:  fmt.Sprintf("%d", maxTokens),
			Result: fmt.Sprintf("%d", count),
		}
		for _, hook := range hooks {
			hook(ctx, ev)
		}
	}
	return kept, count, nil
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
