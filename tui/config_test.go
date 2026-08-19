package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// written puts a config file in a home directory of the test's own, so a test
// never reads or writes the real one.
func written(t *testing.T, body string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	if body == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(home, ConfigFile), []byte(body), 0o600); err != nil {
		t.Fatalf("writing the config: %v", err)
	}
}

// Most people never write one, so a missing file has to be ordinary rather
// than an error.
func TestNoConfigFileLeavesTheDefaultsStanding(t *testing.T) {
	written(t, "")

	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if config.Backend != "anthropic" || config.Root != "." {
		t.Errorf("config = %+v, want the defaults", config)
	}
}

// A config that cannot be parsed must not be skipped in silence: the setting
// you carefully wrote is simply not in effect, and nothing says so.
func TestAMalformedConfigIsAnError(t *testing.T) {
	written(t, "backend: [this is not a string")

	if _, err := settings(Config{}); err == nil {
		t.Fatal("a malformed config was accepted")
	}
}

func TestTheFileBeatsTheDefaults(t *testing.T) {
	written(t, "backend: openrouter\nmodel: deepseek/deepseek-v4-flash-0731\n")

	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if config.Backend != "openrouter" || config.Model != "deepseek/deepseek-v4-flash-0731" {
		t.Errorf("config = %+v, want the file's backend and model", config)
	}
}

// Environment variables are overrides, not a separate mode. A sibling CLI in
// this suite read its variables only inside a branch that ignored the config
// file, which turned what its README called overrides into two mutually
// exclusive modes nobody could tell apart.
func TestTheEnvironmentBeatsTheFileWithoutReplacingIt(t *testing.T) {
	written(t, "backend: openrouter\nmodel: from-the-file\nroot: /from/the/file\n")
	t.Setenv(EnvPrefix+"MODEL", "from-the-environment")

	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if config.Model != "from-the-environment" {
		t.Errorf("model = %q, want the environment to win", config.Model)
	}
	if config.Root != "/from/the/file" {
		t.Errorf("root = %q, want the file's value to survive an unrelated override", config.Root)
	}
}

func TestAFlagBeatsEverything(t *testing.T) {
	written(t, "model: from-the-file\n")
	t.Setenv(EnvPrefix+"MODEL", "from-the-environment")

	config, err := settings(Config{Model: "from-the-flag"})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if config.Model != "from-the-flag" {
		t.Errorf("model = %q, want the flag to win", config.Model)
	}
}

// The reason every toggle is a pointer. A layer that says nothing and a layer
// that says false are different answers, and a bool cannot tell them apart —
// so a file turning something off has to survive a default that had it on.
func TestATurnedOffToggleIsNotMistakenForAnUnsetOne(t *testing.T) {
	written(t, "max_iterations: 3\nthinking: true\n")

	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if *config.MaxIterations != 3 {
		t.Errorf("max iterations = %d, want the file's 3", *config.MaxIterations)
	}
	if !*config.Thinking {
		t.Error("thinking = false, want the file to have turned it on")
	}
	if *config.Bash {
		t.Error("bash = true, want the default to stand when no layer mentions it")
	}
}

// A value strconv cannot read means the writer meant something; falling through
// to the layer below is closer to that than silently choosing false.
func TestAnUnreadableEnvironmentValueFallsThroughRatherThanMeaningFalse(t *testing.T) {
	written(t, "bash: true\n")
	t.Setenv(EnvPrefix+"BASH", "yes-please")

	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if !*config.Bash {
		t.Error("bash = false, want the file's true to survive an unreadable override")
	}
}

// The whole promise of refusing a malformed file, applied to the keys. A file
// saying max_iteration — one letter short — parsed cleanly, left the ceiling at
// 40 and cost real money on the next long run without a word about it.
func TestATypoInAKeyIsRefusedRatherThanIgnored(t *testing.T) {
	written(t, "max_iteration: 3\n")

	config, err := settings(Config{})
	if err == nil {
		t.Fatalf("settings = %+v, want a misspelt key refused", config)
	}
	if !strings.Contains(err.Error(), "max_iteration") {
		t.Errorf("error = %v, want it to name the key it did not know", err)
	}
}

// Under systemd, cron or `env -i` there is no HOME and no config file either,
// which is the ordinary case this client already handles. Refusing to start
// meant nacelle could not run with every setting passed on the command line,
// for want of a file it was never going to read.
func TestAnUnresolvableHomeMeansNoConfigFileRatherThanNoProgram(t *testing.T) {
	t.Setenv("HOME", "")

	config, err := settings(Config{Model: "from-the-flag"})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if config.Model != "from-the-flag" {
		t.Errorf("model = %q, want the flag honoured with nowhere to read a file from", config.Model)
	}
}
