package nacelle_test

import (
	"encoding/json"
	"testing"

	"github.com/FacileStudio/nacelle"
)

func conversationOf(n int) []nacelle.Message {
	messages := make([]nacelle.Message, n)
	for i := range messages {
		messages[i] = nacelle.UserText("message")
	}
	return messages
}

// The ordinary cases: keeping nothing, keeping everything, and keeping a
// plain middle amount where no call/result pairing is in play at all.
func TestTrimOrdinaryCases(t *testing.T) {
	cases := []struct {
		name        string
		length      int
		keep        int
		wantKept    int
		wantDropped int
	}{
		{"empty input", 0, 5, 0, 0},
		{"keep nothing", 5, 0, 0, 5},
		{"keep a negative amount", 5, -3, 0, 5},
		{"keep more than there is", 3, 10, 3, 0},
		{"keep exactly what there is", 4, 4, 4, 0},
		{"keep the tail", 10, 3, 3, 7},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kept, dropped := nacelle.Trim(conversationOf(c.length), c.keep)
			if len(kept) != c.wantKept {
				t.Errorf("len(kept) = %d, want %d", len(kept), c.wantKept)
			}
			if dropped != c.wantDropped {
				t.Errorf("dropped = %d, want %d", dropped, c.wantDropped)
			}
		})
	}
}

// Keeping the tail must keep the actual most-recent messages, not just the
// right count of them.
func TestTrimKeepsTheMostRecentMessages(t *testing.T) {
	conversation := []nacelle.Message{
		nacelle.UserText("first"),
		nacelle.UserText("second"),
		nacelle.UserText("third"),
	}

	kept, dropped := nacelle.Trim(conversation, 2)

	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	if len(kept) != 2 || said(kept[0]) != "second" || said(kept[1]) != "third" {
		t.Fatalf("kept = %v, want [second third]", texts(kept))
	}
}

// The invariant that makes naive truncation unsafe: a cut landing on a
// message that opens with a ToolResult would keep an answer with no
// question, since the ToolCall it answers lives in the message the naive cut
// just dropped. The boundary has to advance past the whole pair instead —
// kept never exceeds keep, so it drops both halves rather than keeping the
// result and reaching back for its call.
//
// Asking to keep the last two of the four messages below would naively cut
// right on the ToolResult message — the case this test exists to catch. The
// pair before it gets dropped whole instead, so only the final message
// survives.
func TestTrimNeverStartsOnAToolResult(t *testing.T) {
	conversation := []nacelle.Message{
		nacelle.UserText("what is in go.mod?"),
		{
			Role: nacelle.RoleAssistant,
			Parts: []nacelle.Part{
				nacelle.Text{Text: "let me look"},
				nacelle.ToolCall{ID: "call_1", Name: "read_file", Input: json.RawMessage(`{"path":"go.mod"}`), Finished: true},
			},
		},
		{
			Role:  nacelle.RoleUser,
			Parts: []nacelle.Part{nacelle.ToolResult{ID: "call_1", Name: "read_file", Result: "module nacelle"}},
		},
		nacelle.AssistantText("it is the nacelle module"),
	}

	kept, dropped := nacelle.Trim(conversation, 2)

	if len(kept) != 1 {
		t.Fatalf("kept = %v, want exactly the final message once the pair was dropped whole", texts(kept))
	}
	if opensWithToolResult(kept[0]) {
		t.Fatalf("kept[0] opens with a ToolResult: %+v", kept[0])
	}
	if said(kept[0]) != "it is the nacelle module" {
		t.Errorf("kept[0] = %+v, want the message after the pair", kept[0])
	}
	if dropped != 3 {
		t.Errorf("dropped = %d, want 3 (the question and the whole call/result pair)", dropped)
	}
}

// The degenerate case: every remaining message is a tool result with no call
// in view anywhere in what's left. This should not arise from a conversation
// this package itself ever produces, only a hand-built one — and the correct
// answer is to keep nothing rather than emit something a provider refuses.
func TestTrimKeepsNothingRatherThanStartOnAnOrphanedResult(t *testing.T) {
	conversation := []nacelle.Message{
		nacelle.UserText("earlier"),
		{
			Role:  nacelle.RoleUser,
			Parts: []nacelle.Part{nacelle.ToolResult{ID: "orphan", Name: "read_file", Result: "..."}},
		},
	}

	kept, dropped := nacelle.Trim(conversation, 1)

	if len(kept) != 0 {
		t.Errorf("kept = %v, want nothing rather than a conversation starting on an orphaned result", kept)
	}
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2 (everything)", dropped)
	}
}

func opensWithToolResult(message nacelle.Message) bool {
	if len(message.Parts) == 0 {
		return false
	}
	_, ok := message.Parts[0].(nacelle.ToolResult)
	return ok
}

func said(message nacelle.Message) string {
	if len(message.Parts) == 0 {
		return ""
	}
	text, ok := message.Parts[0].(nacelle.Text)
	if !ok {
		return ""
	}
	return text.Text
}

func texts(messages []nacelle.Message) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = said(m)
	}
	return out
}
