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

// clear starts a new session in the same client: the transcript, the
// conversation sent to the model, and the running cost total are all reset.
// Nothing about the process restarts, which is the whole point of it being a
// command rather than a reason to quit and relaunch.
//
// ask() echoes "/clear" itself before calling this, same as any other
// command — but that echo lands in the transcript this then wipes, so it is
// never actually seen. Harmless, and not worth special-casing: the one
// command whose own echo cannot survive it is the one named clear.
//
// The banner is re-said rather than left gone: it is the only place the
// backend and model are ever named, and wiping the transcript would
// otherwise wipe that along with it.
func (m *model) clear() tea.Cmd {
	m.transcript = nil
	m.conversation = nil
	m.spent = nacelle.Usage{}
	m.say(fromClient, m.banner+" · cleared")
	return nil
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
		"Up/Down/PageUp/PageDown and the mouse wheel scroll the transcript; sending anything returns to the end.",
		"Selecting text with the mouse needs shift held down, because the wheel is reported to this client instead of the terminal.",
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
