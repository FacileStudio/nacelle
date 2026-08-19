package main

import (
	"fmt"
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
	MaxIterations *int   `yaml:"max_iterations"`
}

// defaults is the bottom layer, and the only one that answers everything.
func defaults() Config {
	bash, thinking, iterations := false, false, 40
	return Config{
		Backend:       "anthropic",
		Root:          ".",
		System:        defaultSystem,
		Bash:          &bash,
		Thinking:      &thinking,
		MaxIterations: &iterations,
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
	if over.MaxIterations != nil {
		c.MaxIterations = over.MaxIterations
	}
}

// configPath is where the file lives, honouring HOME so a test does not have to
// write to the real one.
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding your home directory: %w", err)
	}
	return filepath.Join(home, ConfigFile), nil
}

// load reads the config file. A missing file is not an error — most people
// never write one — but an unreadable or malformed one is, because a config
// silently ignored is worse than no config at all: the setting you carefully
// wrote is simply not in effect and nothing says so.
func load(path string) (Config, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var file Config
	if err := yaml.Unmarshal(body, &file); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return file, nil
}

// settings resolves every layer in one place.
//
// Flag beats environment beats file beats default, and it is resolved here and
// nowhere else. The suite has already paid for the alternative: a CLI that read
// its environment inside one branch and its file inside another turned what the
// README called overrides into two mutually exclusive modes, and four copies of
// a precedence chain are four chances to disagree.
func settings(flags Config) (Config, error) {
	path, err := configPath()
	if err != nil {
		return Config{}, err
	}
	file, err := load(path)
	if err != nil {
		return Config{}, err
	}

	resolved := defaults()
	resolved.merge(file)
	resolved.merge(fromEnv())
	resolved.merge(flags)
	return resolved, nil
}
