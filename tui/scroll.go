package main

import (
	tea "charm.land/bubbletea/v2"
)

// scroll moves the transcript under the reader, reporting whether the press
// was one of its own.
//
// The viewport is driven by hand rather than handed the key, because its own
// bindings are written for a pager that owns the keyboard: j, k, f, b, u, d
// and space all scroll, and the prompt below is focused, so forwarding a press
// would mean half the alphabet moved the transcript instead of reaching the
// question being typed.
//
// What is left are the four keys a prompt has no use for on its own. Up and
// down do double duty: key() only ever reaches scroll with them when the
// dropdown menu (menu.go) is closed, since an open menu claims both first to
// move its own selection — so scroll never actually has to choose between
// the two, it only ever sees the keys the menu left it. Home, end, ctrl+u
// and ctrl+d are all editing keys — taking those would fix scrolling by
// breaking typing.
func (m *model) scroll(press tea.KeyPressMsg) bool {
	switch press.String() {
	case "up":
		m.viewport.ScrollUp(1)
	case "down":
		m.viewport.ScrollDown(1)
	case "pgup":
		m.viewport.PageUp()
	case "pgdown":
		m.viewport.PageDown()
	default:
		return false
	}
	return true
}

// wheel scrolls the transcript by mouse.
//
// This one is forwarded to the viewport rather than driven by hand, which
// looks like the opposite of what scroll does and rests on the same reason:
// the objection to forwarding was never the viewport's scrolling, it was its
// keyboard. A wheel event collides with nothing the prompt wants, and
// viewport.New already sets the three-line delta every other terminal
// application scrolls by, so restating it here would only be a copy that can
// drift.
//
// None of this arrives unasked. View has to put the terminal in a mouse mode
// before a wheel message exists at all, and until it did, the wheel did
// nothing in this client: the alternate screen has no scrollback of its own to
// fall through to, so an unasked-for wheel is not degraded scrolling, it is
// none. See View for what asking costs.
func (m *model) wheel(press tea.MouseWheelMsg) tea.Cmd {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(press)
	return cmd
}
