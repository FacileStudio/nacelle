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
// Not returning that Cmd is what breaks the loop, so a run that has finished
// stops ticking on its very next frame rather than spinning, unseen, until the
// process exits.
//
// It ticks for as long as the run is busy, not only until the first event
// arrives. That is the whole of the fix for a client that looked hung between
// tool calls, and the guard is the only place it had to change: the frames
// were always being drawn, they just stopped being asked for.
//
// Nothing is re-rendered here. The spinner is drawn by status(), which View
// rebuilds on every message anyway, so the viewport it used to be part of has
// no reason to be rebuilt on a frame it no longer holds.
func (m *model) spun(message spinner.TickMsg) tea.Cmd {
	var cmd tea.Cmd
	m.spin, cmd = m.spin.Update(message)
	if !m.run.busy {
		return nil
	}
	return cmd
}
