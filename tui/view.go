package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/FacileStudio/nacelle"
)

// View draws only what is still changing: whatever a run is streaming right
// now, one status line, any queued messages, the dropdown menu when it has
// something to show, and the prompt. Everything finished has already been
// printed and belongs to the terminal.
//
// There is no alternate screen and no mouse mode, and dropping both is the
// point rather than an omission. An alt-screen program is handed a blank page
// with no scrollback of its own, so it has to own scrolling, which means
// capturing the wheel, which means taking click-drag selection too, which
// means tmux's copy-mode reaches nothing and quitting un-draws the whole
// session. Every one of those was reported here as its own separate
// complaint, and every one of them is the same decision. Giving the page back
// answers all of them at once: the terminal scrolls, selects, searches and
// keeps the session, because the session is ordinary terminal output again.
//
// What it costs is reflow. A printed line is the terminal's, so a resize
// rewraps it the way the terminal rewraps everything else rather than the way
// this client would have. That is the trade every other tool in the terminal
// already makes, including the shell this was launched from.
//
// The blank row above the status line is deliberate. Everything finished has
// been printed already, so without it the status sits hard against the last
// line of the answer and reads as part of it — a token count apparently
// belonging to the sentence above. One empty row is what separates the client
// talking about the run from the run's own output.
//
// The one thing recorded on the way out is how tall the frame came to be.
// Nothing else knows: layout works out what the frame is allowed to be, which
// is a different number the moment the live region is not full, and it runs
// before its own frame reaches the screen. Printing needs the frame that is
// actually up there, so this is where it is taken — see screen and printed.
//
// The cursor is positioned by hand because the prompt renders inside a larger
// frame: the component reports where its cursor sits within itself, and only
// the caller knows how many rows are above it. above is exactly those rows —
// computed once and reused for both the body and the cursor offset, so the
// two can never disagree about how tall the menu drew this frame.
func (m *model) View() tea.View {
	above := append(m.streaming(), "", m.status())
	above = append(above, m.viewQueued()...)
	if menu := m.viewMenu(); menu != "" {
		above = append(above, menu)
	}
	body := strings.Join(append(above, m.prompt.View()), "\n")
	m.frameRows = lipgloss.Height(body)

	view := tea.NewView(body)
	if position := m.prompt.Cursor(); position != nil {
		position.Y += lipgloss.Height(strings.Join(above, "\n"))
		view.Cursor = position
	}
	return view
}

// working is what a busy run is doing, spun so the line moves even when
// nothing else on screen does.
//
// The spinner lives on the status line because that is a row this client
// still owns. It covered only the gap before the first event once: after that
// the screen went still for every gap that followed — a tool running, the
// model called again with its result — and a client that has stopped moving
// is indistinguishable from one that has stopped working.
//
// Naming the tool is the difference between knowing something is happening
// and knowing what. A run_command waiting on a slow build and a wedged client
// look identical without it, and this client's own timeout is measured in
// minutes.
func (m *model) working() string {
	doing := "waiting for a response"
	switch len(m.run.running) {
	case 0:
	case 1:
		for _, name := range m.run.running {
			doing = "running " + name
		}
	default:
		doing = fmt.Sprintf("running %d tools", len(m.run.running))
	}
	return m.spin.View() + " " + doing
}

// absorb folds one event into what is on screen.
//
// Text accumulates into the answer being streamed rather than becoming a line
// of its own: the deltas arrive a few characters at a time, and a transcript
// of those is unreadable.
//
// Reasoning accumulates separately. It is not part of the answer: the two
// written into one buffer come out concatenated with no separator, and that
// concatenation is what would go back as the assistant's message on every
// later turn, putting a chain of thought in the one field no provider wants it
// replayed in.
func (m *model) absorb(event nacelle.Event) {
	switch event.Kind {
	case nacelle.KindText:
		m.run.answer.WriteString(event.Text)
	case nacelle.KindThinking:
		m.run.reasoning.WriteString(event.Text)
	case nacelle.KindToolCall:
		m.run.running[event.Tool.ID] = event.Tool.Name
		m.say(fromTool, fmt.Sprintf("%s %s", event.Tool.Name, event.Tool.Input))
	case nacelle.KindToolResult:
		delete(m.run.running, event.Tool.ID)
		m.say(fromResult, describe(event.Tool))
	case nacelle.KindTurn:
		m.run.usage = m.run.usage.Add(event.Usage)
		m.sized(event.Usage)
	case nacelle.KindDone:
		m.run.usage = event.Usage
		m.run.stop = event.Stop
		m.sized(event.Usage)
	}
}

// cutShort is how a run that stopped short reads, and the empty string for one
// that finished.
//
// A truncated, refused or abandoned answer arrives as a well-formed stream
// that simply ends, so the screen shows a paragraph stopping mid-sentence and
// a status line saying "ready". Saying which of them happened, in words rather
// than in the wire's vocabulary, is the whole point: the person reading has to
// know whether to ask again or to ask differently.
func cutShort(stop nacelle.Stop) string {
	if stop == "" || stop.Complete() {
		return ""
	}
	switch stop {
	case nacelle.StopMaxTokens:
		return "cut off at the token limit"
	case nacelle.StopContext:
		return "cut off: out of context"
	case nacelle.StopRefusal:
		return "refused by the model"
	case nacelle.StopIterations:
		return "stopped at the iteration limit"
	case abandoned:
		return "abandoned"
	}
	return "stopped early"
}

// describe is what a finished tool call reads as. A failure is reported rather
// than hidden, because the model is about to be told the same thing and the
// person watching should see what it sees.
func describe(tool *nacelle.ToolEvent) string {
	if tool.Err != nil {
		return fmt.Sprintf("%s failed after %s: %v", tool.Name, tool.Duration.Round(time.Millisecond), tool.Err)
	}
	return fmt.Sprintf("%s done in %s", tool.Name, tool.Duration.Round(time.Millisecond))
}

// flush moves whatever is still streaming into the transcript, and reports the
// answer it committed so the caller can put that same text in the conversation.
//
// It is not bookkeeping. Both buffers are drawn by render only while they are
// filling, so clearing one without moving its text somewhere permanent erases
// it from the screen at the exact moment it finished — which is what this did
// until it was used.
//
// Reasoning is shown and then dropped rather than recorded. nacelle.Reasoning
// exists and would hold it, but what this buffer holds is the readable copy
// and not the one a provider will take back. Within a run the backend already
// carries the real thing on the assistant message it rebuilds, in the shape
// the provider signs and validates; across runs no provider wants a chain of
// thought from an answer it has already given, and both backends drop the part
// on the way out. Recording it here would fill the conversation with something
// that is only ever skipped.
func (m *model) flush() string {
	if reasoning := m.run.reasoning.String(); reasoning != "" {
		m.run.reasoning.Reset()
		m.say(fromThinking, reasoning)
	}

	answer := m.run.answer.String()
	m.run.answer.Reset()
	if answer != "" {
		m.say(fromModel, answer)
	}
	return answer
}
