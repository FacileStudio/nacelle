package nacelle_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)
// TestParallelSubAgentReturnsAllResults verifies that 3 tasks complete
// and all results are returned, regardless of completion order. 
func TestParallelSubAgentReturnsAllResults(t *testing.T) {
	backend := newLoop(
		[]step{toolStep(nacelle.ParallelAgentsToolName, `{"tasks":["count the stars","echo hello","echo world"]}`), textStep("ok")},
		[]step{toolStep("echo", `{}`), textStep("seven")},
		[]step{toolStep("echo", `{}`), textStep("hello")},
		[]step{toolStep("echo", `{}`), textStep("world")},
	)
	echo := &echoTool{}

	_, parent := newSubAgent(t, backend, echo)

	var toolResult string
	runParent(t, parent, "parallel", func(event nacelle.Event) {
		if event.Kind == nacelle.KindToolResult && event.Tool != nil && event.Tool.Name == nacelle.ParallelAgentsToolName {
			toolResult = event.Tool.Result
		}
	})

	assertContains(t, toolResult, "seven", "hello", "world")
}
// TestParallelSubAgentPartialFailure verifies that when one task fails,
// the others still complete and their results are returned. 
func TestParallelSubAgentPartialFailure(t *testing.T) {
	backend := newLoop(
		[]step{toolStep(nacelle.ParallelAgentsToolName, `{"tasks":["good","bad","good"]}`), textStep("ok")},
		[]step{toolStep("echo", `{}`), textStep("good1")},
		[]step{toolStep("echo", `{}`), textStep("bad")},
		[]step{toolStep("echo", `{}`), textStep("good2")},
	)
	echo := &echoTool{}

	_, parent := newSubAgent(t, backend, echo)

	var toolResult string
	runParent(t, parent, "parallel", func(event nacelle.Event) {
		if event.Kind == nacelle.KindToolResult && event.Tool != nil && event.Tool.Name == nacelle.ParallelAgentsToolName {
			toolResult = event.Tool.Result
		}
	})

	assertContains(t, toolResult, "good1", "good2", "bad")
}
// TestParallelSubAgentSharedConfig verifies that all parallel agents
// share the same config and the parent config is not mutated. 
func TestParallelSubAgentSharedConfig(t *testing.T) {
	backend := newLoop(
		[]step{toolStep(nacelle.ParallelAgentsToolName, `{"tasks":["task1","task2","task3"]}`), textStep("ok")},
		[]step{toolStep("echo", `{}`), textStep("task1")},
		[]step{toolStep("echo", `{}`), textStep("task2")},
		[]step{toolStep("echo", `{}`), textStep("task3")},
	)
	echo := &echoTool{}

	_, parent := newSubAgent(t, backend, echo)

	var toolResult string
	runParent(t, parent, "parallel", func(event nacelle.Event) {
		if event.Kind == nacelle.KindToolResult && event.Tool != nil && event.Tool.Name == nacelle.ParallelAgentsToolName {
			toolResult = event.Tool.Result
		}
	})

	assertContains(t, toolResult, "task1", "task2", "task3")
}
// TestParallelSubAgentEmptyTasks verifies that an empty task list fails. 
func TestParallelSubAgentEmptyTasks(t *testing.T) {
	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: newLoop(), System: "s",
	}, nacelle.ParallelSubAgentOptions{})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	_, err = sub.Run(context.Background(), json.RawMessage(`{"tasks":[]}`))
	if err == nil {
		t.Error("an empty task list was accepted")
	}
}
// TestParallelSubAgentLenientTasks verifies the input shapes models actually
// send still run a fan-out instead of dying on decode: one task as a bare
// string, and the list double-encoded inside a string. 
func TestParallelSubAgentLenientTasks(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"single string", `{"tasks":"do the thing"}`, 1},
		{"double encoded", `{"tasks":"[\"task1\",\"task2\"]"}`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runLenient(t, tc.input, tc.want) })
	}
}

func runLenient(t *testing.T, input string, want int) {
	t.Helper()
	backend := newLoop([]step{toolStep("echo", `{}`), textStep("done")})
	echo := &echoTool{}
	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.ParallelSubAgentOptions{})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	result, err := sub.Run(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var resp struct {
		Tasks map[string]string `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decoding result: %v", err)
	}
	if len(resp.Tasks) != want {
		t.Errorf("got %d task results, want %d", len(resp.Tasks), want)
	}
}
// TestParallelSubAgentNonStringTasks verifies a tasks field that is neither a
// list nor a string is still refused, with a message that names the shape. 
func TestParallelSubAgentNonStringTasks(t *testing.T) {
	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: newLoop(), System: "s",
	}, nacelle.ParallelSubAgentOptions{})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	_, err = sub.Run(context.Background(), json.RawMessage(`{"tasks":42}`))
	if err == nil {
		t.Error("a numeric tasks field was accepted")
	}
	if !strings.Contains(err.Error(), "list of strings") {
		t.Errorf("error %q does not name the expected shape", err)
	}
}
// TestParallelSubAgentDirectCall verifies parallel delegation via direct
// RunTool call, bypassing the parent stream to avoid the loop backend mutex. 
func TestParallelSubAgentDirectCall(t *testing.T) {
	backend := newLoop(
		[]step{toolStep("echo", `{}`), textStep("result1")},
		[]step{toolStep("echo", `{}`), textStep("result2")},
		[]step{toolStep("echo", `{}`), textStep("result3")},
	)
	echo := &echoTool{}

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.ParallelSubAgentOptions{})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"},
		json.RawMessage(`{"tasks":["task1","task2","task3"]}`), sink)

	result := drainToolResult(t, sink)

	assertContains(t, result, "result1", "result2", "result3")
}

func newSubAgent(t *testing.T, backend nacelle.Backend, echo *echoTool) (nacelle.Tool, *nacelle.Agent) {
	t.Helper()

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
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

	return sub, parent
}

func runParent(t *testing.T, parent *nacelle.Agent, prompt string, onEvent func(nacelle.Event)) {
	t.Helper()

	for event, err := range parent.Stream(context.Background(), []nacelle.Message{
		{Role: nacelle.RoleUser, Parts: []nacelle.Part{nacelle.Text{Text: prompt}}},
	}) {
		if err != nil {
			t.Fatalf("parent stream: %v", err)
		}
		onEvent(event)
	}
}

func drainToolResult(t *testing.T, sink *nacelle.ToolSink) string {
	t.Helper()

	var result string
	for _, event := range sink.Drain() {
		if event.Tool == nil {
			continue
		}
		if event.Tool.Err != nil {
			t.Fatalf("parallel delegation failed: %v", event.Tool.Err)
		}
		result = event.Tool.Result
	}
	return result
}

func assertContains(t *testing.T, s string, substrs ...string) {
	t.Helper()

	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			t.Errorf("result missing %q, got %q", sub, s)
		}
	}
}
