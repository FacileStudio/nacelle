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

// viewQueued is one line per message waiting on the run in flight, drawn
// between the status line and the prompt.
//
// They are deliberately not in the transcript. A transcript is what was
// actually said, in the order it was said, and a question that has not been
// asked yet is neither — parked above a half-finished answer it would read as
// already sent, which is the one thing it must not.
//
// Each is truncated to the width rather than allowed to wrap, because
// layout() reserves exactly one row per queued message. A wrapped line here
// would push the prompt off the bottom of the screen, which is the same shape
// of bug the dropdown's own rows already had once.
func (m *model) viewQueued() []string {
	lines := make([]string, 0, len(m.run.queued))
	for _, text := range m.run.queued {
		lines = append(lines, m.theme.waiting.Render(truncate("queued · "+text, max(m.viewport.Width(), 1))))
	}
	return lines
}
