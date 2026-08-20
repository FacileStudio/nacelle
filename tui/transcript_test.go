package main

import (
	"strings"
	"testing"
)

// The alternate screen hands the old one back on exit, so quitting un-draws
// the conversation rather than scrolling it away — and this client keeps no
// session files to recover it from. Printing it once on the way out is the
// only thing standing between a finished session and a lost one.
func TestQuittingLeavesTheWholeSessionInTheTerminal(t *testing.T) {
	m := sized()
	m.say(fromReader, "what did I ask")
	m.say(fromModel, "what it answered")
	m.say(fromTool, "read_file {}")

	kept := visible(m.session())
	for _, want := range []string{"what did I ask", "what it answered", "read_file"} {
		if !strings.Contains(kept, want) {
			t.Errorf("session = %q, want it to still hold %q", kept, want)
		}
	}
}

// Order is the whole value of a transcript: an answer printed above the
// question it answers is not a record of anything.
func TestTheKeptSessionIsInTheOrderItHappened(t *testing.T) {
	m := sized()
	m.say(fromReader, "asked first")
	m.say(fromModel, "answered second")

	kept := visible(m.session())
	if first, second := strings.Index(kept, "asked first"), strings.Index(kept, "answered second"); first > second {
		t.Errorf("session = %q, want the question before the answer", kept)
	}
}

// Launching in the wrong directory and quitting straight back out is the
// common way to ask nothing at all. Echoing the banner at someone who typed
// one key is noise, not a record.
func TestQuittingWithoutAskingAnythingLeavesTheTerminalAlone(t *testing.T) {
	m := sized()
	if kept := m.session(); kept != "" {
		t.Errorf("session = %q, want nothing printed for a session nobody used", kept)
	}
}

// The banner names the backend and model, which is the one line that says
// what produced everything under it — a record that opens without it cannot
// be read months later.
func TestTheKeptSessionOpensWithTheBanner(t *testing.T) {
	m := sized()
	m.say(fromReader, "a question")

	if kept := visible(m.session()); !strings.HasPrefix(kept, "test · model") {
		t.Errorf("session = %q, want it to open with the banner", kept)
	}
}
