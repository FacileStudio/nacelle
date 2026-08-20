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
	m.layout(size.Height)

	if widthChanged {
		m.restyle()
	} else {
		m.render()
	}
	return nil
}

// layout is the one place the transcript's height is computed, so a resize
// and the dropdown opening or closing can never disagree about how tall it
// is.
func (m *model) layout(height int) {
	m.viewport.SetHeight(max(height-2-m.menu.height(), 1))
}
