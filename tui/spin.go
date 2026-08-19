package main

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// spun advances the spinner one frame, and stops it once there is nothing
// left to spin for.
//
// The library's own Update always returns a Cmd that re-arms the next tick —
// there is no separate stop call, only declining to ask for the next frame.
// Not returning that Cmd is what breaks the loop, so a run that has heard back
// stops ticking on its very next frame rather than spinning, unseen, until the
// process exits.
func (m *model) spun(message spinner.TickMsg) tea.Cmd {
	var cmd tea.Cmd
	m.spin, cmd = m.spin.Update(message)
	if !m.run.waiting {
		return nil
	}
	m.render()
	return cmd
}
