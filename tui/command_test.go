package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
)

// /clear is the whole point of naming it a command rather than a message:
// it has to reset state a real question never touches.
func TestSlashClearResetsTranscriptConversationAndSpent(t *testing.T) {
	m := sized()
	m.conversation = append(m.conversation, nacelle.UserText("earlier"))
	m.spent = nacelle.Usage{InputTokens: 42}

	m.prompt.SetValue("/clear")
	m.ask()

	if len(m.conversation) != 0 {
		t.Errorf("conversation = %v, want it emptied", m.conversation)
	}
	if m.spent != (nacelle.Usage{}) {
		t.Errorf("spent = %+v, want it reset", m.spent)
	}
	if said := spoken(m); len(said) == 0 || !strings.Contains(said[len(said)-1], "cleared") {
		t.Errorf("said = %v, want the last line to report the session was cleared", said)
	}
}

// /help has to name every command it does not itself explain — it is the
// only place any of them are listed.
func TestSlashHelpListsCommandsWithoutStartingARun(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/help")

	m.ask()

	lines := spoken(m)
	if len(lines) != 2 {
		t.Fatalf("transcript = %v, want the echoed command plus one reply", lines)
	}
	for _, want := range []string{"/clear", "/help", "/quit"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("help text = %q, want it to mention %q", lines[1], want)
		}
	}
	if m.run.busy {
		t.Error("/help started a run")
	}
}

func TestSlashQuitReturnsTeaQuit(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/quit")

	cmd := m.ask()

	if cmd == nil {
		t.Fatal("/quit returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("/quit did not resolve to tea.QuitMsg")
	}
}

// A typo is far more likely than a real question starting with a slash, so
// an unrecognised command is reported rather than sent to the model as text.
func TestAnUnknownSlashCommandIsReportedAndDoesNotReachTheModel(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/cler")

	m.ask()

	if len(m.conversation) != 0 {
		t.Error("an unknown command reached the model's conversation")
	}
	lines := spoken(m)
	if len(lines) != 2 || !strings.Contains(lines[1], "/cler") || !strings.Contains(lines[1], "/help") {
		t.Errorf("transcript = %v, want the echoed input plus a line naming the bad command and pointing at /help", lines)
	}
}

// No command may start a run: m.agent is nil in this test, and start()
// dereferences it in a goroutine, so a command that fell through to the
// model path would crash the whole test binary rather than fail cleanly.
func TestACommandNeverStartsARunEvenWithNoAgent(t *testing.T) {
	for _, line := range []string{"/clear", "/help", "/quit", "/nope"} {
		m := sized()
		m.prompt.SetValue(line)

		m.ask()

		if m.run.busy {
			t.Errorf("%q left a run busy", line)
		}
	}
}

func TestParseCommandIgnoresTextWithoutALeadingSlash(t *testing.T) {
	if _, ok := sized().parseCommand("clear the transcript please"); ok {
		t.Error("text with no leading slash was treated as a command")
	}
}

// commandNames is the dropdown menu's only source of the client's own
// commands (menuItems, in menu.go) — every registered command has to reach
// it, "/"-prefixed, or it never appears there even though "/name" still
// works once entered by hand.
func TestCommandNamesListsEveryRegisteredCommandSlashPrefixed(t *testing.T) {
	names := commandNames()
	if len(names) != len(commands) {
		t.Fatalf("commandNames() = %v, want one entry per registered command", names)
	}
	for _, name := range names {
		if _, ok := commands[strings.TrimPrefix(name, "/")]; !ok {
			t.Errorf("commandNames() included %q, which names no registered command", name)
		}
	}
}

// A model built by newModel is the one a reader actually types into, so the
// dropdown's candidate pool has to be wired there rather than merely exist.
func TestNewModelWiresCommandsIntoTheMenu(t *testing.T) {
	m := sized()
	if len(m.menu.items) != len(commands) {
		t.Errorf("menu.items = %+v, want exactly the client's own commands with no skills loaded", m.menu.items)
	}
}
