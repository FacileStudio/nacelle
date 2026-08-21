package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ask takes whatever is in the prompt: sent now, or queued behind the run
// already going and delivered when it settles.
//
// The prompt is echoed once, up front, for every non-empty line — a
// command's own reply and a /skill:name's expanded question would otherwise
// need their own echo, the way /clear already had to work around wiping its
// own before this existed.
//
// Sending is also the one thing that ends being scrolled back, and it has to
// be, because render only follows a reader already at the bottom. Without
// this, asking a question while parked halfway up put both the echo of it and
// the whole answer off-screen: the visible result of pressing enter was the
// prompt going empty and nothing else, which reads as the client having
// dropped the question. Scrolling away says "let me read"; sending says
// "I'm done reading", and only the second one is worth guessing at.
func (m *model) ask() tea.Cmd {
	question := strings.TrimSpace(m.prompt.Value())
	if question == "" {
		return nil
	}
	m.prompt.Reset()

	if m.run.busy {
		m.run.queued = append(m.run.queued, question)
		m.layout(m.windowHeight)
		return nil
	}
	m.layout(m.windowHeight)
	return m.dispatch(question)
}

// dispatch echoes one line and routes it, as one of this client's own
// commands or as a question for the model.
//
// It is separate from ask because it has two callers that agree on
// everything after the prompt itself: a line typed now, and a line typed
// while the last run was still going and delivered when it settled. A queued
// "/help" is still a command, which it would not be if the queue fed send
// directly.
//
// It closes over its own lines the way Update closes over a message's, and for
// the same reason spelled out there — say, do, then hand over what was said
// before running what it started. One drain at Update alone is not enough
// because deliver dispatches several lines inside a single routed message: it
// would collect every echo the loop said, print the lot ahead of all their
// commands, and leave the last line's echo above the blank run that a "/clear"
// queued in front of it scrolls away — losing the echo of the very question
// being asked. A command's own reply has the mirror fault, landing above the
// input it answers. Binding each line's output to that line's command is what
// keeps a queue of them in the order they were typed.
func (m *model) dispatch(line string) tea.Cmd {
	m.say(fromReader, line)

	started := tea.Cmd(nil)
	if cmd, ok := m.parseCommand(line); ok {
		started = cmd(m)
	} else {
		started = m.send(line)
	}
	return tea.Sequence(m.prints(), started)
}
