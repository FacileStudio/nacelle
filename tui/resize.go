package main

import (
	tea "charm.land/bubbletea/v2"
)

// resize records the terminal's new shape and re-measures what still fits in
// it. windowHeight is remembered here because it is the one input layout()
// needs that nothing but a WindowSizeMsg ever reports — refreshMenu calls
// layout again, on the same remembered height, whenever the menu's own size
// changes with no resize involved at all.
//
// Only the prompt is told the new width. Everything already said belongs to
// the terminal's scrollback now, and the terminal reflows it the way it
// reflows the shell's output and everything else up there; this client cannot
// reach back into lines it no longer owns, and should not want to.
//
// restyle only runs when the width actually changed. It rebuilds the markdown
// renderer, which a resize that only touches height does not need — and a
// resize-drag sends a burst of those.
func (m *model) resize(size tea.WindowSizeMsg) tea.Cmd {
	widthChanged := size.Width != m.width
	m.width, m.windowHeight = size.Width, size.Height

	m.prompt.SetWidth(size.Width)
	m.prompt.MaxHeight = promptCap(size.Height)
	m.prompt.SetHeight(m.prompt.Height())
	m.layout(size.Height)

	if widthChanged {
		m.restyle()
	}
	return nil
}

// layout is the one place the live region's height is computed, so a resize,
// the dropdown opening or closing, and a message being queued or delivered
// can never disagree about how tall it is.
//
// The 1 is the status line, the only row that is always exactly one. The
// prompt is asked how tall it is rather than assumed to be one row: it grows
// with what has been typed, so a long question takes rows from the live
// region while it is being written and gives them back when it is sent.
// Everything after that is a row only sometimes on screen, reserved by the
// same rule — one row per line View will actually draw. Queued messages are
// one line each because viewQueued truncates them to the width rather than
// letting them wrap.
//
// What is left over is how much of a streaming answer can be shown while it
// is still arriving. That is a preview and nothing depends on it: the whole
// answer is printed, rendered, the moment the run commits it.
func (m *model) layout(height int) {
	m.liveRows = max(height-1-m.prompt.Height()-m.menu.height()-m.queuedHeight(), 1)
}
