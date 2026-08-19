package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

// A non-zero exit is information, not a broken tool: a failing test suite is
// the answer to "run the tests".
func TestCommandReportsOutputAndExitStatus(t *testing.T) {
	set := newSet(t, nil)

	out, err := call(t, set, "run_command", commandInput{Command: "echo hello"})
	if err != nil || !strings.Contains(out, "hello") {
		t.Fatalf("run = %q, %v; want the command's output", out, err)
	}

	failed, err := call(t, set, "run_command", commandInput{Command: "exit 3"})
	if err != nil {
		t.Fatalf("a failing command returned an error: %v", err)
	}
	if !strings.Contains(failed, "exit status 3") {
		t.Errorf("output = %q, want the exit status", failed)
	}
}

// The process environment is where a service keeps the credentials the model
// must not be able to print, and `env` is a command like any other.
func TestCommandsDoNotInheritTheProcessEnvironment(t *testing.T) {
	t.Setenv("NACELLE_TEST_SECRET", "do-not-leak")
	set := newSet(t, nil)

	out, err := call(t, set, "run_command", commandInput{Command: "env"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "do-not-leak") {
		t.Errorf("the process environment leaked into the command:\n%s", out)
	}
}

func TestCommandTimesOutAndSaysSo(t *testing.T) {
	set := newSet(t, nil)

	started := time.Now()
	out, err := call(t, set, "run_command", commandInput{Command: "sleep 30", Timeout: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Errorf("the timeout took %s to fire", elapsed)
	}
	if !strings.Contains(out, "timed out") {
		t.Errorf("output = %q, want it to say the command timed out", out)
	}
}

// A timeout that kills only the shell leaves its children holding the port the
// next command needs.
func TestATimeoutKillsTheWholeProcessGroup(t *testing.T) {
	set := newSet(t, nil)
	marker := filepath.Join(set.Dir(), "child-alive")

	_, err := call(t, set, "run_command", commandInput{
		Command: "sh -c 'sleep 3; touch " + marker + "' & sleep 30",
		Timeout: 1,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	time.Sleep(4 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a child outlived the timeout; only the shell was killed")
	}
}

func TestReadOnlySetCannotWrite(t *testing.T) {
	set := newSet(t, map[string]string{"a.txt": "x"})
	readOnly, err := set.ReadOnly()
	if err != nil {
		t.Fatalf("ReadOnly: %v", err)
	}

	for name := range nacelle.ToolsByName(readOnly) {
		switch name {
		case "write_file", "edit_file", "run_command":
			t.Errorf("the read-only set offers %q", name)
		}
	}
}

func TestBashIsNotMountedUnlessAskedFor(t *testing.T) {
	dir := t.TempDir()
	set, err := New(Config{Root: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer set.Close()

	all, err := set.Tools()
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if _, found := nacelle.ToolsByName(all)["run_command"]; found {
		t.Error("run_command is mounted without AllowBash")
	}
}
