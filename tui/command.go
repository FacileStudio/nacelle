package main

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
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
// match — nothing here yet takes an argument.
//
// A line starting with '/' that names no known command still counts as a
// command, reported back to the reader rather than sent to the model: a typo
// like "/clera" is far more likely than a real question meant to start with
// a slash, the same trade-off every peer client with slash commands makes.
func parseCommand(line string) (command, bool) {
	if !strings.HasPrefix(line, "/") {
		return nil, false
	}
	name, _, _ := strings.Cut(line[1:], " ")
	if cmd, ok := commands[name]; ok {
		return cmd, true
	}
	return func(m *model) tea.Cmd {
		m.say(fromClient, "unknown command "+line+" — try /help")
		return nil
	}, true
}

// commandNames lists every registered command, "/"-prefixed and sorted, for
// the prompt's own suggestion list — the one place that list is built, so a
// command added to commands starts suggesting itself instead of only working
// once someone remembers to wire it in twice.
func commandNames() []string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, "/"+name)
	}
	sort.Strings(names)
	return names
}

// suggestCommands turns commandNames into completion: a match ghosts in
// ahead of the cursor as it's typed, tab accepts it. Kept next to commands
// itself rather than in newModel, so the prompt's own construction does not
// need to know the two are related.
func suggestCommands(prompt *textinput.Model) {
	prompt.ShowSuggestions = true
	prompt.SetSuggestions(commandNames())
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
		"",
		"Ctrl+C cancels a run, or quits when idle. Ctrl+\\ force-quits.",
		"Up/Down/PageUp/PageDown scroll the transcript.",
		"Tab completes a /command; ctrl+n/ctrl+p cycle suggestions when more than one matches.",
	}, "\n"))
	return nil
}

// quit ends the program outright. Unlike Ctrl+C it carries no ambiguity
// about a run in flight, because ask() never reaches a command while one is
// busy — this is always a deliberate, idle exit.
func (m *model) quit() tea.Cmd {
	return tea.Quit
}
