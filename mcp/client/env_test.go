package client

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// PATH and HOME and nothing else. The rest of the process environment is where
// a service keeps its API keys, and an MCP server is a third-party program
// about to be handed model-chosen arguments.
func TestTheBaseEnvironmentIsPathAndHomeOnly(t *testing.T) {
	t.Setenv("HOME", "/home/somebody")
	t.Setenv(secretEnv, "sk-do-not-leak")

	got := environment(nil)
	want := []string{defaultPath, "HOME=/home/somebody"}
	if !slices.Equal(got, want) {
		t.Errorf("environment(nil) = %v, want %v", got, want)
	}
}

// A credential the server needs is named where the server is configured,
// rather than reaching it invisibly from whatever started the agent. Sorted,
// so two identical configurations produce identical environments.
func TestWhatTheCallerNamesIsAddedInAStableOrder(t *testing.T) {
	got := environment(map[string]string{"TOKEN": "t", "API_KEY": "k", "REGION": "r"})

	if want := []string{"API_KEY=k", "REGION=r", "TOKEN=t"}; !slices.Equal(got[len(got)-3:], want) {
		t.Errorf("environment(...) = %v, want it to end with %v", got, want)
	}
}

// The doc comment on Command.Path tells callers they can set their own PATH,
// which only holds because os/exec keeps the last value for a repeated key and
// the caller's entry comes last.
func TestACallerCanReplaceThePathBecauseTheirEntryComesLast(t *testing.T) {
	got := environment(map[string]string{"PATH": "/opt/bin"})

	if got[0] != defaultPath || got[len(got)-1] != "PATH=/opt/bin" {
		t.Errorf("environment(...) = %v, want the caller's PATH last", got)
	}
}

// The one that matters, run against a process that really was forked: a
// sentinel the parent holds must not reach the child, and what the caller
// named must.
func TestASubprocessSeesWhatItWasGivenAndNothingElse(t *testing.T) {
	t.Setenv(secretEnv, "sk-do-not-leak")

	set, err := Connect(t.Context(), helperCommand(t, "helper", map[string]string{tokenEnv: "explicitly-given"}))
	if err != nil {
		t.Fatalf("Connect = %v, want it to succeed", err)
	}
	defer func() { _ = set.Close() }()

	got, err := find(t, set.Tools(), "helper_environment").Run(t.Context(), nil)
	if err != nil {
		t.Fatalf("Run = %v, want it to succeed", err)
	}
	if strings.Contains(got, "sk-do-not-leak") {
		t.Errorf("the server read %q from the process environment", secretEnv)
	}
	if !strings.Contains(got, tokenEnv+"=explicitly-given") {
		t.Errorf("environment = %q, want the caller's variable to have arrived", got)
	}
}

// A sanity check on the check above: the parent really was holding the
// sentinel, so the test cannot pass because nothing set it in the first place.
func TestTheSentinelIsActuallySetInTheParent(t *testing.T) {
	t.Setenv(secretEnv, "sk-do-not-leak")

	if os.Getenv(secretEnv) != "sk-do-not-leak" {
		t.Fatal("the sentinel is not set, so the isolation test proves nothing")
	}
}
