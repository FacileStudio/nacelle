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

	notice := augmentSystem(&config)

	agent, backend, err := build(config, local)
	if err != nil {
		return err
	}

	client := newModel(agent, banner(backend, config))
	if notice != "" {
		client.say(fromClient, notice)
	}
	_, err = tea.NewProgram(client).Run()
	return err
}

// localTools opens the file/search/command tools and, when asked, adds
// mycelium's — the caller owns closing the returned Set.
func localTools(config Config) (*tools.Set, []nacelle.Tool, error) {
	set, err := tools.New(tools.Config{Root: config.Root, AllowBash: *config.Bash})
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", config.Root, err)
	}

	local, err := set.Tools()
	if err != nil {
		return nil, nil, fmt.Errorf("building the tool set: %w", err)
	}
	if *config.Mycelium {
		myceliumTools, err := tools.Mycelium()
		if err != nil {
			return nil, nil, fmt.Errorf("building mycelium's tools: %w", err)
		}
		local = append(local, myceliumTools...)
	}
	return set, local, nil
}

// augmentSystem folds project context and skills into config.System in
// place, and returns whatever the person running this should be told before
// the transcript opens — empty when there is nothing to say.
func augmentSystem(config *Config) string {
	if *config.ProjectContext {
		config.System += projectContext(config.Root)
	}
	if !*config.Skills {
		return ""
	}
	result := loadSkills(config.Root, *config.TrustSkills)
	config.System += result.system
	return result.notice
}

// build assembles the agent the settings describe, and hands the backend back
// so the caller can say which one answered.
func build(config Config, local []nacelle.Tool) (*nacelle.Agent, nacelle.Backend, error) {
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

// banner is the line the transcript opens with.
//
// It names the backend and model before anything is typed, because the failure
// that costs the most is discovering, after composing a question, that the
// client was pointed at a provider you have no key for.
func banner(backend nacelle.Backend, config Config) string {
	model := config.Model
	if model == "" {
		model = anthropic.DefaultModel
	}
	return fmt.Sprintf("%s · %s", backend.Name(), model)
}
