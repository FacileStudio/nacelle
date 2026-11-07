package nacelle_test

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// screenshot is a tool that answers with text and a picture.
func screenshot(t *testing.T) nacelle.Tool {
	t.Helper()
	tool, err := nacelle.NewImageTool("screenshot", "Take a screenshot",
		func(context.Context, struct{}) (string, []nacelle.Image, error) {
			return "the page", []nacelle.Image{{MediaType: "image/png", Data: []byte{1, 2, 3}}}, nil
		}, nacelle.ToolOptions{})
	if err != nil {
		t.Fatalf("NewImageTool: %v", err)
	}
	return tool
}

// The transcript a run returns is the conversation the model saw, with the
// pictures a tool returned and each turn's Finish, ending on the run's done.
func TestTranscriptKeepsImagesAndFinishes(t *testing.T) {
	tool := screenshot(t)
	backend := nacelle.NewBackend(&script{steps: []step{toolStep("screenshot", `{}`), textStep("seen it")}})
	messages, done, err := nacelle.Transcript(backend.Stream(context.Background(), nacelle.Request{Tools: []nacelle.Tool{tool}}))
	if err != nil {
		t.Fatalf("Transcript: %v", err)
	}
	if done.Kind != nacelle.KindDone || done.Stop != nacelle.StopEnd {
		t.Fatalf("done = %+v, want the run's KindDone", done)
	}
	if len(messages) != 3 {
		t.Fatalf("committed %d messages, want call, result and answer", len(messages))
	}
	result, ok := messages[1].Parts[0].(nacelle.ToolResult)
	if !ok || len(result.Images) != 1 || result.Images[0].MediaType != "image/png" {
		t.Fatalf("the result turn is %#v, want the screenshot kept", messages[1].Parts)
	}
	for _, at := range []int{0, 2} {
		parts := messages[at].Parts
		if _, ok := parts[len(parts)-1].(nacelle.Finish); !ok {
			t.Errorf("assistant turn %d ends with %#v, want its Finish", at, parts[len(parts)-1])
		}
	}
}

// The consumer sees the picture on the result event too, not only in the
// transcript.
func TestAToolResultEventCarriesItsImages(t *testing.T) {
	got := runTool(t, context.Background(), nacelle.Request{}, screenshot(t), `{}`)
	if len(got.tool.Images) != 1 {
		t.Fatalf("the result event carried %d images, want the screenshot", len(got.tool.Images))
	}
}

// A run that fails keeps the turns it committed before the failure.
func TestTranscriptKeepsWhatCameBeforeAFailure(t *testing.T) {
	broken := errors.New("connection reset")
	answer := nacelle.AssistantText("partial")
	seq := iter.Seq2[nacelle.Event, error](func(yield func(nacelle.Event, error) bool) {
		if yield(nacelle.Event{Kind: nacelle.KindMessage, Message: &answer}, nil) {
			yield(nacelle.Event{}, broken)
		}
	})
	messages, _, err := nacelle.Transcript(seq)
	if !errors.Is(err, broken) || len(messages) != 1 {
		t.Fatalf("Transcript = %d messages, %v; want the one committed and the failure", len(messages), err)
	}
}
