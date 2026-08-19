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
	Backend        string `yaml:"backend"`
	Model          string `yaml:"model"`
	Effort         string `yaml:"effort"`
	Root           string `yaml:"root"`
	System         string `yaml:"system"`
	Bash           *bool  `yaml:"bash"`
	Thinking       *bool  `yaml:"thinking"`
	Jardin         *bool  `yaml:"jardin"`
	ProjectContext *bool  `yaml:"project_context"`
	MaxIterations  *int   `yaml:"max_iterations"`
}

// defaults is the bottom layer, and the only one that answers everything.
//
// Jardin and ProjectContext default on, unlike Bash: both fail soft with
// nothing to show for it when there is nothing to find — no jardin on PATH,
// no CLAUDE.md or AGENTS.md anywhere above Root — so a machine without either
// is no worse off for asking, and a machine with them gets the benefit
// without a flag to discover first.
func defaults() Config {
	bash, thinking, jardin, projectContext, iterations := false, false, true, true, 40
	return Config{
		Backend:        "anthropic",
		Root:           ".",
		System:         defaultSystem,
		Bash:           &bash,
		Thinking:       &thinking,
		Jardin:         &jardin,
		ProjectContext: &projectContext,
		MaxIterations:  &iterations,
	}
}

// merge overwrites every setting the layer above actually mentions, and leaves
// the rest alone.
func (c *Config) merge(over Config) {
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
	if over.MaxIterations != nil {
		c.MaxIterations = over.MaxIterations
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
