package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// printed hands text to the terminal, cut into pieces the frame can survive.
//
// tea.Println does not append to the screen the way a shell command does. It
// scrolls the screen up by as many rows as it is about to write, and only then
// inserts them above the frame and puts the frame back where it was. Scrolling
// takes whatever is standing on those rows with it, and once the frame's own
// rows are up there the renderer never touches them again: it draws relative
// to where it thinks the frame is, which is where the frame ended up, not
// where its lost rows went. What the reader finds on the way up is the live
// region as it looked mid-stream — the answer in raw markdown, asterisks and
// all — with a second copy of the prompt under it, sitting above the rendered
// answer that arrived a moment later.
//
// The rule is exact and was measured rather than reasoned about: the first row
// of the frame is lost as soon as a batch is one row taller than the window
// has free above it, and one more row goes for every row past that. So a batch
// taller than the free rows is written as several, each inside the budget.
// Nothing is re-rendered between them, so every piece scrolls the same
// distance the first one did, and the frame comes back to the same place —
// which is why cutting the batch works at all and why it costs only the extra
// escape sequences.
//
// A single Println for a batch that fits, rather than a sequence of one: order
// is the whole reason this path exists, and there is nothing to order.
func (m *model) printed(text string) tea.Cmd {
	lines := strings.Split(text, "\n")

	var cmds []tea.Cmd
	for len(lines) > 0 {
		take := m.fits(lines)
		cmds = append(cmds, tea.Println(strings.Join(lines[:take], "\n")))
		lines = lines[take:]
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Sequence(cmds...)
}

// fits is how many of lines the terminal can take in one go: as many as scroll
// the screen by no more than the rows the frame left free.
//
// Never fewer than one. A single line too tall for the whole budget — a
// wrapped code block in a window with a tall prompt — still has to be printed,
// and the frame losing a row is a better outcome than a client that stops
// saying anything.
func (m *model) fits(lines []string) int {
	budget := m.budget()
	rows, taken := 0, 0
	for _, line := range lines {
		rows += m.scrolls(line)
		if rows > budget && taken > 0 {
			break
		}
		taken++
	}
	return taken
}

// budget is how many rows a single Println may scroll the screen by: the
// window, less the frame standing at the bottom of it.
//
// One at the least, because a batch has to go somewhere even in a window with
// no room in it, and because a budget of zero is a loop that never advances.
func (m *model) budget() int {
	return max(m.windowHeight-m.frameRows, 1)
}

// scrolls is how far one printed line moves the screen: its own row, plus one
// for every whole width it overruns.
//
// This is bubbletea's arithmetic, copied rather than derived, and the copy is
// the point. A line of exactly twice the width is counted as three rows there
// and wraps onto two here, but it is that count, not the truth, that decides
// how many newlines get written and therefore how far the screen actually
// moves. A more accurate answer here would be the wrong one.
func (m *model) scrolls(line string) int {
	width := max(m.width, 1)
	if cells := ansi.StringWidth(line); cells > width {
		return 1 + cells/width
	}
	return 1
}
