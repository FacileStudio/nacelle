package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
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

// Writing output into a buffer makes os/exec insert a pipe, and Wait waits for
// every holder of its write end. A grandchild that called setsid is not in the
// group the kill reaches, so without a WaitDelay the tool call never returns
// and an agent that ran `./server &` has hung the daemon.
func TestAnOrphanHoldingThePipesDoesNotHangTheToolCall(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid is not installed, so no orphan can leave the process group")
	}
	set := newSet(t, nil)

	started := time.Now()
	out, err := call(t, set, "run_command", commandInput{
		Command: "setsid sh -c 'sleep 25' & sleep 30",
		Timeout: 1,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Errorf("the tool call took %s to return; the orphan held it open", elapsed)
	}
	if !strings.Contains(out, "timed out") {
		t.Errorf("output = %q, want it to say the command timed out", out)
	}
}

// The deadline and the caller giving up both close the same channel, and
// telling the model a command ran for its full ceiling when the user pressed
// ctrl+c is a lie that also reads as a successful tool round.
func TestACallerCancellationIsNotDressedUpAsATimeout(t *testing.T) {
	set := newSet(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(200*time.Millisecond, cancel)

	out, err := set.run(ctx, "sleep 30", time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run = %q, %v; want the cancellation reported as an error", out, err)
	}
	if strings.Contains(out, "timed out") {
		t.Errorf("output = %q, want no invented timeout", out)
	}
}

// The ceiling belongs to whoever mounted the tool. A model asking for a day is
// clamped, and one asking for a number that overflows a Duration into a
// negative one must not get a command that expires before it starts.
func TestTheModelCannotRaiseTheOperatorsTimeoutCeiling(t *testing.T) {
	ceiling := 5 * time.Minute
	asked := map[int]time.Duration{
		0:       ceiling,
		-1:      ceiling,
		30:      30 * time.Second,
		86400:   ceiling,
		1 << 40: ceiling,
	}

	for seconds, want := range asked {
		if got := bounded(seconds, ceiling); got != want {
			t.Errorf("bounded(%d) = %s, want %s", seconds, got, want)
		}
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
