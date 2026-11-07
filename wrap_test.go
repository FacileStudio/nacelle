package nacelle_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// lookTool is a read-only tool that answers with a picture.
func lookTool(t *testing.T) nacelle.Tool {
	t.Helper()
	tool, err := nacelle.NewImageTool("look", "Look at the page",
		func(context.Context, struct{}) (string, []nacelle.Image, error) {
			return "the page", []nacelle.Image{{MediaType: "image/png", Data: []byte{1}}}, nil
		}, nacelle.ToolOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("NewImageTool: %v", err)
	}
	return tool
}

// A decorator built with Wrap keeps everything the tool it wraps can do: its
// read-only declaration, its pictures and its streamed output.
func TestWrapKeepsWhatTheToolCanDo(t *testing.T) {
	var seen nacelle.Call
	wrapped := nacelle.Wrap(lookTool(t),
		func(spec nacelle.Spec) nacelle.Spec {
			spec.Description += " (logged)"
			return spec
		},
		func(next nacelle.RunFunc) nacelle.RunFunc {
			return func(ctx context.Context, call nacelle.Call, emit func(string)) (nacelle.Result, error) {
				seen = call
				emit("logged")
				return next(ctx, call, emit)
			}
		})

	spec := wrapped.Spec()
	if !spec.ReadOnly || spec.Name != "look" || spec.Description != "Look at the page (logged)" {
		t.Errorf("spec = %+v, want the inner spec with the rewrite", spec)
	}
	got := runTool(t, context.Background(), nacelle.Request{}, wrapped, `{}`)
	if seen.Name != "look" || seen.ID == "" {
		t.Errorf("the wrapper saw %+v, want the call", seen)
	}
	if len(got.tool.Images) != 1 || got.result != "the page" {
		t.Errorf("ran = %q with %d images, want the inner answer and its picture", got.result, len(got.tool.Images))
	}
	if events := got.toolEvents(); len(events) != 2 || events[0].Kind != nacelle.KindToolOutput {
		t.Errorf("events = %+v, want the wrapper's output before the result", events)
	}
}

// A nil rewrite and a nil wrapper leave the tool as it was.
func TestWrapWithNothingIsTheTool(t *testing.T) {
	inner := echoInput(t, "search")
	wrapped := nacelle.Wrap(inner, nil, nil)
	if wrapped.Spec().Name != "search" {
		t.Fatalf("spec = %+v, want the inner one", wrapped.Spec())
	}
	if got, err := callTool(context.Background(), wrapped, json.RawMessage(`{"query":"x"}`)); err != nil || got != "search:x" {
		t.Fatalf("callTool = %q, %v, want the inner answer", got, err)
	}
}
