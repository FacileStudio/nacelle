package main

import (
	"fmt"
)

// dropQueued forgets whatever was typed while a run was going, and says so.
//
// Stopping a run has to stop everything the run would have led to. Left
// alone, the queue is delivered by settle — which cancelling reaches, the
// same as any other ending — so ctrl+c would abandon one run and immediately
// start the next, which is the opposite of what it was pressed for.
//
// Dropping silently would be its own trap: the queued lines leave the screen
// either way, and the difference between "delivered" and "thrown away" is
// exactly what the person who typed them needs to know.
func (m *model) dropQueued() {
	if len(m.run.queued) == 0 {
		return
	}
	m.say(fromClient, fmt.Sprintf("%s dropped, not sent", countedNoun(len(m.run.queued), "queued message")))
	m.run.queued = nil
	m.layout(m.windowHeight)
}

// queuedRows is how many waiting messages are listed before the rest are
// counted in one line instead.
//
// Without a ceiling the list is as tall as the queue, and layout reserves a
// row for every one of them — so a reader who queued thirty questions during
// a long run got a transcript squeezed to a single row and a prompt pushed
// off the bottom of the screen entirely. The queue is a reminder of what is
// waiting, not a second transcript, and three of them plus a count says
// everything the full list would.
const queuedRows = 3

// queuedHeight is how many rows viewQueued draws. layout reserves exactly
// this many, so the two must never be able to disagree — which is why it is
// this function and not len(queued) at both call sites.
func (m *model) queuedHeight() int {
	if len(m.run.queued) <= queuedRows {
		return len(m.run.queued)
	}
	return queuedRows + 1
}

// viewQueued is the messages waiting on the run in flight, drawn between the
// status line and the prompt.
//
// They are deliberately not in the transcript. A transcript is what was
// actually said, in the order it was said, and a question that has not been
// asked yet is neither — parked above a half-finished answer it would read as
// already sent, which is the one thing it must not.
//
// Each is truncated to the width rather than allowed to wrap, because layout
// reserves exactly one row per line drawn here. A wrapped line would push the
// prompt off the bottom, the same shape of bug the dropdown's own rows had
// once.
func (m *model) viewQueued() []string {
	shown, hidden := m.run.queued, 0
	if len(shown) > queuedRows {
		shown, hidden = shown[:queuedRows], len(shown)-queuedRows
	}

	width := max(m.width, 1)
	lines := make([]string, 0, m.queuedHeight())
	for _, text := range shown {
		lines = append(lines, m.theme.waiting.Render(truncate("queued · "+text, width)))
	}
	if hidden > 0 {
		lines = append(lines, m.theme.waiting.Render(truncate(fmt.Sprintf("queued · and %d more", hidden), width)))
	}
	return lines
}
