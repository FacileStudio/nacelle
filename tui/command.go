package main

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
)

// command is one of the client's own actions, typed with a leading '/' and
// resolved entirely without a run — nothing here reaches the model, unlike
// everything else typed into the prompt.
type command func(m *model) tea.Cmd

var commands = map[string]command{
	"clear": (*model).clear,
	"help":  (*model).help,
	"quit":  (*model).quit,
}

// parseCommand reports the command a line names, and whether the line named
// one at all. Only the first word is read, so "/clear" and "/clear now" both
// match the same client command — "/skill:name and-this" is the one case
// with an argument, forwarded to runSkill as everything after the name.
//
// A line starting with '/' that names no known command or skill still
// counts as a command, reported back to the reader rather than sent to the
// model: a typo like "/cler" is far more likely than a real question meant
// to start with a slash, the same trade-off every peer client with slash
// commands makes.
//
// This is a method, not the free function it was, because a skill's own
// name is only known at this run's construction — m.skills — unlike
// commands, fixed at compile time.
func (m *model) parseCommand(line string) (command, bool) {
	if !strings.HasPrefix(line, "/") {
		return nil, false
	}
	name, rest, _ := strings.Cut(line[1:], " ")
	if cmd, ok := commands[name]; ok {
		return cmd, true
	}
	if skillName, ok := strings.CutPrefix(name, "skill:"); ok {
		if s, ok := m.skills[skillName]; ok {
			return runSkill(s, rest), true
		}
		return func(m *model) tea.Cmd {
			m.say(fromClient, "unknown skill "+skillName+" — try /help")
			return nil
		}, true
	}
	return func(m *model) tea.Cmd {
		m.say(fromClient, "unknown command "+line+" — try /help")
		return nil
	}, true
}

// commandNames lists every registered command, "/"-prefixed and sorted, for
// the dropdown menu's own candidate list (menuItems, in menu.go) — the one
// place this list is built, so a command added to commands starts showing
// up there too instead of only working once someone remembers to wire it
// in twice.
func commandNames() []string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, "/"+name)
	}
	sort.Strings(names)
	return names
}

// clear starts a new session in the same client: the conversation sent to
// the model and the running cost total are both reset. Nothing about the
// process restarts, which is the whole point of it being a command rather
// than a reason to quit and relaunch.
//
// It clears the screen rather than the history, and the difference is the
// point. What was said is in the terminal's scrollback and is not this
// client's to delete — the same reason no shell's own clear erases what came
// before it. Scroll back and the old session is still there, which is what
// somebody who cleared the wrong window will want.
//
// The banner is re-said rather than left gone: it is the only place the
// backend and model are ever named, and it is what makes the fresh screen
// legible as a fresh session rather than as a client that lost its place.
func (m *model) clear() tea.Cmd {
	m.conversation = nil
	m.spent = nacelle.Usage{}
	m.say(fromClient, m.banner+" · cleared")
	return tea.ClearScreen
}

// help lists the client's own commands and keybindings, distinct from
// anything the model is ever asked — the one list of them that exists.
func (m *model) help() tea.Cmd {
	m.say(fromClient, strings.Join([]string{
		"/clear — start a new session, same client",
		"/help — show this message",
		"/quit — quit",
		"/skill:name [what to do] — run a loaded skill directly, instead of waiting for the model to decide to",
		"",
		"Ctrl+C cancels a run, or quits when idle. Ctrl+\\ force-quits.",
		"Enter during a run queues the line and sends it once the run finishes; ctrl+c drops whatever is queued.",
		"The prompt wraps and grows as you type. Alt+Enter (or ctrl+j) starts a new line without sending.",
		"Scroll, select and copy with the terminal as usual — what was said is ordinary terminal output, not a window this client owns.",
		"Typing / opens a dropdown of commands and skills — up/down move, tab/enter pick, esc closes it.",
	}, "\n"))
	return nil
}

// quit ends the program outright. Unlike Ctrl+C it carries no ambiguity
// about a run in flight, because ask() never reaches a command while one is
// busy — this is always a deliberate, idle exit.
func (m *model) quit() tea.Cmd {
	return tea.Quit
}
