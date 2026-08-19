package main

import (
	"strings"
)

// speaker is who an entry on screen belongs to, which is all the drawing needs
// to know about it.
type speaker int

const (
	// fromClient is the client talking about itself, which is the banner and
	// nothing else so far.
	fromClient speaker = iota

	// fromReader is the question that was typed.
	fromReader

	// fromModel is the answer.
	fromModel

	// fromThinking is the model's reasoning.
	fromThinking

	// fromTool is a tool the model asked for.
	fromTool

	// fromResult is that tool having finished.
	fromResult

	// fromFailure is the run falling over.
	fromFailure
)

// entry is one thing on screen: what it is, what it said, and how it looked the
// last time the window was this wide.
//
// The drawn form is kept rather than produced on demand because it is expensive
// — an answer goes through a markdown renderer — and because it is produced far
// less often than it is used. Every arriving character redraws the whole
// transcript, and re-rendering forty entries per keystroke of the model's is
// what makes a terminal feel slow.
type entry struct {
	who   speaker
	text  string
	drawn string
}

// say commits one entry to the transcript, drawn at the current width.
func (m *model) say(who speaker, text string) {
	m.transcript = append(m.transcript, m.paint(entry{who: who, text: text}))
	m.render()
}

// redraw renders every entry again, which is what a resize and a change of
// terminal theme both cost. It is the only place the whole transcript is
// re-rendered, and neither of those happens per keystroke.
func (m *model) redraw() {
	for i := range m.transcript {
		m.transcript[i] = m.paint(m.transcript[i])
	}
}

// paint fills in how an entry looks.
//
// Nobody is labelled. A transcript prefixing every line with who said it spends
// the left margin on something the styling already says, and reads like a chat
// log rather than like a session. The reader's own question is the thing they
// scroll back to find, so that is what gets a background; the answer is the
// thing being read, so it gets none, and is rendered as the markdown the model
// almost certainly wrote it in.
func (m *model) paint(e entry) entry {
	width := max(m.viewport.Width(), 1)
	switch e.who {
	case fromReader:
		e.drawn = m.theme.question.Width(width).Render(e.text)
	case fromModel:
		e.drawn = m.markdown(e.text)
	case fromThinking:
		e.drawn = m.theme.thinking.Width(width).Render(e.text)
	case fromTool:
		e.drawn = m.theme.tool.Width(width).Render("⏺ " + e.text)
	case fromResult:
		e.drawn = m.theme.result.Width(width).Render("  ⤷ " + e.text)
	case fromFailure:
		e.drawn = m.theme.failure.Width(width).Render(e.text)
	default:
		e.drawn = m.theme.client.Width(width).Render(e.text)
	}
	return e
}

// render redraws the transcript, and follows it only while the reader is
// already at the bottom.
//
// Both halves are load-bearing. The viewport does not follow new content on its
// own, so appending without a GotoBottom leaves the stream scrolling out of
// sight; but doing it unconditionally is what made the transcript unscrollable,
// because a run emits text a few characters at a time and every delta yanked
// the reader back down. Following is therefore a state the reader controls
// rather than a thing that happens to them: at the bottom means keep up,
// anywhere else means stay put, and scrolling back down resumes it.
//
// What is still streaming is drawn plainly, not as markdown. Half a fenced code
// block is not a document, and a parser run over the answer again on every
// arriving character is the cost this whole file exists to avoid.
//
// The spinner is drawn here rather than committed as an entry of its own,
// because it never has one: waiting ends on the same event that gives the
// transcript something real to show instead, so the frame drawn one tick ago
// is already stale and is never worth keeping.
func (m *model) render() {
	follow := m.viewport.AtBottom()
	width := max(m.viewport.Width(), 1)

	body := make([]string, 0, len(m.transcript)+2)
	for _, e := range m.transcript {
		body = append(body, e.drawn)
	}
	if m.run.waiting {
		body = append(body, m.theme.waiting.Width(width).Render(m.spin.View()+" waiting for a response"))
	}
	if reasoning := m.run.reasoning.String(); reasoning != "" {
		body = append(body, m.theme.thinking.Width(width).Render(reasoning))
	}
	if streaming := m.run.answer.String(); streaming != "" {
		body = append(body, m.theme.plain.Width(width).Render(streaming))
	}
	m.viewport.SetContent(strings.Join(body, "\n\n"))

	if follow {
		m.viewport.GotoBottom()
	}
}
