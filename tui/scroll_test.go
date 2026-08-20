package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
)

// filled is a model holding more transcript than fits on screen, which is the
// only state in which scrolling means anything at all.
func filled() *model {
	m := sized()
	for i := range 40 {
		m.say(fromReader, fmt.Sprintf("question %d", i))
	}
	return m
}

// pressing is a key as bubbletea delivers it, checked against the library's own
// name for it so that a rename fails here rather than silently unbinding it.
func pressing(t *testing.T, code rune, name string) tea.KeyPressMsg {
	t.Helper()

	press := tea.KeyPressMsg{Code: code}
	if press.String() != name {
		t.Fatalf("constructed key = %q, want %q", press.String(), name)
	}
	return press
}

// A transcript nothing can scroll loses everything above the fold, and on a run
// that used tools that is most of the run.
func TestTheTranscriptCanBeScrolledBack(t *testing.T) {
	m := filled()
	bottom := m.viewport.YOffset()
	if bottom == 0 {
		t.Fatal("the transcript fits on screen, so this proves nothing")
	}

	if !m.scroll(pressing(t, tea.KeyPgUp, "pgup")) {
		t.Fatal("pgup was passed through to the prompt instead of scrolling")
	}
	if got := m.viewport.YOffset(); got >= bottom {
		t.Errorf("offset = %d, want it above the bottom at %d", got, bottom)
	}
}

// Following the stream has to be something the reader can stop. Every text
// delta redraws, so a transcript pinned to the bottom on every redraw is one
// that cannot be read at all while a run is going.
func TestNewOutputLeavesAScrolledBackReaderWhereTheyAre(t *testing.T) {
	m := filled()
	m.scroll(pressing(t, tea.KeyPgUp, "pgup"))
	parked := m.viewport.YOffset()

	m.absorb(nacelle.Event{Kind: nacelle.KindText, Text: "a fresh delta"})
	m.render()

	if got := m.viewport.YOffset(); got != parked {
		t.Errorf("offset = %d, want it left at %d where the reader put it", got, parked)
	}
}

// The other half of that bargain: returning to the bottom has to start the
// stream following again, or reading back once costs the rest of the session.
func TestScrollingBackToTheBottomFollowsAgain(t *testing.T) {
	m := filled()
	m.scroll(pressing(t, tea.KeyPgUp, "pgup"))
	m.scroll(pressing(t, tea.KeyPgDown, "pgdown"))

	m.say(fromModel, "the newest line")

	if !strings.Contains(visible(m.viewport.View()), "the newest line") {
		t.Errorf("viewport = %q, want it following the transcript again", m.viewport.View())
	}
}

// A reader parked halfway up while a run streams sees a screen that has stopped
// moving, which is what a client that hung looks like too.
func TestTheStatusLineSaysWhenTheReaderIsScrolledBack(t *testing.T) {
	m := filled()
	if status := m.status(); strings.Contains(status, "scrolled back") {
		t.Fatalf("status = %q, want nothing said while the reader is at the end", status)
	}

	m.scroll(pressing(t, tea.KeyPgUp, "pgup"))

	if status := m.status(); !strings.Contains(status, "scrolled back") {
		t.Errorf("status = %q, want it to say the transcript is not at its end", status)
	}
}

// The wheel is how a terminal application is scrolled, and an alt-screen one
// has no terminal scrollback to fall through to when it does not handle it.
func TestTheMouseWheelScrollsTheTranscript(t *testing.T) {
	m := filled()
	bottom := m.viewport.YOffset()
	if bottom == 0 {
		t.Fatal("the transcript fits on screen, so this proves nothing")
	}

	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})

	if got := m.viewport.YOffset(); got >= bottom {
		t.Errorf("offset = %d, want it above the bottom at %d", got, bottom)
	}
}

// A wheel message the model never routes is a wheel that does nothing, which
// is the state this client shipped in — so assert on Update, not on wheel.
func TestTheMouseWheelScrollsBackDownAgain(t *testing.T) {
	m := filled()
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	parked := m.viewport.YOffset()

	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})

	if got := m.viewport.YOffset(); got <= parked {
		t.Errorf("offset = %d, want it below where the wheel left it at %d", got, parked)
	}
}

// Asking a question while scrolled back used to put the echo of it, and the
// whole answer under it, off-screen — so pressing enter emptied the prompt and
// visibly did nothing else.
func TestSendingReturnsAScrolledBackReaderToTheEnd(t *testing.T) {
	m := filled()
	m.agent = answering(t)
	defer m.run.cancel()

	m.scroll(pressing(t, tea.KeyPgUp, "pgup"))
	if m.viewport.AtBottom() {
		t.Fatal("pgup did not scroll back, so this proves nothing")
	}

	m.prompt.SetValue("a question asked from halfway up")
	m.ask()

	if !strings.Contains(visible(m.viewport.View()), "a question asked from halfway up") {
		t.Errorf("viewport = %q, want the question that was just sent on screen", m.viewport.View())
	}
}

// The mouse mode is load-bearing, not decoration: with none set the terminal
// never reports a wheel event, so every test above passes against a client
// whose wheel is dead in the one place it is actually used.
func TestTheViewAsksTheTerminalForMouseEvents(t *testing.T) {
	if mode := filled().View().MouseMode; mode != tea.MouseModeCellMotion {
		t.Errorf("MouseMode = %v, want %v so the wheel is reported at all", mode, tea.MouseModeCellMotion)
	}
}

// The viewport's own bindings are written for a pager that owns the keyboard —
// j, k, f, b, u, d and space all scroll it. Handing it the press would mean
// half the alphabet moved the transcript instead of reaching the question.
func TestOrdinaryTypingIsLeftToThePrompt(t *testing.T) {
	m := filled()
	for _, code := range []rune{'j', 'k', 'f', 'b', 'u', 'd', ' '} {
		if handled, _ := m.key(tea.KeyPressMsg{Code: code}); handled {
			t.Errorf("%q was taken as a scroll binding, so it cannot be typed", string(code))
		}
	}
}
