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
	"flag"
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

	set, err := tools.New(tools.Config{Root: config.Root, AllowBash: *config.Bash})
	if err != nil {
		return fmt.Errorf("opening %s: %w", config.Root, err)
	}
	defer set.Close()

	local, err := set.Tools()
	if err != nil {
		return fmt.Errorf("building the tool set: %w", err)
	}

	agent, backend, err := build(config, local)
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(newModel(agent, banner(backend, config))).Run()
	return err
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

// fromFlags is the settings layer the command line supplies.
//
// Only the flags actually typed are collected. Go's flag package cannot tell a
// flag left alone from one passed its own default value, so Visit — which
// reports exactly the ones that were set — is what stops a default from
// silently outranking the config file it is supposed to sit beneath.
func fromFlags() Config {
	fallback := defaults()
	backend := flag.String("backend", fallback.Backend, "anthropic or openrouter")
	model := flag.String("model", fallback.Model, "model id, defaulting to the backend's own")
	effort := flag.String("effort", fallback.Effort, "low, medium, high, xhigh or max")
	root := flag.String("root", fallback.Root, "directory the file tools may reach")
	system := flag.String("system", fallback.System, "system prompt")
	bash := flag.Bool("bash", *fallback.Bash, "let the model run commands")
	thinking := flag.Bool("thinking", *fallback.Thinking, "stream the model's reasoning")
	iterations := flag.Int("max-iterations", *fallback.MaxIterations, "how many times the model may be asked")
	flag.Parse()

	typed := map[string]func(*Config){
		"backend":        func(c *Config) { c.Backend = *backend },
		"model":          func(c *Config) { c.Model = *model },
		"effort":         func(c *Config) { c.Effort = *effort },
		"root":           func(c *Config) { c.Root = *root },
		"system":         func(c *Config) { c.System = *system },
		"bash":           func(c *Config) { c.Bash = bash },
		"thinking":       func(c *Config) { c.Thinking = thinking },
		"max-iterations": func(c *Config) { c.MaxIterations = iterations },
	}

	var flags Config
	flag.Visit(func(f *flag.Flag) {
		if take, known := typed[f.Name]; known {
			take(&flags)
		}
	})
	return flags
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
