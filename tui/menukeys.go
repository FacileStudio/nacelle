package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// refreshMenu recomputes what the dropdown shows from the prompt's current
// value. Called after every edit reaches the prompt, not only from a key
// that typed a character — a paste, or /clear's own prompt.Reset(), change
// the value just as much and would otherwise leave the menu showing
// whatever it last matched.
//
// layout() runs here too, not only from resize(): the dropdown opening,
// closing, or simply matching a different number of items changes how much
// height the transcript gets, and no WindowSizeMsg arrives to trigger that
// on its own.
func (m *model) refreshMenu() {
	value := m.prompt.Value()
	if !strings.HasPrefix(value, "/") {
		m.menu.filtered = nil
		m.menu.dismissed = false
	} else {
		m.menu.filtered = filterMenu(m.menu.items, value)
		if typedOut(m.menu.filtered, value) {
			m.menu.filtered = nil
		}
		if m.menu.selected >= len(m.menu.filtered) {
			m.menu.selected = 0
		}
	}
	m.layout(m.windowHeight)
}

// navigateMenu handles a keypress while the dropdown is open, reporting
// whether it consumed the press — everything it does not claim (typing a
// character, backspace) falls through to the prompt as normal, which is
// what keeps the filter itself editable while the menu is up.
//
// tab/enter and esc both close the dropdown, which changes menu.height() —
// and unlike an ordinary character, neither one reaches prompt.Update, the
// path that would otherwise call refreshMenu and its layout() for free. Skip
// layout() here and the viewport stays sized for the dropdown that just
// closed, short of the terminal's real bottom, which is exactly what made
// the prompt look like it "jumped up" after picking an item.
func (m *model) navigateMenu(press tea.KeyPressMsg) (bool, tea.Cmd) {
	switch press.String() {
	case "up":
		m.menu.selected = max(m.menu.selected-1, 0)
	case "down":
		m.menu.selected = min(m.menu.selected+1, len(m.menu.filtered)-1)
	case "tab", "enter":
		m.selectMenuItem()
	case "esc":
		m.menu.dismissed = true
	default:
		return false, nil
	}
	m.layout(m.windowHeight)
	return true, nil
}

// selectMenuItem fills the prompt with the highlighted entry and a trailing
// space, ready for an argument, and closes the dropdown. It does not submit
// — a /skill:name most often takes one, and enter inside the dropdown has
// to mean "pick this," not "send it with nothing typed after it yet." A
// second, ordinary enter — once there is nothing left to pick from — is
// what actually starts the run.
func (m *model) selectMenuItem() {
	if m.menu.selected >= len(m.menu.filtered) {
		return
	}
	m.prompt.SetValue(m.menu.filtered[m.menu.selected].value + " ")
	m.prompt.CursorEnd()
	m.menu.dismissed = true
}

// viewMenu draws the dropdown, or "" when it has nothing to show — every
// keystroke that does not start a line with '/' would otherwise insert a
// blank line into View() for no reason.
//
// The selected row highlights via theme.question, unpadded: that style
// carries Padding(0, 1) for its other job, the transcript's quoted-question
// pill, and left as-is here it shifted only the selected row's text one
// column right of every unselected row's, which render through theme.plain
// and have no padding at all.
func (m *model) viewMenu() string {
	if !m.menu.open() {
		return ""
	}
	items := m.menu.filtered[:m.menu.height()]
	width := max(m.width, 1)

	rows := make([]string, len(items))
	for i, it := range items {
		style := m.theme.plain
		if i == m.menu.selected {
			style = m.theme.question.Padding(0, 0)
		}
		rows[i] = style.Width(width).Render(menuRow(it, width))
	}
	return strings.Join(rows, "\n")
}

// truncationSuffix is what truncate (approve.go) appends when it actually
// cuts something — 3 bytes, not 1, since "…" is multi-byte UTF-8. Named
// here so menuRow's own budget math reads as what it is instead of a bare
// "- 3" nobody can trace back to why.
const truncationSuffix = "…"

// menuRow is one dropdown line, fit to width: the value always shows in
// full — truncating a command or skill's own name would make it useless to
// read — and the description, only when there is room left worth using for
// one, fills the rest. A row this does not fit inside width, on a narrow
// enough terminal, is one line too many everywhere else in this file
// assumes it is not: height(), layout() and the cursor math in View() all
// count one line per item.
func menuRow(it menuItem, width int) string {
	if it.description == "" {
		return it.value
	}
	const separator = "  "
	budget := width - len(it.value) - len(separator) - len(truncationSuffix)
	if budget < 10 {
		return it.value
	}
	return it.value + separator + truncate(it.description, budget)
}
