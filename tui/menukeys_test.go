package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Typing '/' alone is what opens the dropdown at all — with no query yet,
// refreshMenu has to show the full pool, not wait for a second character.
func TestRefreshMenuOpensOnASlashWithNoQuery(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/")

	m.refreshMenu()

	if !m.menu.open() {
		t.Fatal("menu did not open on a bare '/'")
	}
	if len(m.menu.filtered) != len(commands) {
		t.Errorf("filtered = %+v, want every command with no query yet", m.menu.filtered)
	}
}

// Deleting back past the '/' has to close the menu and forget any earlier
// esc — otherwise a dismissed dropdown stays dismissed for a line that has
// not even been typed yet.
func TestRefreshMenuClosesAndForgetsDismissalOnceTheSlashIsGone(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/")
	m.refreshMenu()
	m.menu.dismissed = true

	m.prompt.SetValue("hello")
	m.refreshMenu()

	if m.menu.open() {
		t.Error("menu stayed open once the line no longer started with '/'")
	}
	if m.menu.dismissed {
		t.Error("dismissed survived the line that cleared it")
	}
}

func TestRefreshMenuNarrowsAsMoreIsTyped(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/cl")

	m.refreshMenu()

	if len(m.menu.filtered) != 1 || m.menu.filtered[0].value != "/clear" {
		t.Errorf("filtered = %+v, want exactly /clear", m.menu.filtered)
	}
}

func TestNavigateMenuMovesSelectionWithinBounds(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/")
	m.refreshMenu()

	m.navigateMenu(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.menu.selected != 0 {
		t.Errorf("selected = %d after up at the top, want it to stay at 0", m.menu.selected)
	}

	for range m.menu.filtered {
		m.navigateMenu(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.menu.selected != len(m.menu.filtered)-1 {
		t.Errorf("selected = %d after running past the bottom, want it to stop at %d", m.menu.selected, len(m.menu.filtered)-1)
	}
}

// tab picking a command has to leave the run alone — the whole reason enter
// inside the dropdown does not double as ask()'s enter.
func TestNavigateMenuTabSelectsWithoutStartingARun(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/cl")
	m.refreshMenu()

	handled, _ := m.navigateMenu(tea.KeyPressMsg{Code: tea.KeyTab})

	if !handled {
		t.Fatal("navigateMenu did not claim tab while the menu was open")
	}
	if got := m.prompt.Value(); got != "/clear " {
		t.Errorf("prompt = %q, want the picked command plus a trailing space", got)
	}
	if m.run.busy {
		t.Error("selecting from the menu started a run")
	}
	if m.menu.open() {
		t.Error("the menu stayed open after a selection")
	}
}

func TestNavigateMenuEscDismissesWithoutChangingText(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/cl")
	m.refreshMenu()

	handled, _ := m.navigateMenu(tea.KeyPressMsg{Code: tea.KeyEscape})

	if !handled {
		t.Fatal("navigateMenu did not claim esc while the menu was open")
	}
	if got := m.prompt.Value(); got != "/cl" {
		t.Errorf("prompt = %q, want esc to leave the typed text alone", got)
	}
	if m.menu.open() {
		t.Error("the menu stayed open after esc")
	}
}

// An ordinary character is exactly what navigateMenu must not claim — it is
// the prompt's own filter that has to see it, by falling all the way
// through key() to prompt.Update.
func TestNavigateMenuDoesNotClaimAnOrdinaryCharacter(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/cl")
	m.refreshMenu()

	handled, _ := m.navigateMenu(tea.KeyPressMsg{Code: 'e'})

	if handled {
		t.Error("navigateMenu claimed an ordinary character")
	}
}

// The exact bug live-verifying this turned up: a row built without regard
// to the terminal's real width wrapped in tmux's 63-column pane, which
// broke every line-counting assumption elsewhere in this file at once.
func TestMenuRowNeverExceedsWidth(t *testing.T) {
	it := menuItem{value: "/skill:antenne", description: strings.Repeat("x", 200)}

	for _, width := range []int{20, 40, 63, 80} {
		if got := menuRow(it, width); len(got) > width {
			t.Errorf("menuRow(width=%d) = %q (%d chars), want it to fit", width, got, len(got))
		}
	}
}

func TestMenuRowDropsTheDescriptionRatherThanOverflowOnATinyWidth(t *testing.T) {
	it := menuItem{value: "/skill:facile-review", description: "a description"}

	if got := menuRow(it, 10); got != it.value {
		t.Errorf("menuRow(width=10) = %q, want just the value with no room for a description", got)
	}
}

func TestViewMenuIsEmptyWhenClosed(t *testing.T) {
	m := sized()
	if got := m.viewMenu(); got != "" {
		t.Errorf("viewMenu() = %q, want empty with nothing typed", got)
	}
}

func TestViewMenuListsEveryVisibleMatch(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/")
	m.refreshMenu()

	got := m.viewMenu()

	for _, want := range []string{"/clear", "/help", "/quit"} {
		if !strings.Contains(got, want) {
			t.Errorf("viewMenu() = %q, want it to mention %q", got, want)
		}
	}
}

// key() is where up/down actually get routed — this is the one place that
// proves the menu wins over the scroller while it's open, not only that
// navigateMenu itself works in isolation.
func TestKeyRoutesUpDownToTheMenuInsteadOfScrollingWhileItIsOpen(t *testing.T) {
	m := sized()
	m.prompt.SetValue("/")
	m.refreshMenu()
	before := m.viewport.YOffset()

	m.key(tea.KeyPressMsg{Code: tea.KeyDown})

	if m.menu.selected == 0 {
		t.Error("selected did not move, want key() to have routed down to the menu")
	}
	if m.viewport.YOffset() != before {
		t.Error("the transcript scrolled, want down claimed by the open menu instead")
	}
}
