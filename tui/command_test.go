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
	printed := printedBy(m.ask())

	if len(m.conversation) != 0 {
		t.Errorf("conversation = %v, want it emptied", m.conversation)
	}
	if m.spent != (nacelle.Usage{}) {
		t.Errorf("spent = %+v, want it reset", m.spent)
	}

	echo, banner := strings.Index(printed, "/clear"), strings.LastIndex(printed, "cleared")
	if echo < 0 || banner < 0 {
		t.Fatalf("printed = %q, want the echoed command and the fresh banner both in it", printed)
	}
	if echo > banner {
		t.Errorf("printed = %q, want the echo of the command above the banner that replaced it", printed)
	}
	if blanks := strings.Count(printed[echo:banner], "\n"); blanks < m.windowHeight {
		t.Errorf("only %d blank rows between the echo and the banner, want the window's %d — "+
			"the banner has to go up after the run that scrolls the old session away, not with it",
			blanks, m.windowHeight)
	}
}

// The screen is cleared by scrolling it away, so the blank run has to cover
// the whole window — a shorter one leaves the tail of the old session sitting
// above the fresh banner, which is what "cleared" apparently not clearing
// anything looked like.
func TestScrolledAwayCoversTheWholeWindow(t *testing.T) {
	if got := strings.Count(scrolledAway(40), "\n"); got != 40 {
		t.Errorf("scrolledAway(40) = %d lines, want 40", got)
	}
	if got := scrolledAway(0); got == "" {
		t.Error("scrolledAway(0) = \"\", want at least one line — insertAbove drops an empty string")
	}
}

// /help has to name every command it does not itself explain — it is the
// only place any of them are listed.
func TestSlashHelpListsCommandsWithoutStartingARun(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/help")

	printed := printedBy(m.ask())

	for _, want := range []string{"/clear", "/help", "/quit"} {
		if !strings.Contains(printed, want) {
			t.Errorf("help text = %q, want it to mention %q", printed, want)
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
	if printed := printedBy(cmd); !strings.Contains(printed, "/quit") {
		t.Errorf("printed = %q, want the echo handed over before the quit that would take it with it", printed)
	}
	cmds, ordered := sequenced(cmd())
	if !ordered {
		t.Fatalf("/quit produced %T, want the echo sequenced ahead of the quit", cmd())
	}
	if _, quits := cmds[len(cmds)-1]().(tea.QuitMsg); !quits {
		t.Error("/quit did not resolve to tea.QuitMsg")
	}
}

// A typo is far more likely than a real question starting with a slash, so
// an unrecognised command is reported rather than sent to the model as text.
func TestAnUnknownSlashCommandIsReportedAndDoesNotReachTheModel(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/cler")

	printed := printedBy(m.ask())

	if len(m.conversation) != 0 {
		t.Error("an unknown command reached the model's conversation")
	}
	echo, reply := strings.Index(printed, "/cler"), strings.Index(printed, "unknown command")
	if reply < 0 || !strings.Contains(printed, "/help") {
		t.Fatalf("printed = %q, want a line naming the bad command and pointing at /help", printed)
	}
	if echo < 0 || echo > reply {
		t.Errorf("printed = %q, want the echoed input above the reply to it", printed)
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
