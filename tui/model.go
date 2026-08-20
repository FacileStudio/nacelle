package main

import (
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

	skills map[string]skill
	run    inflight
}

// newModel builds the client. The banner names the backend and model, so
// which provider is billed is visible before typing, not after it fails.
// skills is every skill loaded this run — kept keyed by name so
// /skill:name is a lookup, not a scan, every time it's typed.
func newModel(agent *nacelle.Agent, banner string, skills []skill) *model {
	byName := bySkillName(skills)

	prompt := textinput.New()
	prompt.Placeholder = "Ask something. Ctrl+C to stop or quit, Ctrl+\\ to force it."
	prompt.Prompt = "> "
	prompt.SetVirtualCursor(false)
	suggestCommands(&prompt, byName)
	prompt.Focus()
	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m := &model{
		agent:    agent,
		banner:   banner,
		viewport: view,
		prompt:   prompt,
		theme:    themed(true),
		spin:     spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		skills:   byName,
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
// The prompt is echoed once, up front, for every non-empty line — a
// command's own reply and a /skill:name's expanded question would otherwise
// need their own echo, the way /clear already had to work around wiping its
// own before this existed.
func (m *model) ask() tea.Cmd {
	question := strings.TrimSpace(m.prompt.Value())
	if question == "" || m.run.busy {
		return nil
	}
	m.prompt.Reset()
	m.say(fromReader, question)

	if cmd, ok := m.parseCommand(question); ok {
		return cmd(m)
	}
	return m.send(question)
}
