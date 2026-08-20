package main

import (
	tea "charm.land/bubbletea/v2"
)

// resize gives the prompt one line, the status one, the dropdown menu its
// own when open, and the transcript the rest. windowHeight is remembered
// here because it is the one input layout() needs that nothing but a
// WindowSizeMsg ever reports — refreshMenu calls layout again, on the same
// remembered height, whenever the menu's own size changes with no resize
// involved at all.
//
// restyle only runs when the width actually changed. It rebuilds the markdown
// renderer and re-renders every entry in the transcript, which a resize that
// only touches height does not need — and a resize-drag burst sends many of
// those. render() still runs either way, which is the cheap half and the one
// that keeps the pinned-to-bottom state correct.
func (m *model) resize(size tea.WindowSizeMsg) tea.Cmd {
	widthChanged := size.Width != m.viewport.Width()
	m.windowHeight = size.Height

	m.viewport.SetWidth(size.Width)
	m.prompt.SetWidth(size.Width)
	m.prompt.MaxHeight = promptCap(size.Height)
	m.prompt.SetHeight(m.prompt.Height())
	m.layout(size.Height)

	if widthChanged {
		m.restyle()
	} else {
		m.render()
	}
	return nil
}

// layout is the one place the transcript's height is computed, so a resize,
// the dropdown opening or closing, and a message being queued or delivered
// can never disagree about how tall it is.
//
// The 1 is the status line, the only row that is always exactly one. The
// prompt is asked how tall it is rather than assumed to be one row: it grows
// with what has been typed, so a long question takes rows from the transcript
// while it is being written and gives them back when it is sent. Everything
// after that is a row only sometimes on screen, reserved by the same rule —
// one row per line View will actually draw. Queued messages are one line each
// because viewQueued truncates them to the width rather than letting them
// wrap.
// A reader already at the end is kept there, the same rule render follows.
// Shortening the viewport leaves its offset alone, so the last line slides
// out of sight and the status line starts claiming the transcript is scrolled
// back — which typing a second line into the prompt should not do, and which
// nothing but this would put right.
func (m *model) layout(height int) {
	follow := m.viewport.AtBottom()
	m.viewport.SetHeight(max(height-1-m.prompt.Height()-m.menu.height()-m.queuedHeight(), 1))
	if follow {
		m.viewport.GotoBottom()
	}
}
