package main

import (
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"

	"github.com/FacileStudio/nacelle"
)

// forceQuit is how long the offer to quit outright stays open after a run is
// first asked to stop — long enough to read the status line, short enough that
// a ctrl+c minutes later still means "stop this run", not "quit".
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

	account

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

// Update routes each message to the one place that owns it, and hands whatever
// that had to say to the terminal before whatever it started.
//
// That ordering is the whole of a bug this client shipped on two paths at
// once. Two of the commands route returns block until the model sends
// something — waitFor, and the batch send wraps it in — and a sequence does
// not reach its next command until the one before it is done, so a line said
// on the way into a wait was not drawn until that wait ended. The question
// appeared when the answer did rather than when it was asked, and a tool's
// call line waited on the tool it announced: six seconds late for a
// six-second command, measured. Nothing already said is worth less than the
// thing it is waiting for. Fixing it here rather than in those two commands
// is the point — a third would arrive with no reason to know it had to flush
// first, which is how the second sat there while the first was being fixed.
//
// Two statements, not one call: as arguments they are evaluated left to right,
// which would drain the queue before the message that fills it was routed.
//
// Sequence, not Batch: a batch makes no promise about the order its commands
// run in, and the routed cmd may be tea.Quit — a quit that wins that race
// takes the last thing said with it, which for a queued /quit is the echo of
// the line that quit.
func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	started := m.route(message)
	return m, tea.Sequence(m.prints(), started)
}

// route is Update's own body, split out so that draining the print queue is
// one seam rather than a line repeated down every branch.
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
//
// Esc stops a run and does nothing else, which is the whole reason it is
// worth having next to a ctrl+c that already cancels: the key that stops the
// answer is then never the key that might close the client, so there is no
// press that has to be thought about first. It sits below the menu on
// purpose — esc closes the dropdown before it stops anything, because a
// dropdown standing open is the nearer thing to back out of, and it is what
// esc already meant there. See escaped for what it does with no run to stop.
func (m *model) key(press tea.KeyPressMsg) (bool, tea.Cmd) {
	switch press.String() {
	case "ctrl+\\":
		return true, tea.Quit
	case "ctrl+c":
		if !m.run.busy || time.Since(m.run.interrupted) < forceQuit {
			return true, tea.Quit
		}
		m.abandon()
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
	case "esc":
		return m.escaped()
	case "enter":
		return true, m.ask()
	}
	return false, nil
}
