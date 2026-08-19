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

// forceQuit is how long the offer to quit outright stays open after the first
// ctrl+c. Long enough to read the status line, short enough that a second
// ctrl+c minutes later still means "stop this run" rather than "throw the
// session away".
const forceQuit = 3 * time.Second

// model is the whole client: a transcript, a prompt, and at most one run in
// flight.
//
// spent is what every finished run cost, and lives outside the run because it
// outlives it. The status line adds the two, which is a session total that only
// ever goes up.
type model struct {
	agent *nacelle.Agent

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
// Reasoning gets a buffer of its own rather than sharing the answer's. Sharing
// one concatenated the two, which put the last thought against the first word
// with no separator on screen and, worse, committed the reasoning to the
// conversation — so every later turn re-sent a chain of thought the provider
// bills for and does not want replayed.
//
// usage is this run alone, because KindTurn accumulates and KindDone replaces:
// a single counter carrying over from the last run reads as that run's total
// plus this run's turns, and then drops to this run's total the moment it
// finishes.
//
// waiting is true from the moment a question is sent until the first event of
// any kind comes back, success or error. Nothing tells the reader that gap is
// still the model and not a hung client — a request can sit a full second or
// more before the first token, and a screen that has not moved since the
// question was echoed looks exactly like one that stopped responding.
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
}

// newModel builds the client. The banner names the backend and model, so which
// provider is about to be billed is visible before anything is typed rather
// than after the first question fails.
func newModel(agent *nacelle.Agent, banner string) *model {
	prompt := textinput.New()
	prompt.Placeholder = "Ask something. Ctrl+C to stop or quit, Ctrl+\\ to force it."
	prompt.Prompt = "> "
	prompt.SetVirtualCursor(false)
	prompt.Focus()

	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m := &model{
		agent:    agent,
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
	case result:
		return m, m.consume(message)
	case finished:
		return m, m.settle()
	}

	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(message)
	return m, cmd
}

// key handles this client's bindings, reporting whether it consumed the press.
// Anything the scroller does not claim either belongs to the prompt.
//
// Ctrl+C cancels a run in flight and only quits when there is nothing to
// cancel, so a long answer can be abandoned without losing the session. The
// terminal is in raw mode, which means nothing quits on Ctrl+C unless this
// says so.
//
// That courtesy needs an escape hatch, because it depends on the very thing
// that may be stuck: busy is cleared by settle, settle waits for the results
// channel to close, and a tool wedged on a subprocess never closes it. A
// second ctrl+c inside forceQuit, or ctrl+\ at any time, quits whatever the
// run is doing — otherwise the only way out of an alt-screen raw-mode terminal
// is kill -9 from somewhere else.
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
		m.run.cancel()
		m.render()
		return true, nil
	case "enter":
		return true, m.ask()
	}
	return m.scroll(press), nil
}

// ask sends whatever is in the prompt, unless a run is already going.
//
// Everything that belonged to the last run is cleared here and nowhere else: a
// stop reason left standing would accuse this answer of being truncated, a
// usage left standing would be counted twice, and an interruption left standing
// would make the first ctrl+c of a fresh run quit the client outright.
func (m *model) ask() tea.Cmd {
	question := strings.TrimSpace(m.prompt.Value())
	if question == "" || m.run.busy {
		return nil
	}
	m.prompt.Reset()
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

// consume folds one result into the transcript and waits for the next.
//
// The answer is committed before the error is, because an error line printed
// first tells the reader the request failed and only then shows them the half
// sentence it interrupted, which is the wrong order to read a failure in.
//
// waiting ends here, on the very first result of the run, whatever kind it
// is. An error is still an answer to "is anything happening" — the spinner's
// job was only ever to cover the silence before the first response, not to
// promise that response will be good news.
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
// session total, and the prompt opens again.
//
// The run's usage is folded into spent here rather than left standing, so the
// next question starts from a clean per-run counter and the status line keeps
// showing a session total that only grows.
//
// waiting is cleared here too, not only on the first event: a run whose
// stream closes without yielding anything at all — an immediate cancel, or a
// backend that errors before its first yield — never reaches consume, and the
// spinner it started would otherwise spin for a question already abandoned.
func (m *model) settle() tea.Cmd {
	m.run.cancel()
	m.run.busy = false
	m.run.waiting = false

	m.closeResults()
	m.dropUnanswered()
	m.closeTurn(m.run.stop)
	m.spent = m.spent.Add(m.run.usage)
	m.run.usage = nacelle.Usage{}

	m.render()
	return nil
}
