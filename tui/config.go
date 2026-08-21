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
	Effort        string `yaml:"effort"`
	Root          string `yaml:"root"`
	System        string `yaml:"system"`
	Bash          *bool  `yaml:"bash"`
	Thinking      *bool  `yaml:"thinking"`
	ApproveTools  *bool  `yaml:"approve_tools"`
	MaxIterations *int   `yaml:"max_iterations"`

	// Web is embedded rather than named, so every field on it is still
	// reached as config.Search, for the reason Discovery is: it keeps this
	// struct's own field count from growing every time the list does.
	// yaml:",inline" keeps the keys at the file's top level.
	Web `yaml:",inline"`

	// Discovery is embedded rather than named so every field on it is still
	// reached as config.Jardin, not config.Discovery.Jardin — the grouping
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
}

// defaults is the bottom layer, and the only one that answers everything.
//
// Jardin, ProjectContext and Skills default on, unlike Bash: all three fail
// soft with nothing to show for it when there is nothing to find — no
// jardin on PATH, no CLAUDE.md or AGENTS.md anywhere above Root, no
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
func defaults() Config {
	bash, thinking, jardin, projectContext, skills, trustSkills, approveTools :=
		false, false, true, true, true, false, false
	iterations := 40
	search, fetch := "", true
	return Config{
		Web:           Web{Search: &search, Fetch: &fetch},
		Backend:       "anthropic",
		Root:          ".",
		System:        defaultSystem,
		Bash:          &bash,
		Thinking:      &thinking,
		ApproveTools:  &approveTools,
		MaxIterations: &iterations,
		Discovery: Discovery{
			Jardin:         &jardin,
			ProjectContext: &projectContext,
			Skills:         &skills,
			TrustSkills:    &trustSkills,
		},
	}
}

// merge overwrites every setting the layer above actually mentions, and leaves
// the rest alone. SkillDirs reads "mentioned" the same way mergeStrings reads
// an empty string: nothing yet lets a layer clear it on purpose, so an empty
// slice only ever means a layer that never mentioned it.
//
// MCP is the one list that accumulates rather than replaces. Sources.MCP is
// where the reason is written down, because it is a fact about what that list
// is for and not about how layers are resolved.
func (c *Config) merge(over Config) {
	c.mergeStrings(over)
	c.mergeToggles(over)
	if over.MaxIterations != nil {
		c.MaxIterations = over.MaxIterations
	}
	if over.Search != nil {
		c.Search = over.Search
	}
	if over.Fetch != nil {
		c.Fetch = over.Fetch
	}
	if len(over.SkillDirs) > 0 {
		c.SkillDirs = over.SkillDirs
	}
	c.MCP = append(c.MCP, over.MCP...)
}

// mergeStrings overwrites every string setting over actually mentions. Empty
// is "not mentioned" for these — none has a meaningful empty value, which is
// exactly what makes a string safe to use unlike the toggles below.
func (c *Config) mergeStrings(over Config) {
	if over.Backend != "" {
		c.Backend = over.Backend
	}
	if over.Model != "" {
		c.Model = over.Model
	}
	if over.Effort != "" {
		c.Effort = over.Effort
	}
	if over.Root != "" {
		c.Root = over.Root
	}
	if over.System != "" {
		c.System = over.System
	}
}

// mergeToggles overwrites every *bool setting over actually mentions. These
// stay pointers rather than joining mergeStrings' plain-value treatment
// because a layer saying nothing and a layer saying false are different
// answers a bool cannot tell apart.
func (c *Config) mergeToggles(over Config) {
	if over.Bash != nil {
		c.Bash = over.Bash
	}
	if over.Thinking != nil {
		c.Thinking = over.Thinking
	}
	if over.Jardin != nil {
		c.Jardin = over.Jardin
	}
	if over.ProjectContext != nil {
		c.ProjectContext = over.ProjectContext
	}
	if over.Skills != nil {
		c.Skills = over.Skills
	}
	if over.TrustSkills != nil {
		c.TrustSkills = over.TrustSkills
	}
	if over.ApproveTools != nil {
		c.ApproveTools = over.ApproveTools
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
