package client

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// dying is a server that explains itself on the way out, the way a
// misconfigured one does, and then is not a server at all.
func dying(name, complaint string) Command {
	return Command{
		Name: name,
		Path: "/bin/sh",
		Args: []string{"-c", "echo '" + complaint + "' >&2; exit 1"},

		Timeout: 5 * time.Second,
	}
}

// The reason a server failed to start is the sentence it printed, not the
// protocol step that noticed the silence afterwards.
func TestAServerThatFailsToStartSaysWhyInTheError(t *testing.T) {
	_, err := Connect(t.Context(), dying("docs", "FATAL: GITHUB_TOKEN is not set"))
	if err == nil {
		t.Fatal("Connect accepted a server that exits immediately")
	}
	if !strings.Contains(err.Error(), "FATAL: GITHUB_TOKEN is not set") {
		t.Errorf("Connect = %v, want it to carry what the server wrote to stderr", err)
	}
	if !strings.Contains(err.Error(), "docs") {
		t.Errorf("Connect = %v, want it to name the server", err)
	}
}

// Command.Stderr is for the rest of what a server says, over the life of a run
// that started fine.
func TestCommandStderrReceivesWhatTheServerWrites(t *testing.T) {
	var log bytes.Buffer
	command := dying("docs", "starting up")
	command.Stderr = &log

	if _, err := Connect(t.Context(), command); err == nil {
		t.Fatal("Connect accepted a server that exits immediately")
	}
	if !strings.Contains(log.String(), "starting up") {
		t.Errorf("Command.Stderr = %q, want what the server wrote", log.String())
	}
}

// A server that floods stderr costs a bounded amount of memory and says that
// it was cut, rather than quietly presenting a fragment as the whole of it.
func TestFloodedDiagnosticsAreBoundedAndSaySo(t *testing.T) {
	notes := &diagnostics{}
	if _, err := notes.Write(bytes.Repeat([]byte("x"), stderrLimit*3)); err != nil {
		t.Fatalf("Write = %v, want a writer that never fails", err)
	}

	note := notes.note()
	if len(note) > stderrLimit+200 {
		t.Errorf("note is %d bytes, want it bounded near %d", len(note), stderrLimit)
	}
	if !strings.Contains(note, "first") {
		t.Errorf("note = %.80q..., want it to admit it was cut", note)
	}
}

// A server that said nothing gets no clause, because a dangling "the server
// wrote:" reads as output that went missing rather than output never sent.
func TestASilentServerAddsNothingToTheError(t *testing.T) {
	notes := &diagnostics{}
	if _, err := notes.Write([]byte("   \n\t ")); err != nil {
		t.Fatalf("Write = %v, want a writer that never fails", err)
	}
	if note := notes.note(); note != "" {
		t.Errorf("note = %q, want nothing at all", note)
	}
}
