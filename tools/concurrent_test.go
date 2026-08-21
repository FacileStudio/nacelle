package tools_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/tools"
)

// One tool set, many callers reading at once, each getting its own file back.
//
// os.Root is documented safe from several goroutines, so the descriptor
// underneath is not the risk; what this holds still is the tools above it,
// which would stop being safe the moment one grew a field to remember
// something between calls. That is a plausible change to make and an
// implausible one to notice, since every other test here calls one tool once.
func TestOneToolSetServesManyCallersWithoutCrossingThem(t *testing.T) {
	const callers = 20
	root := populated(t, callers)

	set, err := tools.New(tools.Config{Root: root})
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	defer func() { _ = set.Close() }()

	built, err := set.ReadOnly()
	if err != nil {
		t.Fatalf("ReadOnly = %v", err)
	}

	answers, failures := readTogether(t, named(t, built, "read_file"), callers)
	for i, answer := range answers {
		want := fmt.Sprintf("contents-%d", i)
		switch {
		case failures[i] != nil:
			t.Errorf("caller %d: %v", i, failures[i])
		case !strings.Contains(answer, want):
			t.Errorf("caller %d read %q, want it to contain %q — the reads crossed", i, answer, want)
		}
	}
}

// populated is a directory holding one distinguishable file per caller.
func populated(t *testing.T, files int) string {
	t.Helper()

	root := t.TempDir()
	for i := range files {
		name := filepath.Join(root, fmt.Sprintf("file-%d.txt", i))
		if err := os.WriteFile(name, fmt.Appendf(nil, "contents-%d", i), 0o600); err != nil {
			t.Fatalf("writing the fixture: %v", err)
		}
	}
	return root
}

// readTogether starts every caller at once, each on the one file that is its
// own, and waits for all of them.
func readTogether(t *testing.T, read nacelle.Tool, callers int) ([]string, []error) {
	t.Helper()

	answers := make([]string, callers)
	failures := make([]error, callers)

	var waiting sync.WaitGroup
	for i := range callers {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			asked := fmt.Sprintf(`{"path":"file-%d.txt"}`, i)
			answers[i], failures[i] = read.Run(t.Context(), json.RawMessage(asked))
		}()
	}
	waiting.Wait()
	return answers, failures
}

// named finds one tool by name, failing the test rather than the assertion
// below it when the set stops offering it.
func named(t *testing.T, built []nacelle.Tool, name string) nacelle.Tool {
	t.Helper()

	for _, tool := range built {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("no tool named %q", name)
	return nil
}
