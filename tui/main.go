// Command nacelle is a terminal client for a nacelle agent.
//
// It exists to be the SDK's first consumer. A terminal exercises every event
// kind a backend can produce — text, reasoning, a tool starting, a tool
// finishing, why a turn ended, what it cost — which is more of the contract
// than a headless caller touches, and it does so while someone is watching.
//
// It is deliberately small. Sessions, profiles, themes and panes are what a
// product grows; none of them tests the API, and the point of this one is to
// find out where the API is annoying before three consumers depend on it.
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/anthropic"
	"github.com/FacileStudio/nacelle/openrouter"
	"github.com/FacileStudio/nacelle/tools"
)

const defaultSystem = "You are a terminal coding assistant. " +
	"Read before you write, keep edits small, and say what you changed."

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nacelle:", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := settings(fromFlags())
	if err != nil {
		return err
	}

	set, local, err := localTools(config)
	if err != nil {
		return err
	}
	defer func() { _ = set.Close() }()

	found := augmentSystem(&config)

	approvalGate, approve := buildApprovals(config)

	agent, backend, err := build(config, local, approve)
	if err != nil {
		return err
	}

	client := newModel(agent, banner(backend, config, found), found.skills)
	if found.notice != "" {
		client.say(fromClient, found.notice)
	}

	program := tea.NewProgram(client)
	wireApprovals(approvalGate, program)
	_, err = program.Run()
	return err
}

// localTools opens the file/search/command tools and, when asked, adds
// mycelium's and the web search — the caller owns closing the returned Set.
//
// WebSearch's error is returned unwrapped, unlike mycelium's. It already names
// the endpoint and what is wrong with it, and a second "building the web
// search tool" in front of that says nothing the reader has not just read.
//
// Every failure after tools.New succeeds closes the Set on the way out.
// Without that it is dropped on the floor: the caller's own defer only ever
// sees the nil this returns on an error, so the *os.Root behind it — a real
// descriptor — outlives the only reference to it. The process exits moments
// later today and nothing notices, which is the kind of leak that becomes
// real the first time something rebuilds a tool set without restarting.
//
// The handle is held in a plain local rather than in the returned one, and
// that is load-bearing rather than style. A named result is assigned by the
// return statement *before* any deferred function runs, so with the Set named
// there, `return nil, nil, err` had already overwritten it — the deferred
// close then panicked on a nil receiver instead of releasing anything. The
// error paths still hand the caller nil, which is what it wants; only the
// closing needs a name the returns cannot reach.
func localTools(config Config) (_ *tools.Set, local []nacelle.Tool, err error) {
	opened, err := tools.New(tools.Config{Root: config.Root, AllowBash: *config.Bash})
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", config.Root, err)
	}

	defer func() {
		if err != nil {
			_ = opened.Close()
		}
	}()

	local, err = opened.Tools()
	if err != nil {
		return nil, nil, fmt.Errorf("building the tool set: %w", err)
	}
	if *config.Mycelium {
		var myceliumTools []nacelle.Tool
		if myceliumTools, err = tools.Mycelium(); err != nil {
			return nil, nil, fmt.Errorf("building mycelium's tools: %w", err)
		}
		local = append(local, myceliumTools...)
	}

	searching, err := tools.WebSearch(config.Search)
	if err != nil {
		return nil, nil, err
	}

	local = append(local, searching...)

	return opened, local, nil
}

// loaded is what augmentSystem folds into config.System, and enough about
// what it actually found to summarize back — in the notice when something
// needs the person running this to act, in the banner every time, and in
// []skill so /skill:name has something to resolve a name against. One
// call, not a second walk of the same files just to count them.
type loaded struct {
	notice       string
	skills       []skill
	contextFiles int
}

// augmentSystem folds project context and skills into config.System in
// place.
func augmentSystem(config *Config) loaded {
	var found loaded
	if *config.ProjectContext {
		text, files := projectContext(config.Root)
		config.System += text
		found.contextFiles = files
	}
	if !*config.Skills {
		return found
	}
	skills := loadSkills(config.Root, *config.TrustSkills, config.SkillDirs)
	config.System += skills.system
	found.notice = skills.notice
	found.skills = skills.skills
	return found
}

// build assembles the agent the settings describe, and hands the backend back
// so the caller can say which one answered. approve is nil unless
// -approve-tools was asked for — see nacelle.Approve's own doc comment for
// why nil, not a rubber-stamp function, is what "off" means here.
func build(config Config, local []nacelle.Tool, approve nacelle.Approve) (*nacelle.Agent, nacelle.Backend, error) {
	backend, err := chosen(config)
	if err != nil {
		return nil, nil, err
	}

	agent, err := nacelle.New(nacelle.Config{
		Backend:       nacelle.Retry(backend, nacelle.RetryOptions{}),
		System:        config.System,
		Effort:        nacelle.Effort(config.Effort),
		Thinking:      *config.Thinking,
		Tools:         local,
		MaxIterations: *config.MaxIterations,
		Approve:       approve,
	})
	if err != nil {
		return nil, nil, err
	}
	return agent, backend, nil
}

// chosen builds the backend the settings ask for.
//
// An unknown name is refused rather than quietly falling back to a model the
// caller did not choose and will be billed for, which is the same reason the
// library itself ships no default backend.
func chosen(config Config) (nacelle.Backend, error) {
	switch config.Backend {
	case "anthropic":
		return anthropic.New(anthropic.Config{Model: config.Model}), nil
	case "openrouter":
		if config.Model == "" {
			return nil, fmt.Errorf("openrouter needs a model: pass -model, or set model in ~/%s", ConfigFile)
		}
		return openrouter.New(openrouter.Config{Model: config.Model})
	default:
		return nil, fmt.Errorf("unknown backend %q, want anthropic or openrouter", config.Backend)
	}
}
