package nacelle_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// everyPart is a conversation holding one of each part type.
func everyPart() []nacelle.Message {
	picture := nacelle.Image{MediaType: "image/png", Data: []byte{0, 1, 2}}
	return []nacelle.Message{
		{Role: nacelle.RoleUser, Parts: []nacelle.Part{
			nacelle.Text{Text: "look"},
			picture,
			nacelle.Document{Name: "spec.pdf", MediaType: "application/pdf", Data: []byte("%PDF")},
		}},
		{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{
			nacelle.Reasoning{Text: "thinking"},
			nacelle.Native{Provider: "anthropic", Data: json.RawMessage(`{"type":"thinking","signature":"s"}`)},
			nacelle.ToolCall{ID: "c1", Name: "screenshot", Input: json.RawMessage(`{"url":"x"}`), Finished: true},
			nacelle.Finish{Stop: nacelle.StopTools},
		}},
		{Role: nacelle.RoleUser, Parts: []nacelle.Part{
			nacelle.ToolResult{ID: "c1", Name: "screenshot", Result: "the page", Failed: true, Images: []nacelle.Image{picture}},
		}},
	}
}

// Every part survives the round trip unchanged.
func TestTheCodecRoundTripsEveryPart(t *testing.T) {
	encoded, err := nacelle.MarshalMessages(everyPart())
	if err != nil {
		t.Fatalf("MarshalMessages: %v", err)
	}
	decoded, err := nacelle.UnmarshalMessages(encoded)
	if err != nil {
		t.Fatalf("UnmarshalMessages: %v", err)
	}
	if !reflect.DeepEqual(decoded, everyPart()) {
		t.Fatalf("decoded\n%#v\nwant\n%#v", decoded, everyPart())
	}
}

// A transcript stored in the shape consumers already wrote decodes as it is.
func TestTheCodecReadsTheStoredShape(t *testing.T) {
	stored := `[{"role":"assistant","parts":[{"type":"text","text":"hi"},` +
		`{"type":"tool_call","id":"c","name":"grep","input":{"pattern":"x"},"finished":true},` +
		`{"type":"finish","stop":"tools"}]}]`
	decoded, err := nacelle.UnmarshalMessages([]byte(stored))
	if err != nil {
		t.Fatalf("UnmarshalMessages: %v", err)
	}
	call, ok := decoded[0].Parts[1].(nacelle.ToolCall)
	if !ok || call.Name != "grep" || string(call.Input) != `{"pattern":"x"}` || !call.Finished {
		t.Fatalf("decoded %#v, want the stored call", decoded[0].Parts)
	}
}

// A part type nobody knows is an error, not a quietly shorter transcript.
func TestTheCodecRefusesAnUnknownPart(t *testing.T) {
	if _, err := nacelle.UnmarshalMessages([]byte(`[{"role":"user","parts":[{"type":"hologram"}]}]`)); err == nil {
		t.Fatal("an unknown part type decoded without an error")
	}
}

// Half a call's arguments is not JSON; the message still encodes.
func TestAnUnfinishedCallStillEncodes(t *testing.T) {
	message := nacelle.Message{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{
		nacelle.ToolCall{ID: "c", Name: "grep", Input: json.RawMessage(`{"pattern":`)},
	}}
	if _, err := nacelle.MarshalMessages([]nacelle.Message{message}); err != nil {
		t.Fatalf("MarshalMessages: %v", err)
	}
}
