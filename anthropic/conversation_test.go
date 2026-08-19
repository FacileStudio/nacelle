package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
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

	encoded, err := json.Marshal(toParams(conversation))
	if err != nil {
		t.Fatalf("marshalling the conversation: %v", err)
	}
	return string(encoded)
}

// The exit criterion for widening Message: a conversation holding a tool call
// has to reach the API as one, or a resumed run asks the model to carry on from
// a transcript with its own tool history cut out.
func TestAToolRoundReachesTheAPI(t *testing.T) {
	sent := wire(t, round())

	for _, want := range []string{
		`"type":"tool_use"`,
		`"id":"call_1"`,
		`"input":{"path":"go.mod"}`,
		`"name":"read_file"`,
		`"type":"tool_result"`,
		`"tool_use_id":"call_1"`,
		"module nacelle",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("request does not carry %s:\n%s", want, sent)
		}
	}
}

// Reasoning would need the signature the stream never carries, a half-streamed
// call carries arguments that do not parse, and Finish has no field in the wire
// format at all. Sending any of the three is a rejected request.
func TestThePartsTheAPICannotTakeAreLeftOut(t *testing.T) {
	sent := wire(t, round())

	for _, unwanted := range []string{"the file sits at the root", "call_2", "write_file", "half a"} {
		if strings.Contains(sent, unwanted) {
			t.Errorf("request carries %q, which the API refuses:\n%s", unwanted, sent)
		}
	}
}

// A message whose every part drops out has no content, and the API refuses an
// empty message rather than ignoring it.
func TestAMessageWithNothingLeftIsNotSent(t *testing.T) {
	params := toParams([]nacelle.Message{
		nacelle.UserText("still here"),
		{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{
			nacelle.Reasoning{Text: "thinking"},
			nacelle.Finish{Stop: nacelle.StopEnd},
		}},
	})

	if len(params) != 1 {
		t.Fatalf("converted %d messages, want only the one with content", len(params))
	}
	if params[0].Role != sdk.BetaMessageParamRoleUser {
		t.Errorf("role = %q, want the user message that survived", params[0].Role)
	}
}

// input is a required field, and a tool that takes no arguments is still a tool
// the model called.
func TestACallWithNoArgumentsIsSentAsAnEmptyObject(t *testing.T) {
	sent := wire(t, []nacelle.Message{{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{
		nacelle.ToolCall{ID: "call_1", Name: "list_files", Finished: true},
	}}})

	if !strings.Contains(sent, `"input":{}`) {
		t.Errorf("request = %s, want an empty object for the arguments", sent)
	}
}

// A tool that failed is reported as a failure, because the model is the one
// deciding whether the task can still be finished.
func TestAFailedToolIsMarkedAsOne(t *testing.T) {
	sent := wire(t, []nacelle.Message{{Role: nacelle.RoleUser, Parts: []nacelle.Part{
		nacelle.ToolResult{ID: "call_1", Name: "read_file", Result: "no such file", Failed: true},
	}}})

	if !strings.Contains(sent, `"is_error":true`) {
		t.Errorf("request = %s, want the failure marked", sent)
	}
}
