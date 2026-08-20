package openrouter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// round is the conversation a run that used a tool leaves behind, with the two
// parts no backend can send mixed in among the ones every backend must.
func round() []nacelle.Message {
	return []nacelle.Message{
		nacelle.UserText("what is in go.mod?"),
		{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{
			nacelle.Text{Text: "let me look"},
			nacelle.Reasoning{Text: "the file sits at the root"},
			nacelle.ToolCall{ID: "call_1", Name: "read_file", Input: json.RawMessage(`{"path":"go.mod"}`), Finished: true},
			nacelle.ToolCall{ID: "call_2", Name: "write_file", Input: json.RawMessage(`{"contents":"half a`)},
			nacelle.Finish{Stop: nacelle.StopTools},
		}},
		{Role: nacelle.RoleUser, Parts: []nacelle.Part{
			nacelle.ToolResult{ID: "call_1", Name: "read_file", Result: "module nacelle"},
		}},
	}
}

// wire is what the SDK will actually put on the request, which is the only
// level at which "the model sees it" means anything.
func wire(t *testing.T, conversation []nacelle.Message) string {
	t.Helper()

	backend, err := New(Config{Model: "test/model", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	encoded, err := json.Marshal(backend.messages(nacelle.Request{System: "be brief", Messages: conversation}))
	if err != nil {
		t.Fatalf("marshalling the conversation: %v", err)
	}
	return string(encoded)
}

// The exit criterion for widening Message, on the backend that has to split the
// shape rather than mirror it: a tool result is a message of its own here.
func TestAToolRoundReachesTheAPI(t *testing.T) {
	sent := wire(t, round())

	for _, want := range []string{
		`"role":"assistant"`,
		`"tool_calls"`,
		`"id":"call_1"`,
		`"name":"read_file"`,
		`"arguments":"{\"path\":\"go.mod\"}"`,
		`"role":"tool"`,
		`"tool_call_id":"call_1"`,
		"module nacelle",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("request does not carry %s:\n%s", want, sent)
		}
	}
}

// The schema wants a tool message to follow the assistant message that asked
// for it. Prose from the same turn wedged in between breaks the pairing.
func TestAToolResultFollowsTheTurnThatAskedForIt(t *testing.T) {
	sent := wire(t, []nacelle.Message{
		{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{
			nacelle.ToolCall{ID: "call_1", Name: "read_file", Input: json.RawMessage(`{}`), Finished: true},
		}},
		{Role: nacelle.RoleUser, Parts: []nacelle.Part{
			nacelle.ToolResult{ID: "call_1", Name: "read_file", Result: "module nacelle"},
			nacelle.Text{Text: "and now summarise it"},
		}},
	})

	answer, prose := strings.Index(sent, `"role":"tool"`), strings.Index(sent, "and now summarise it")
	if answer < 0 || prose < 0 {
		t.Fatalf("request = %s, want both the result and the question on it", sent)
	}
	if prose < answer {
		t.Errorf("request = %s, want the tool result ahead of the next question", sent)
	}
}

// Reasoning is excluded by the request in the first place, a half-streamed call
// carries arguments that do not parse, and Finish has no field in this schema
// at all.
func TestThePartsTheAPICannotTakeAreLeftOut(t *testing.T) {
	sent := wire(t, round())

	for _, unwanted := range []string{"the file sits at the root", "call_2", "write_file", "half a"} {
		if strings.Contains(sent, unwanted) {
			t.Errorf("request carries %q, which the API refuses:\n%s", unwanted, sent)
		}
	}
}

// The schema requires an assistant message to carry content or tool calls, so a
// turn that recorded neither has no message to become.
func TestAMessageWithNothingLeftIsNotSent(t *testing.T) {
	sent := wire(t, []nacelle.Message{
		nacelle.UserText("still here"),
		{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{
			nacelle.Reasoning{Text: "thinking"},
			nacelle.Finish{Stop: nacelle.StopEnd},
		}},
	})

	if strings.Contains(sent, `"role":"assistant"`) {
		t.Errorf("request = %s, want no empty assistant message", sent)
	}
}

// arguments is a required field, and a provider handed an empty string has
// nothing to parse.
func TestACallWithNoArgumentsIsSentAsAnEmptyObject(t *testing.T) {
	sent := wire(t, []nacelle.Message{{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{
		nacelle.ToolCall{ID: "call_1", Name: "list_directory", Finished: true},
	}}})

	if !strings.Contains(sent, `"arguments":"{}"`) {
		t.Errorf("request = %s, want an empty object for the arguments", sent)
	}
}
