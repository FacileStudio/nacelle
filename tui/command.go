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
// The screen is cleared by scrolling it away, not by tea.ClearScreen, which
// did nothing here and is why this command appeared to reset the session
// without resetting the window. bubbletea's inline renderer owns only the few
// rows it draws, so its clearScreen erases that frame's own cell buffer and
// never touches the transcript printed above it. Blank lines pushed through
// the same insertAbove path every printed line already takes do reach the
// terminal — and they scroll the old session up into the scrollback rather
// than erasing it, which is the behaviour above, kept by accident of being
// the only one available.
//
// This is the only command that prints from its own Cmd rather than by saying
// something, which is why it drains the queue by hand at both ends. Update
// prints what was said before it runs what the message started, which is right
// everywhere else and exactly wrong here: the echoed "/clear" has to go up
// before the blank run and the fresh banner has to go up after it. Left to the
// one drain, a clear either leaves the old prompt standing on the new screen or
// scrolls away the only line saying the session restarted.
//
// The banner is re-said rather than left gone: it is the only place the
// backend and model are ever named, and it is what makes the fresh screen
// legible as a fresh session rather than as a client that lost its place.
func (m *model) clear() tea.Cmd {
	m.conversation = nil
	m.spent = nacelle.Usage{}
	m.size, m.trimmed = 0, 0
	echoed := m.prints()
	m.say(fromClient, m.banner+" · cleared")
	return tea.Sequence(echoed, m.printed(scrolledAway(m.windowHeight)), m.prints())
}

// scrolledAway is the run of blank lines that pushes a whole window of
// finished session up out of sight. One line per row the terminal has, which
// overshoots by however tall the live frame is — that overshoot is the gap in
// the scrollback that reads as where one session ended and the next began.
//
// It goes out through printed for the same reason every other batch does. A
// window's worth of blank lines is exactly the size that scrolls the frame off
// the top, and /clear leaving a copy of the old prompt in the scrollback is
// the one artefact it exists to avoid.
func scrolledAway(height int) string {
	return strings.Repeat("\n", max(height, 1))
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
		"Esc stops a run and nothing else. Ctrl+C stops one too, or quits when idle; ctrl+\\ force-quits.",
		"Enter during a run queues the line and sends it once the run finishes; stopping the run drops whatever is queued.",
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
