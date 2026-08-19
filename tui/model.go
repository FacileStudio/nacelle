package main

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
)

// model is the whole client: a transcript, a prompt, and at most one run in
// flight.
type model struct {
	agent *nacelle.Agent

	viewport viewport.Model
	prompt   textinput.Model

	transcript   []string
	answer       strings.Builder
	conversation []nacelle.Message

	results <-chan result
	cancel  context.CancelFunc
	usage   nacelle.Usage
	stop    nacelle.Stop
	busy    bool
}

func newModel(agent *nacelle.Agent) *model {
	prompt := textinput.New()
	prompt.Placeholder = "Ask something. Ctrl+C to stop or quit."
	prompt.Prompt = "> "
	prompt.SetVirtualCursor(false)
	prompt.Focus()

	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	return &model{
		agent:    agent,
		viewport: view,
		prompt:   prompt,
		cancel:   func() {},
	}
}

func (m *model) Init() tea.Cmd { return textinput.Blink }

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
	case result:
		return m, m.consume(message)
	case finished:
		return m, m.settle()
	}

	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(message)
	return m, cmd
}

// resize gives the prompt one line and the status one, and the transcript the
// rest.
func (m *model) resize(size tea.WindowSizeMsg) tea.Cmd {
	m.viewport.SetWidth(size.Width)
	m.prompt.SetWidth(size.Width)
	m.viewport.SetHeight(max(size.Height-2, 1))
	m.render()
	return nil
}

// key handles the two bindings this client has, reporting whether it consumed
// the press. Anything else belongs to the prompt.
//
// Ctrl+C cancels a run in flight and only quits when there is nothing to
// cancel, so a long answer can be abandoned without losing the session. The
// terminal is in raw mode, which means nothing quits on Ctrl+C unless this
// says so.
func (m *model) key(press tea.KeyPressMsg) (bool, tea.Cmd) {
	switch press.String() {
	case "ctrl+c":
		if m.busy {
			m.cancel()
			return true, nil
		}
		return true, tea.Quit
	case "enter":
		return true, m.ask()
	}
	return false, nil
}

// ask sends whatever is in the prompt, unless a run is already going.
func (m *model) ask() tea.Cmd {
	question := strings.TrimSpace(m.prompt.Value())
	if question == "" || m.busy {
		return nil
	}
	m.prompt.Reset()
	m.stop = ""
	m.say("you", question)
	m.conversation = append(m.conversation, nacelle.Message{Text: question})

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.busy = true
	m.results = start(ctx, m.agent, m.conversation)
	return waitFor(m.results)
}

// consume folds one result into the transcript and waits for the next.
func (m *model) consume(next result) tea.Cmd {
	if next.err != nil {
		m.say("error", next.err.Error())
		return waitFor(m.results)
	}
	m.absorb(next.event)
	m.render()
	return waitFor(m.results)
}

// settle ends a run: the streamed answer joins the conversation so the next
// question carries it, and the prompt opens again.
//
// Only the text is kept. A nacelle.Message holds text and nothing else today,
// so a tool call the model made cannot be replayed to it — which is a gap in
// the core, not something a client can paper over.
func (m *model) settle() tea.Cmd {
	m.cancel()
	m.busy = false
	if answer := m.answer.String(); answer != "" {
		m.conversation = append(m.conversation, nacelle.Message{Assistant: true, Text: answer})
	}
	m.answer.Reset()
	m.render()
	return nil
}
