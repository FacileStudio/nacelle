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
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/anthropic"
	"github.com/FacileStudio/nacelle/openrouter"
)

// defaultSystem is who the model is and how it answers — the one layer
// -system replaces outright, because a different persona is exactly what
// that flag is for. Everything augmentSystem appends survives it: where the
// root is and what a leading slash does are not a matter of taste.
//
// It stays this short on purpose. Codex ships two prompts for one harness —
// 6.6KB for the models post-trained on it, 24KB for general GPT-5 — and the
// extra seventeen kilobytes is autonomy, planning and answer-formatting that
// the tuned model already knew. Claude is that tuned model for this shape of
// tool, so the ceiling here is closer to pi's ~600 characters than to
// anyone's twenty. What is here is only what a terminal changes about
// answering. Be brief, because the transcript competes with the live region
// for rows. Prefer a path over pasted source, because a dumped file scrolls
// the answer off the screen. And do not claim a thing works unchecked, which
// is the one failure a person cannot catch by reading the reply.
const defaultSystem = `You are a terminal coding assistant. Read before you write, keep edits small, and say what you changed.

Answer as briefly as the question allows, and lead with the result rather than the reasoning that reached it. Refer to code by path and line instead of pasting it back — the person asking has the file open, and a transcript full of quoted source scrolls the answer out of view. Do not say something works until you have checked that it does; where you could not check, say so.`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nacelle:", unprefixed(err))
		os.Exit(1)
	}
}

// unprefixed drops the library's own name from a message this program is about
// to put its name in front of.
//
// nacelle's errors say which package refused, which is correct for a library:
// a caller with several dependencies needs to know which one turned it down.
// This program is that library's front end and has already written the name, so
// keeping both prints "nacelle: nacelle: backend ...". A backend's prefix is
// left alone on purpose, because "nacelle/openrouter" says which backend and
// that half is worth reading twice.
func unprefixed(err error) string {
	return strings.TrimPrefix(err.Error(), "nacelle: ")
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

	mcp, local, err := mcpTools(config, local)
	if err != nil {
		return err
	}
	defer func() { _ = mcp.set.Close() }()

	found := augmentSystem(&config)
	approvalGate, approve := buildApprovals(config)

	hooks, hookNotice, err := sessionHooks(config)
	if err != nil {
		return err
	}

	agent, backend, err := build(config, local, approve, hooks)
	if err != nil {
		return err
	}

	return launch(uiSession{
		agent: agent, banner: banner(backend, config, found, mcp),
		skills: found.skills, hookNotice: hookNotice, gate: approvalGate,
	})
}

// client is what run hands to the bubbletea program: everything built above
// it, so run reads as setup and this as the one call that starts the UI.
type uiSession struct {
	agent      *nacelle.Agent
	banner     string
	skills     []skill
	hookNotice string
	gate       *approvals
}

// launch opens the program and delivers whatever was queued for the
// transcript before it opened.
func launch(c uiSession) error {
	model := newModel(c.agent, c.banner, c.skills)
	if c.hookNotice != "" {
		model.say(fromClient, c.hookNotice)
	}

	program := tea.NewProgram(model)
	wireApprovals(c.gate, program)
	_, err := program.Run()
	return err
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

// augmentSystem folds this session's own facts, then project context, then
// skills into config.System in place.
//
// The order is general to specific, the same rule projectContext sorts its
// own levels by: how this machine works, then what this project expects,
// then what is available to run. Only the first is unconditional — the
// other two are switches, and a session that turned both off still needs to
// be told where it is.
func augmentSystem(config *Config) loaded {
	var found loaded
	config.System += environment(*config, time.Now())
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
//
// The three reasoning settings fold into one nacelle.Thinking here, and the
// one rename in that fold is worth knowing about: this client's -thinking
// becomes Show, which decides what the transcript displays and nothing else.
// The model reasons and is billed for it either way, and the reasoning now
// travels back over the wire either way, so turning -thinking off no longer
// costs the model its own last thought on the next tool round.
func build(config Config, local []nacelle.Tool, approve nacelle.Approve, hooks map[nacelle.HookPoint][]nacelle.Hook) (*nacelle.Agent, nacelle.Backend, error) {
	backend, err := chosen(config)
	if err != nil {
		return nil, nil, err
	}

	agent, err := nacelle.New(nacelle.Config{
		Backend: nacelle.Retry(backend, nacelle.RetryOptions{}),
		System:  config.System,
		Thinking: nacelle.Thinking{
			Effort: nacelle.Effort(config.Effort),
			Budget: *config.Budget,
			Show:   *config.Thinking,
		},
		Tools:         local,
		MaxIterations: *config.MaxIterations,
		Approve:       approve,
		Hooks:         hooks,
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
