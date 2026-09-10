package anthropic

import (
	"context"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

// The whole reason the agent loop exists: a tool that streams its output must
// deliver a line to the consumer before the result, not only in a burst once
// the tool finishes. The loop drains the sink on a tick while the tool runs,
// which is what makes a line tick instead of snap.
func TestStreamedToolOutputArrivesBeforeItsResult(t *testing.T) {
	tool, err := nacelle.NewOutputTool("slow_echo", "Answers after a pause", func(_ context.Context, in struct {
		Text string `json:"text"`
	}, emit func(string)) (string, error) {
		emit("first line\n")
		time.Sleep(80 * time.Millisecond)
		emit("second line\n")
		return in.Text, nil
	})
	if err != nil {
		t.Fatalf("NewOutputTool: %v", err)
	}

	backend := New(Config{Client: stub(t,
		sse(t, messageStart(),
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"slow_echo","input":{}}}`,
			arguments(t, 0, `{"text":"hi"}`),
			`{"type":"content_block_stop","index":0}`,
			messageDelta("tool_use"), `{"type":"message_stop"}`),
		sse(t, messageStart(), messageDelta("end_turn"), `{"type":"message_stop"}`),
	)})

	events := collect(t, backend, nacelle.Request{
		Tools:         []nacelle.Tool{tool},
		MaxTokens:     1024,
		MaxIterations: 4,
	})

	// Record arrival order in the stream rather than trusting wall-clock
	// timestamps, which are not reliable for a delta in the tens of ms.
	var sequence []nacelle.Kind
	for _, event := range events {
		sequence = append(sequence, event.Kind)
	}

	firstOutput := indexOfKind(sequence, nacelle.KindToolOutput)
	result := indexOfKind(sequence, nacelle.KindToolResult)
	if firstOutput < 0 {
		t.Fatalf("no KindToolOutput in the stream; events were %v", sequence)
	}
	if result < 0 {
		t.Fatal("no KindToolResult; the run did not close the call")
	}
	if firstOutput >= result {
		t.Errorf("the first output (%d) did not precede the result (%d); output still snaps", firstOutput, result)
	}
	if len(sequence) < 2 {
		t.Fatalf("stream too short: %v", sequence)
	}
}

func indexOfKind(seq []nacelle.Kind, want nacelle.Kind) int {
	for i, k := range seq {
		if k == want {
			return i
		}
	}
	return -1
}
