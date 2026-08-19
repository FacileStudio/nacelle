package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// needsJardin skips a test on a machine without the jardin binary, the same
// way TestOrphanIsReaped skips without setsid: a tool this package only
// builds when the binary is present has nothing to test without it.
func needsJardin(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("jardin"); err != nil {
		t.Skip("jardin is not installed")
	}
}

func TestJardinIsAbsentWithoutTheBinaryAndThatIsNotAnError(t *testing.T) {
	if _, err := exec.LookPath("jardin"); err == nil {
		t.Skip("jardin is installed on this machine; this test wants it absent")
	}

	tools, err := Jardin()
	if err != nil {
		t.Fatalf("Jardin: %v, want no error when the binary is simply missing", err)
	}
	if tools != nil {
		t.Errorf("tools = %v, want nil when there is nothing to build", tools)
	}
}

func TestJardinBuildsThreeToolsWhenInstalled(t *testing.T) {
	needsJardin(t)

	tools, err := Jardin()
	if err != nil {
		t.Fatalf("Jardin: %v", err)
	}

	byName := nacelle.ToolsByName(tools)
	for _, name := range []string{"list_flows", "run_flow", "search_memory"} {
		if _, found := byName[name]; !found {
			t.Errorf("tools = %v, want %q among them", names(tools), name)
		}
	}
}

func TestRunFlowReportsAnUnknownNameAsTextNotAGoError(t *testing.T) {
	needsJardin(t)

	tool := jardinTool(t, "run_flow")
	input, err := json.Marshal(runFlowInput{Name: "definitely-not-a-real-flow-xyz"})
	if err != nil {
		t.Fatalf("marshalling input: %v", err)
	}

	result, err := tool.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run returned a Go error rather than reporting the failure as text: %v", err)
	}
	if !strings.Contains(result, "exit status") {
		t.Errorf("result = %q, want it to say the command failed", result)
	}
}

func TestSearchMemoryReturnsText(t *testing.T) {
	needsJardin(t)

	tool := jardinTool(t, "search_memory")
	input, err := json.Marshal(searchMemoryInput{Query: "jardin"})
	if err != nil {
		t.Fatalf("marshalling input: %v", err)
	}

	result, err := tool.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(result) == "" {
		t.Error("result is empty, want at least a report of what was searched")
	}
}

func TestListFlowsReturnsText(t *testing.T) {
	needsJardin(t)

	tool := jardinTool(t, "list_flows")

	result, err := tool.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(result) == "" {
		t.Error("result is empty, want at least the empty-list report jardin itself prints")
	}
}

// jardinTool fetches one tool from Jardin() by name, failing the test if it
// is missing rather than letting a later nil-pointer panic stand in for the
// real assertion.
func jardinTool(t *testing.T, name string) nacelle.Tool {
	t.Helper()

	tools, err := Jardin()
	if err != nil {
		t.Fatalf("Jardin: %v", err)
	}
	tool, found := nacelle.ToolsByName(tools)[name]
	if !found {
		t.Fatalf("no tool named %q, want it among %v", name, names(tools))
	}
	return tool
}

func names(tools []nacelle.Tool) []string {
	list := make([]string, len(tools))
	for i, tool := range tools {
		list[i] = tool.Name()
	}
	return list
}
