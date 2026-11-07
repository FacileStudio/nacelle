package anthropic

import (
	"context"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// A tool returning EndTurn ends the run after its round: the other call still
// runs, both results are successes, and no further turn is requested.
func TestEndTurnYieldsAfterTheRound(t *testing.T) {
	ask, err := nacelle.NewTool("ask", "Ask the user", func(context.Context, struct{}) (string, error) {
		return "", nacelle.EndTurn("waiting for the user")
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	backend := New(Config{Client: stub(t,
		sse(t, messageStart(),
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"ask","input":{}}}`,
			arguments(t, 0, `{}`),
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_2","name":"echo","input":{}}}`,
			arguments(t, 1, `{"text":"hi"}`),
			`{"type":"content_block_stop","index":1}`,
			messageDelta("tool_use"), `{"type":"message_stop"}`),
	)})

	events := collect(t, backend, nacelle.Request{Tools: []nacelle.Tool{ask, echoTool(t)}, MaxTokens: 1024})

	results := toolsOf(events, nacelle.KindToolResult)
	if r := results["toolu_1"]; r == nil || r.Err != nil || r.Result != "waiting for the user" {
		t.Fatalf("ask result = %+v, want a success carrying the EndTurn result", r)
	}
	if r := results["toolu_2"]; r == nil || r.Err != nil {
		t.Fatalf("echo result = %+v, want it to finish", r)
	}
	if last := events[len(events)-1]; last.Kind != nacelle.KindDone || last.Stop != nacelle.StopYield {
		t.Fatalf("last event = %+v, want KindDone with StopYield", last)
	}
}
