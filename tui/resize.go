package main

import (
	tea "charm.land/bubbletea/v2"
)

// resize gives the prompt one line and the status one, and the transcript the
// rest.
//
// restyle only runs when the width actually changed. It rebuilds the markdown
// renderer and re-renders every entry in the transcript, which a resize that
// only touches height does not need — and a resize-drag burst sends many of
// those. render() still runs either way, which is the cheap half and the one
// that keeps the pinned-to-bottom state correct.
func (m *model) resize(size tea.WindowSizeMsg) tea.Cmd {
	widthChanged := size.Width != m.viewport.Width()

	m.viewport.SetWidth(size.Width)
	m.prompt.SetWidth(size.Width)
	m.viewport.SetHeight(max(size.Height-2, 1))

	if widthChanged {
		m.restyle()
	} else {
		m.render()
	}
	return nil
}
