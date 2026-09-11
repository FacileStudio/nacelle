package client

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// In isolation a child gets PATH and HOME and nothing else. The rest of the
// process environment is where a service keeps its API keys, and a guarded
// server is one that receives only what was named for it.
func TestAnIsolatedEnvironmentIsPathAndHomeOnly(t *testing.T) {
	t.Setenv("HOME", "/home/somebody")
	t.Setenv(secretEnv, "sk-do-not-leak")

	got := environment(nil, true)
	want := []string{defaultPath, "HOME=/home/somebody"}
	if !slices.Equal(got, want) {
		t.Errorf("environment(nil, true) = %v, want %v", got, want)
	}
}

// The default is inheritance: the agent was launched from a shell, and the
// servers it starts need the same PATH and exports that shell had.
func TestTheDefaultEnvironmentInheritsTheProcess(t *testing.T) {
	t.Setenv(secretEnv, "sk-do-not-leak")
	t.Setenv("HOME", "/home/somebody")

	got := environment(nil, false)
	if !slices.Contains(got, secretEnv+"=sk-do-not-leak") {
		t.Errorf("environment(nil, false) = %v, want the process environment inherited", got)
	}
	if !slices.Contains(got, "HOME=/home/somebody") {
		t.Errorf("environment(nil, false) = %v, want HOME to have arrived", got)
	}
}

// A credential the server needs is named where the server is configured,
// rather than reaching it invisibly from whatever started the agent. Sorted,
// so two identical configurations produce identical environments.
func TestWhatTheCallerNamesIsAddedInAStableOrder(t *testing.T) {
	got := environment(map[string]string{"TOKEN": "t", "API_KEY": "k", "REGION": "r"}, false)

	if want := []string{"API_KEY=k", "REGION=r", "TOKEN=t"}; !slices.Equal(got[len(got)-3:], want) {
		t.Errorf("environment(...) = %v, want it to end with %v", got, want)
	}
}

// In either mode, what the caller names overrides what was inherited, because
// the caller's entries come last and os/exec keeps the last value.
func TestACallerCanReplaceAnInheritedValueBecauseTheirEntryComesLast(t *testing.T) {
	got := environment(map[string]string{"PATH": "/opt/bin"}, false)

	if got[len(got)-1] != "PATH=/opt/bin" {
		t.Errorf("environment(...) = %v, want the caller's PATH last", got)
	}
}

// The one that matters, run against a process that really was forked. With
// IsolateEnv set, a sentinel the parent holds must not reach the child, and
// what the caller named must.
func TestAnIsolatedSubprocessSeesWhatItWasGivenAndNothingElse(t *testing.T) {
	t.Setenv(secretEnv, "sk-do-not-leak")

	command := helperCommand(t, "helper", map[string]string{tokenEnv: "explicitly-given"})
	command.IsolateEnv = true
	set, err := Connect(t.Context(), command)
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

// The default inheritance, against the same real fork: the sentinel does
// reach the child, because that is what inheritance means and a server
// relying on it must not be told otherwise.
func TestASubprocessByDefaultInheritsTheParentEnvironment(t *testing.T) {
	t.Setenv(secretEnv, "sk-do-not-leak")

	set, err := Connect(t.Context(), helperCommand(t, "helper", nil))
	if err != nil {
		t.Fatalf("Connect = %v, want it to succeed", err)
	}
	defer func() { _ = set.Close() }()

	got, err := find(t, set.Tools(), "helper_environment").Run(t.Context(), nil)
	if err != nil {
		t.Fatalf("Run = %v, want it to succeed", err)
	}
	if !strings.Contains(got, secretEnv+"=sk-do-not-leak") {
		t.Errorf("environment = %q, want the inherited variable to have arrived", got)
	}
}

// WithEnvIsolation is the bulk switch a harness uses from one security
// setting; it must reach the servers Parse built.
func TestWithEnvIsolationFlipsEveryCommand(t *testing.T) {
	command := helperCommand(t, "helper", nil)
	servers := WithEnvIsolation([]Server{command}, true)

	flipped, ok := servers[0].(Command)
	if !ok || !flipped.IsolateEnv {
		t.Fatalf("WithEnvIsolation left %T with IsolateEnv false", servers[0])
	}
	if command.IsolateEnv {
		t.Errorf("the slice that went in was mutated; the copy is the contract")
	}
}

// A sanity check on the checks above: the parent really was holding the
// sentinel, so the isolation test cannot pass because nothing set it.
func TestTheSentinelIsActuallySetInTheParent(t *testing.T) {
	t.Setenv(secretEnv, "sk-do-not-leak")

	if os.Getenv(secretEnv) != "sk-do-not-leak" {
		t.Fatal("the sentinel is not set, so the isolation test proves nothing")
	}
}
