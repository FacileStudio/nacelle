// Command tui is a terminal client for a nacelle agent.
//
// It exists to be the SDK's first consumer. A terminal exercises every event
// kind a backend can produce — text, reasoning, a tool starting, a tool
// finishing, what a turn cost — which is more of the contract than a headless
// caller touches, and it does so while someone is watching.
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

// options is what the flags collect.
type options struct {
	backend string
	model   string
	root    string
	bash    bool
	system  string
}

const defaultSystem = "You are a terminal coding assistant. " +
	"Read before you write, keep edits small, and say what you changed."

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		os.Exit(1)
	}
}

// parse reads the flags. AllowBash is a flag rather than a default because the
// tool set is not a sandbox: run_command is unconfined, and that has to be a
// decision someone made rather than one they inherited.
func parse() options {
	var opts options
	flag.StringVar(&opts.backend, "backend", "anthropic", "anthropic or openrouter")
	flag.StringVar(&opts.model, "model", "", "model id, defaulting to the backend's own")
	flag.StringVar(&opts.root, "root", ".", "directory the file tools may reach")
	flag.BoolVar(&opts.bash, "bash", false, "let the model run commands")
	flag.StringVar(&opts.system, "system", defaultSystem, "system prompt")
	flag.Parse()
	return opts
}

func run() error {
	opts := parse()

	set, err := tools.New(tools.Config{Root: opts.root, AllowBash: opts.bash})
	if err != nil {
		return fmt.Errorf("opening %s: %w", opts.root, err)
	}
	defer set.Close()

	local, err := set.Tools()
	if err != nil {
		return fmt.Errorf("building the tool set: %w", err)
	}

	backend, err := chosen(opts)
	if err != nil {
		return err
	}

	agent, err := nacelle.New(nacelle.Config{
		Backend:       nacelle.Retry(backend, nacelle.RetryOptions{}),
		System:        opts.system,
		Tools:         local,
		MaxIterations: 40,
	})
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(newModel(agent)).Run()
	return err
}

// chosen builds the backend the flags asked for.
//
// There is no default backend in the library and there is none here either:
// the flag has a value, but it names the choice rather than hiding it, and an
// unknown one is refused rather than quietly falling back to a model the
// caller did not ask for and will be billed for.
func chosen(opts options) (nacelle.Backend, error) {
	switch opts.backend {
	case "anthropic":
		return anthropic.New(anthropic.Config{Model: opts.model}), nil
	case "openrouter":
		if opts.model == "" {
			return nil, fmt.Errorf("openrouter needs -model, it has no default")
		}
		return openrouter.New(openrouter.Config{Model: opts.model})
	default:
		return nil, fmt.Errorf("unknown backend %q, want anthropic or openrouter", opts.backend)
	}
}
