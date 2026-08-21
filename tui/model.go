package main

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"

	"github.com/FacileStudio/nacelle"
)

// forceQuit is how long the offer to quit outright stays open after the
// first ctrl+c — long enough to read the status line, short enough that a
// second ctrl+c minutes later still means "stop this run", not "quit".
const forceQuit = 3 * time.Second

// commandState is everything /skill:name and the dropdown menu need beyond
// what command.go itself owns: skills to resolve a name against, the
// dropdown's own filter/selection state, and the window height layout()
// needs to reserve the dropdown's own space out of. Embedded rather than
// named, the same reason Config embeds Discovery in config.go: every field
// still reads as m.skills or m.menu, not m.commandState.skills — grouping
// exists only to keep model's own field count from growing by one every
// time this list does.
type commandState struct {
	skills map[string]skill
	menu   commandMenu
}

// model is the whole client: a transcript, a prompt, and at most one run in
// flight.
//
// spent outlives the run it came from — the status line adds the two into a
// session total that only ever goes up.
type model struct {
	agent  *nacelle.Agent
	banner string

	prompt    textarea.Model
	unprinted []string

	conversation []nacelle.Message
	spent        nacelle.Usage

	theme  palette
	pretty *glamour.TermRenderer
	spin   spinner.Model

	commandState
	screen
	run inflight
}

// newModel builds the client. The banner names the backend and model, so
// which provider is billed is visible before typing, not after it fails.
// skills is every skill loaded this run — kept keyed by name so
// /skill:name is a lookup, not a scan, every time it's typed, and listed
// alongside the client's own commands in the dropdown menu.
func newModel(agent *nacelle.Agent, banner string, skills []skill) *model {
	byName := bySkillName(skills)

	m := &model{
		agent:  agent,
		banner: banner,
		prompt: newPrompt(),
		theme:  themed(true),
		spin:   spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		screen: screen{width: 80, liveRows: 1},
		commandState: commandState{
			skills: byName,
			menu:   commandMenu{items: menuItems(byName)},
		},
		run: inflight{cancel: func() {}, running: map[string]string{}},
	}
	m.pretty = prettier(m.theme.markdown, m.width)
	m.say(fromClient, banner)
	return m
}

// Init asks the terminal what colour it is and nothing else. There is no
// cursor-blink command because the prompt draws no cursor of its own — see
// newPrompt's SetVirtualCursor(false); the caret on screen is the terminal's,
// positioned by View, and a blink tick for a cursor nobody renders is a
// timer that wakes the program up to change nothing.
func (m *model) Init() tea.Cmd { return tea.RequestBackgroundColor }

// Update routes each message to the one place that owns it. The cases stay
// thin on purpose: everything that changes more than one field is a method, so
// this reads as a table of what can happen rather than as the logic itself.
func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	return m, tea.Sequence(m.route(message), m.prints())
}

// route is Update's own body, split out so that draining the print queue is
// one seam rather than a line repeated down every branch.
//
// Sequence, not Batch: a batch makes no promise about order, and the cmd
// being routed may be tea.Quit — a quit that wins the race takes the last
// thing said with it, which for ctrl+c is the notice explaining what was
// dropped.
func (m *model) route(message tea.Msg) tea.Cmd {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		return m.resize(message)
	case tea.KeyPressMsg:
		if handled, cmd := m.key(message); handled {
			return cmd
		}
	case tea.BackgroundColorMsg:
		m.theme = themed(message.IsDark())
		m.restyle()
		return nil
	case spinner.TickMsg:
		return m.spun(message)
	case approvalRequest:
		m.run.pending = &message
		return nil
	case result:
		return m.consume(message)
	case finished:
		return m.settle()
	}

	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(message)
	m.refreshMenu()
	return cmd
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
//
// The dropdown menu is checked next, ahead of both enter and the scroller:
// while it's open, up/down/tab/enter/esc belong to picking a command, not
// to scrolling the transcript or sending what's typed. Anything the menu
// itself does not claim (an ordinary character, backspace) falls all the
// way through to the prompt, which is what keeps its own filter editable.
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
		m.dropQueued()
		return true, nil
	}
	if m.run.pending != nil {
		return true, m.decide(press)
	}
	if m.menu.open() {
		if handled, cmd := m.navigateMenu(press); handled {
			return true, cmd
		}
	}
	switch press.String() {
	case "enter":
		return true, m.ask()
	}
	return false, nil
}

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
func (m *model) dispatch(line string) tea.Cmd {
	m.say(fromReader, line)

	if cmd, ok := m.parseCommand(line); ok {
		return cmd(m)
	}
	return m.send(line)
}
