package nacelle_test

import (
	"context"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// A nested run that stops short says so rather than handing back a truncated
// answer shaped like a whole one — finish's note reaches the model inside the
// task result.
func TestParallelSubAgentReportsAnUnfinishedRun(t *testing.T) {
	backend := newLoop(
		[]step{toolStep(nacelle.ParallelAgentsToolName, `{"tasks":["task"]}`), textStep("ok")},
		[]step{textStep("half a thought", nacelle.StopMaxTokens)},
	)

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s",
	}, nacelle.ParallelSubAgentOptions{})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}
	parent, err := nacelle.New(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{sub}, MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var result string
	for event, err := range parent.Stream(context.Background(), []nacelle.Message{{Role: nacelle.RoleUser}}) {
		if err != nil {
			t.Fatalf("parent stream: %v", err)
		}
		if event.Kind == nacelle.KindToolResult && event.Tool != nil && event.Tool.Name == nacelle.ParallelAgentsToolName {
			result = event.Tool.Result
		}
	}
	if !strings.Contains(result, "half a thought") || !strings.Contains(result, "max_tokens") {
		t.Errorf("result = %q, want the partial answer plus the stop reason", result)
	}
}
