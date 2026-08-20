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
// What is left are the four keys a prompt has no use for. Up and down stay
// claimed here even though the prompt now sets a suggestion list (the
// "/"-command names): that list's own up/down binding is only a second way
// to cycle a multi-match suggestion, and its other binding for the same
// action, ctrl+n/ctrl+p, reaches the prompt untouched — nothing is lost by
// leaving the transcript on the keys everyone reaches for first. Home, end,
// ctrl+u and ctrl+d are all editing keys — taking those would fix scrolling
// by breaking typing.
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
