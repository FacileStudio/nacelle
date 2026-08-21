package main

import (
	"fmt"
	"path/filepath"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/anthropic"
)

// banner is the two lines the transcript opens with.
//
// The backend and model come first, because the failure that costs the most
// is discovering, after composing a question, that the client was pointed
// at a provider you have no key for. Root, skill count and how many
// CLAUDE.md/AGENTS.md files loaded come second — none of it is decorative:
// each is a real "is that actually on" question this client had no way to
// answer before without a debug build.
//
// Each web setting is named in the direction that would otherwise surprise:
// search when it is on, because it had to be configured and confirming that
// is the point; fetch when it is off, because it is on by default and a model
// that cannot read a page it just found needs the reason on screen.
//
// Search is named only when it is on. Being off has no symptom — nothing is
// offered, so nothing goes wrong and there is nothing to explain — and naming
// it every launch would be a permanent line about something most people
// running this have not configured and did not ask about.
//
// Root is resolved to an absolute path rather than echoed as typed, because
// "-root ." reads the same from any directory nacelle happens to be
// launched from and answers nothing on its own.
//
// Whether bash is on is named last and named after the flag, because the
// symptom of it being off arrives from the model rather than from this
// client: asked to build something, it answers that it has no terminal and
// cannot run a command. That is true and deliberate — run_command is
// unconfined, so it stays a decision — but nothing on screen connected it to
// a line in ~/.nacelle.yml written once and forgotten. Saying "bash off"
// where the model's own capabilities are listed is the shortest path from
// that answer back to the switch that causes it.
//
// MCP earns its words only when a server was configured, for the reason
// search does: a launch with none is the ordinary launch, nothing is offered,
// and there is no symptom to explain. When there is one, both counts are named
// rather than only the servers — a server that starts and offers nothing is
// the one failure this client cannot otherwise show, since it is an error
// nowhere and its only trace is a model that never reaches for the tool.
func banner(backend nacelle.Backend, config Config, found loaded, mcp connected) string {
	model := config.Model
	if model == "" {
		model = anthropic.DefaultModel
	}
	root := absolute(config.Root)
	bash := "bash off"
	if *config.Bash {
		bash = "bash on"
	}
	line := fmt.Sprintf("%s · %s\n%s · %s · %s · %s", backend.Name(), model, root,
		countedNoun(len(found.skills), "skill"), countedNoun(found.contextFiles, "context file"), bash)

	if mcp.servers > 0 {
		line += " · " + countedNoun(mcp.servers, "MCP server") + ", " + countedNoun(mcp.tools, "tool")
	}
	if *config.Search != "" {
		line += " · search on"
	}
	if !*config.Fetch {
		line += " · fetch off"
	}
	return line
}

// countedNoun is "N noun" or "N nouns" — the one piece of English this
// client bothers to pluralize, because the banner reads it on every launch.
func countedNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// absolute is the root as a path that means the same thing from anywhere.
// "-root ." reads identically from every directory nacelle was launched in
// and so answers nothing on its own — neither for the person reading the
// banner nor for the model reading the system prompt, which are the two
// callers.
func absolute(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	return abs
}
