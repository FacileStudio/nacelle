package main

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"

	"github.com/FacileStudio/nacelle"
)

// forceQuit is how long the offer to quit outright stays open after the
// first ctrl+c — long enough to read the status line, short enough that a
// second ctrl+c minutes later still means "stop this run", not "quit".
const forceQuit = 3 * time.Second

// model is the whole client: a transcript, a prompt, and at most one run in
// flight.
//
// spent outlives the run it came from — the status line adds the two into a
// session total that only ever goes up.
type model struct {
	agent  *nacelle.Agent
	banner string

	viewport viewport.Model
	prompt   textinput.Model

	transcript   []entry
	conversation []nacelle.Message
	spent        nacelle.Usage

	theme  palette
	pretty *glamour.TermRenderer
	spin   spinner.Model

	run inflight
}

// inflight is the one run this client allows at a time: how to hear from it,
// how to abandon it, what it has produced, and what it has cost.
//
// Reasoning gets a buffer of its own: sharing the answer's put the last
// thought against the first word with no separator, and worse, committed the
// reasoning to the conversation — re-sending a chain of thought every later
// turn that the provider bills for and does not want replayed.
//
// usage is this run alone, because KindTurn accumulates and KindDone
// replaces: a counter carried over from the last run would double-count its
// total against this run's turns.
//
// waiting is true from the moment a question is sent until the first event
// comes back, success or error — otherwise a screen that has not moved since
// the question was echoed looks exactly like a hung client, not a slow one.
//
// pending is set only when -approve-tools is on and a call is waiting on a
// decision — nil otherwise, so status() and key() only change behaviour for
// someone who asked for a gate at all.
type inflight struct {
	results     <-chan result
	cancel      context.CancelFunc
	answer      strings.Builder
	reasoning   strings.Builder
	usage       nacelle.Usage
	stop        nacelle.Stop
	interrupted time.Time
	busy        bool
	waiting     bool
	asked       []nacelle.Part
	answered    []nacelle.Part
	pending     *approvalRequest
}

// newModel builds the client. The banner names the backend and model, so
// which provider is billed is visible before typing, not after it fails.
func newModel(agent *nacelle.Agent, banner string) *model {
	prompt := textinput.New()
	prompt.Placeholder = "Ask something. Ctrl+C to stop or quit, Ctrl+\\ to force it."
	prompt.Prompt = "> "
	prompt.SetVirtualCursor(false)
	suggestCommands(&prompt)
	prompt.Focus()
	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m := &model{
		agent:    agent,
		banner:   banner,
		viewport: view,
		prompt:   prompt,
		theme:    themed(true),
		spin:     spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		run:      inflight{cancel: func() {}},
	}
	m.pretty = prettier(m.theme.markdown, m.viewport.Width())
	m.say(fromClient, banner)
	return m
}

func (m *model) Init() tea.Cmd { return tea.Batch(textinput.Blink, tea.RequestBackgroundColor) }

// Update routes each message to the one place that owns it. The cases stay
// thin on purpose: everything that changes more than one field is a method, so
// this reads as a table of what can happen rather than as the logic itself.
func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		return m, m.resize(message)
	case tea.KeyPressMsg:
		if handled, cmd := m.key(message); handled {
			return m, cmd
		}
	case tea.BackgroundColorMsg:
		m.theme = themed(message.IsDark())
		m.restyle()
		return m, nil
	case spinner.TickMsg:
		return m, m.spun(message)
	case approvalRequest:
		m.run.pending = &message
		m.render()
		return m, nil
	case result:
		return m, m.consume(message)
	case finished:
		return m, m.settle()
	}

	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(message)
	return m, cmd
}

// key handles this client's bindings, reporting whether it consumed the
// press. Anything the scroller does not claim belongs to the prompt.
//
// Ctrl+C cancels a run in flight and only quits when nothing is running, so
// a long answer can be abandoned without losing the session — the terminal
// is in raw mode, so nothing quits on Ctrl+C unless this says so. That needs
// an escape hatch: busy only clears once settle sees the results channel
// close, and a tool wedged on a subprocess never closes it. A second ctrl+c
// inside forceQuit, or ctrl+\ at any time, quits regardless — otherwise the
// only way out of an alt-screen raw-mode terminal is kill -9 from elsewhere.
//
// Both stay live while a tool approval is pending too: a question nobody
// answers must not be a second way to get stuck. See decide's doc comment
// for why cancelling clears run.pending directly instead.
func (m *model) key(press tea.KeyPressMsg) (bool, tea.Cmd) {
	switch press.String() {
	case "ctrl+\\":
		return true, tea.Quit
	case "ctrl+c":
		if !m.run.busy || time.Since(m.run.interrupted) < forceQuit {
			return true, tea.Quit
		}
		m.run.interrupted = time.Now()
		m.run.stop = abandoned
		m.run.pending = nil
		m.run.cancel()
		m.render()
		return true, nil
	}
	if m.run.pending != nil {
		return true, m.decide(press)
	}
	switch press.String() {
	case "enter":
		return true, m.ask()
	}
	return m.scroll(press), nil
}

// ask sends whatever is in the prompt, unless a run is already going.
//
// Everything from the last run is cleared here, nowhere else: a leftover
// stop reason mislabels this answer truncated, a leftover usage
// double-counts, and a leftover interruption quits a fresh run outright.
func (m *model) ask() tea.Cmd {
	question := strings.TrimSpace(m.prompt.Value())
	if question == "" || m.run.busy {
		return nil
	}
	m.prompt.Reset()
	if cmd, ok := parseCommand(question); ok {
		m.say(fromReader, question)
		return cmd(m)
	}
	m.run.stop = ""
	m.run.usage = nacelle.Usage{}
	m.run.interrupted = time.Time{}
	m.run.asked, m.run.answered = nil, nil
	m.run.waiting = true
	m.say(fromReader, question)
	m.conversation = append(m.conversation, nacelle.UserText(question))

	ctx, cancel := context.WithCancel(context.Background())
	m.run.cancel = cancel
	m.run.busy = true
	m.run.results = start(ctx, m.agent, m.conversation)
	return tea.Batch(waitFor(m.run.results), m.spin.Tick)
}

// consume folds one result into the transcript and waits for the next. The
// answer is committed before the error is — an error printed first would
// tell the reader the request failed, then show the half sentence it
// interrupted, the wrong order to read a failure in.
//
// waiting ends here, on the very first result whatever kind it is: an error
// is still an answer to "is anything happening," and the spinner's job was
// only ever to cover the silence before that first response.
func (m *model) consume(next result) tea.Cmd {
	m.run.waiting = false
	if next.err != nil {
		m.flush()
		m.say(fromFailure, next.err.Error())
		return waitFor(m.run.results)
	}
	m.record(next.event)
	m.absorb(next.event)
	m.render()
	return waitFor(m.run.results)
}

// settle ends a run: whatever streamed is committed, what it cost joins the
// session total, and the prompt opens again. Usage is folded into spent
// here, not left standing, so the next question starts from a clean counter
// and the status line keeps showing a total that only grows.
//
// waiting and pending are cleared here too, not only on their own paths: a
// stream that closes without yielding anything never reaches either one, and
// nothing should be left spinning, or asking a question, about a dead run.
func (m *model) settle() tea.Cmd {
	m.run.cancel()
	m.run.busy = false
	m.run.waiting = false
	m.run.pending = nil

	m.closeResults()
	m.dropUnanswered()
	m.closeTurn(m.run.stop)
	m.spent = m.spent.Add(m.run.usage)
	m.run.usage = nacelle.Usage{}

	m.render()
	return nil
}
