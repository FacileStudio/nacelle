package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"
)

// ConfigFile is where settings are read from when the flags leave them out.
const ConfigFile = ".nacelle.yml"

// Config is one layer of settings. Every field is a pointer or an empty-able
// string so that a layer can say nothing about a setting rather than saying
// zero, which is the whole difficulty of a precedence chain: "false" and "not
// mentioned" are different answers and a bool cannot tell them apart.
//
// It holds no credentials, deliberately. They already have two homes — the
// environment, and the Anthropic SDK's own profile — and a file with a key in
// it is a file that can never be committed to a dotfiles repo, which is the
// only reason to want one of these on two machines.
type Config struct {
	Backend       string `yaml:"backend"`
	Model         string `yaml:"model"`
	Root          string `yaml:"root"`
	System        string `yaml:"system"`
	Bash          *bool  `yaml:"bash"`
	ApproveTools  *bool  `yaml:"approve_tools"`
	MaxIterations *int   `yaml:"max_iterations"`

	// Reasoning is embedded and inlined for both of the reasons the three
	// groups below are, and the inlining is load-bearing in a way it is
	// nowhere else here. Effort and Thinking were top-level fields until a
	// third setting joined them, so `effort:` and `thinking:` are already
	// written at the top level of every ~/.nacelle.yml that mentions them.
	// load() decodes with KnownFields, which means a key that quietly
	// relocated under a heading would not be a warning on the next launch,
	// it would be a refusal to start. The tag is what keeps both keys
	// exactly where they were, and config.Effort and config.Thinking still
	// resolve, so no call site moved either.
	Reasoning `yaml:",inline"`

	// Web is embedded rather than named, so every field on it is still
	// reached as config.Search, for the reason Discovery is: it keeps this
	// struct's own field count from growing every time the list does.
	// yaml:",inline" keeps the keys at the file's top level.
	Web `yaml:",inline"`

	// Discovery is embedded rather than named so every field on it is still
	// reached as config.Mycelium, not config.Discovery.Mycelium — the grouping
	// exists only to keep this struct's own field count from growing by one
	// every time that list does, not to change how anything reads it.
	// yaml:",inline" is load-bearing for the same reason on the file side:
	// every key stays at ~/.nacelle.yml's top level, exactly where it was
	// before this existed.
	Discovery `yaml:",inline"`

	// Sources is embedded, and inlined, for both of the reasons just
	// given — and the inlining is what keeps skill_dirs at the top level
	// of every config file already written rather than relocating it
	// under a key nobody has.
	Sources `yaml:",inline"`

	// Hooks are lifecycle commands compiled down to library hooks at
	// launch. Inline so the key stays at the file's top level like every
	// other; accumulated by merge for the reason MCP is — a second
	// layer's hooks are more hooks, not replacements.
	Hooks hookConfig `yaml:"hooks"`
}

// defaults is the bottom layer, and the only one that answers everything.
//
// Mycelium, ProjectContext and Skills default on, unlike Bash: all three fail
// soft with nothing to show for it when there is nothing to find — no
// mycelium on PATH, no CLAUDE.md or AGENTS.md anywhere above Root, no
// ~/.agents/skills/ — so a machine without any of them is no worse off for
// asking, and a machine with them gets the benefit without a flag to
// discover first.
//
// TrustSkills and ApproveTools default off, and neither is the reasoning
// above. Loading skills found globally or already trusted is one thing; a
// project's own .agents/skills/ can carry instructions to run arbitrary
// scripts, and blanket-trusting whatever a directory happens to contain is a
// decision only the person running this should make, not a default.
// ApproveTools is off for a plainer reason: every tool this client has ever
// run, it has run unasked, and a gate that started asking by default would
// change that for everyone who never asked for one. Most consumers of an
// agent loop — a script, a CI job, someone who trusts the model to just get
// on with it — have nobody to ask and want none of this; the interactive
// human who does is the one who turns it on.
//
// Budget defaults to zero, which is the one answer here that really is "say
// nothing": no reasoning ceiling is set from this side and the backend applies
// whatever it applies. It is still filled in rather than left nil, because
// this is the layer that answers everything and every deref above it depends
// on that being true.
func defaults() Config {
	bash, thinking, mycelium, projectContext, skills, trustSkills, approveTools, trustHooks :=
		false, false, true, true, true, false, false, false
	iterations, budget := 40, int64(0)
	search, fetch := "", true
	return Config{
		Web:           Web{Search: &search, Fetch: &fetch},
		Backend:       "anthropic",
		Root:          ".",
		System:        defaultSystem,
		Bash:          &bash,
		ApproveTools:  &approveTools,
		MaxIterations: &iterations,
		Reasoning:     Reasoning{Thinking: &thinking, Budget: &budget},
		Discovery: Discovery{
			Mycelium:         &mycelium,
			ProjectContext: &projectContext,
			Skills:         &skills,
			TrustSkills:    &trustSkills,
			TrustHooks:     &trustHooks,
		},
	}
}

// configPath is where the file lives, honouring HOME so a test does not have to
// write to the real one, and the empty string when there is no home to look in.
//
// No home is not a failure. Under systemd, cron or `env -i` there is no HOME
// and there is no config file either, which is the ordinary case this client
// already handles — refusing to start there meant nacelle could not run with
// every setting passed on the command line, for want of a file it was never
// going to read.
func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ConfigFile)
}

// load reads the config file. A missing file is not an error — most people
// never write one — but an unreadable or malformed one is, because a config
// silently ignored is worse than no config at all: the setting you carefully
// wrote is simply not in effect and nothing says so.
//
// KnownFields is that promise applied to the keys as well as to the syntax.
// Unmarshal accepts anything it does not recognise, so `max_iteration: 3` — one
// letter short — parses cleanly, leaves the ceiling at 40, and costs real money
// on the next long run without a word about it. A refused key is a typo found
// in one second instead of one invoice.
//
// An empty path means there is no home directory to hold a file, and an empty
// file decodes to io.EOF; both are the same ordinary "no config" answer.
func load(path string) (Config, error) {
	if path == "" {
		return Config{}, nil
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)

	var settings Config
	if err := decoder.Decode(&settings); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return settings, nil
}

// settings resolves every layer in one place.
//
// Flag beats environment beats file beats default, and it is resolved here and
// nowhere else. The suite has already paid for the alternative: a CLI that read
// its environment inside one branch and its file inside another turned what the
// README called overrides into two mutually exclusive modes, and four copies of
// a precedence chain are four chances to disagree.
func settings(flags Config) (Config, error) {
	file, err := load(configPath())
	if err != nil {
		return Config{}, err
	}

	resolved := defaults()
	resolved.merge(file)
	resolved.merge(fromEnv())
	resolved.merge(flags)
	return resolved, nil
}
