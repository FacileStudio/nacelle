package main

import (
	tea "charm.land/bubbletea/v2"
)

// screen is what this client knows about the terminal it is drawing into:
// how wide, how tall, and how many of those rows the live region may use for
// a run's streaming output once the status line, the prompt, the queue and
// the dropdown have taken theirs.
//
// liveRows exists because the live region is repainted in place on every
// delta, so it has to fit on the screen. Content taller than the terminal
// cannot be redrawn where it stands, and an inline program that tries is one
// that corrupts its own output.
//
// frameRows is the other half of that same rule, and it is the half that was
// missing. tea.Println does not append to the terminal: it scrolls the screen
// up by however many rows it is about to write and then inserts them above the
// frame. Rows the frame itself is standing on get scrolled away with
// everything else, and the renderer never draws them again — they are left in
// the scrollback as a second, unrendered copy of whatever was in the live
// region, with a duplicate prompt underneath it. So the frame is not allowed
// to fill the window, and what it leaves free is all a single Println may
// write.
//
// It is how tall View last drew, recorded rather than predicted. Predicting it
// from liveRows and the furniture is wrong in exactly one case, and it is a
// reachable one: sending a question empties a tall prompt, so layout hands
// those rows back to the live region and the predicted frame stays put — while
// the screen still shows the tall prompt, because the frame that will replace
// it has not been flushed yet. Measuring the last frame is right in that case
// and no worse in any other: the live region only ever grows during a run, so
// a stale figure is a smaller budget rather than a false one.
type screen struct {
	width        int
	windowHeight int
	liveRows     int
	frameRows    int
}

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
// The 2 is the status line and the blank row kept above it, the only rows
// that are always exactly one each — see View for why the blank one earns its
// place. The
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
//
// Left over, but not all of it. Half the window is held back for printing,
// because tea.Println makes room for what it writes by scrolling the screen,
// and a frame standing on every row of the window is a frame that gets
// scrolled into the scrollback and left there — see screen's own doc comment
// for what that looks like from the reader's chair. Half is a split rather
// than a measurement: printing is cut into pieces that fit whatever is
// reserved, so the number decides how many pieces a long answer takes, never
// whether it survives. The live region is a preview of raw text about to be
// reprinted properly a second later, and it has no claim on the whole window.
//
// What this does not do is decide how much may be printed. That is measured
// from the frame View actually drew — see screen — because this function is
// called before the frame it describes reaches the screen, and the gap between
// the two is exactly where a print goes wrong.
func (m *model) layout(height int) {
	taken := 2 + m.prompt.Height() + m.menu.height() + m.queuedHeight()
	m.liveRows = max(height-taken-height/2, 1)
}
