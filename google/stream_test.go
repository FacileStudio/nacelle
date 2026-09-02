package google_test

import (
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// toolStream is the outcome of a stream that exercises tool calls, returned
// as one value so callers do not have to juggle four return values.
type toolStream struct {
	text          string
	sawToolCall   bool
	sawToolResult bool
	toolResult    string
}

// collectStream runs a stream to completion, returning the accumulated text,
// whether it ended with KindDone, and the usage from that event.
func collectStream(t *testing.T, seq func(yield func(nacelle.Event, error) bool)) (string, bool, nacelle.Usage) {
	t.Helper()
	var text string
	var done bool
	var usage nacelle.Usage
	seq(func(event nacelle.Event, err error) bool {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		switch event.Kind {
		case nacelle.KindText:
			text += event.Text
		case nacelle.KindDone:
			done = true
			usage = event.Usage
		}
		return true
	})
	return strings.TrimSpace(text), done, usage
}

// collectToolStream runs a stream carrying tool calls, returning its outcome
// as a single toolStream value.
func collectToolStream(t *testing.T, seq func(yield func(nacelle.Event, error) bool)) toolStream {
	t.Helper()
	var out toolStream
	seq(func(event nacelle.Event, err error) bool {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		switch event.Kind {
		case nacelle.KindToolCall:
			out.sawToolCall = true
		case nacelle.KindToolResult:
			out.sawToolResult = true
			if event.Tool != nil {
				out.toolResult = event.Tool.Result
			}
		case nacelle.KindText:
			out.text += event.Text
		}
		return true
	})
	out.text = strings.TrimSpace(out.text)
	return out
}