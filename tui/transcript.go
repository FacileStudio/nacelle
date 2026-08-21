package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// speaker is who a line on screen belongs to, which is all the drawing needs
// to know about it.
type speaker int

const (
	// fromClient is the client talking about itself: the banner, and what it
	// has to say about its own state.
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

// say commits one finished thing to the terminal's own scrollback.
//
// Nothing is kept. This client used to hold every line in a viewport and
// redraw the lot on each arriving character, which is what made it the only
// thing that could scroll them — and the reason the terminal could not. A
// finished line is printed once, above the live region, and belongs to the
// terminal from then on: its scrollback, its selection, its search, tmux's
// copy-mode. See View for why that is worth more than owning them.
//
// The line is queued rather than printed here because printing is a Cmd, and
// say has callers that cannot return one — absorb folding an event, flush
// committing an answer. Update drains the queue after every message, so the
// order lines are said in is the order they land in.
func (m *model) say(who speaker, text string) {
	m.unprinted = append(m.unprinted, m.paint(who, text))
}

// prints hands everything said since the last message to the terminal, as a
// single Cmd, and forgets it.
//
// One Println for the batch rather than one per line: tea.Batch makes no
// promise about the order its commands run in, and a transcript delivered out
// of order is not a transcript. Joining them first makes the whole batch one
// message, which insertAbove writes in one go.
//
// One message, but not necessarily one Println — see printed, which cuts a
// batch taller than the window has room for into pieces that still arrive in
// order. Joining here and splitting there is deliberate: the split is a
// property of the screen, and nothing about what was said should have to know
// how tall the terminal is.
func (m *model) prints() tea.Cmd {
	if len(m.unprinted) == 0 {
		return nil
	}
	said := strings.Join(m.unprinted, "\n\n")
	m.unprinted = nil
	return m.printed(said)
}

// paint is how one line looks.
//
// Nobody is labelled. A transcript prefixing every line with who said it
// spends the left margin on something the styling already says, and reads
// like a chat log rather than like a session. The reader's own question is
// the thing they scroll back to find, so that is what gets a background; the
// answer is the thing being read, so it gets none, and is rendered as the
// markdown the model almost certainly wrote it in.
//
// Width is taken once, here, at the moment the line is printed. It can never
// be re-taken: the line is in the terminal's scrollback from then on, and a
// resize reflows it the way the terminal reflows everything else rather than
// the way this client would. That is the one thing owning a viewport bought
// that this gives up, and it is worth it — every other tool in the terminal
// behaves this way, including the shell the client was launched from.
func (m *model) paint(who speaker, text string) string {
	width := max(m.width, 1)
	switch who {
	case fromReader:
		return m.theme.question.Width(width).Render(text)
	case fromModel:
		return m.markdown(text)
	case fromThinking:
		return m.theme.thinking.Width(width).Render(text)
	case fromTool:
		return m.theme.tool.Width(width).Render("⏺ " + text)
	case fromResult:
		return m.theme.result.Width(width).Render("  ⤷ " + text)
	case fromFailure:
		return m.theme.failure.Width(width).Render(text)
	default:
		return m.theme.client.Width(width).Render(text)
	}
}

// streaming is what a run has produced but not finished: the reasoning and
// the answer as they arrive, drawn in the live region under everything
// already printed.
//
// It is tailed to the rows the window can spare rather than shown whole. The
// live region is repainted on every delta, so it has to fit on the screen —
// an answer longer than the terminal cannot be redrawn in place at all, and
// trying is how an inline program corrupts its own output. What scrolls off
// the top is not lost: the whole answer is printed, rendered, the moment it
// finishes.
//
// Both are drawn plainly, not as markdown. Half a fenced code block is not a
// document, and a parser run over the answer again on every arriving
// character is a cost this pays once, at the end, instead.
func (m *model) streaming() []string {
	var live []string
	if reasoning := m.run.reasoning.String(); reasoning != "" {
		live = append(live, m.theme.thinking.Width(max(m.width, 1)).Render(reasoning))
	}
	if answer := m.run.answer.String(); answer != "" {
		live = append(live, m.theme.plain.Width(max(m.width, 1)).Render(answer))
	}
	if len(live) == 0 {
		return nil
	}

	lines := strings.Split(strings.Join(live, "\n"), "\n")
	if len(lines) > m.liveRows {
		lines = lines[len(lines)-m.liveRows:]
	}
	return lines
}
