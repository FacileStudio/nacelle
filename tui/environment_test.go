package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withApproval(on bool) Config {
	config := defaults()
	bash := true
	config.ApproveTools, config.Bash = &on, &bash
	return config
}

// The root is the one fact the model cannot recover by any means: no tool
// reports it, and "." resolves differently for every caller.
func TestEnvironmentNamesTheRootAbsolutely(t *testing.T) {
	config := withApproval(false)
	config.Root = "."

	got := environment(config, time.Now())

	want, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, want) {
		t.Errorf("environment() = %q, want the root resolved to %q rather than left as \".\"", got, want)
	}
}

// The trap this exists for: clean() strips a leading "/" instead of refusing
// it, so read_file "/etc/hosts" silently reads "etc/hosts" under the root
// while run_command reads the real one.
func TestEnvironmentWarnsThatAbsolutePathsOnlyWorkInRunCommand(t *testing.T) {
	got := environment(withApproval(false), time.Now())

	for _, want := range []string{"stripped", "run_command"} {
		if !strings.Contains(got, want) {
			t.Errorf("environment() = %q, want it to mention %q", got, want)
		}
	}
}

func TestEnvironmentDatesTheSession(t *testing.T) {
	got := environment(withApproval(false), time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))

	if !strings.Contains(got, "2026-08-20") {
		t.Errorf("environment() = %q, want it to carry the date it was given", got)
	}
}

// Both directions are said, and the negative is the one that matters: with
// the gate off the model is the last check before an irreversible command.
func TestEnvironmentSaysWhetherAnyoneReviewsAToolCall(t *testing.T) {
	on := environment(withApproval(true), time.Now())
	if !strings.Contains(on, "refusal") {
		t.Errorf("environment() = %q, want the approval gate explained when it is on", on)
	}
	if strings.Contains(on, "Nobody sees one first") {
		t.Errorf("environment() = %q, want no unreviewed warning while the gate is on", on)
	}

	if off := environment(withApproval(false), time.Now()); !strings.Contains(off, "Nobody sees one first") {
		t.Errorf("environment() = %q, want the model told nothing reviews a call with the gate off", off)
	}
}

// The shell is unconfined and, by default, unreviewed. Naming the commands
// that cannot be undone is the cheapest guard there is — but only where a
// shell was actually mounted.
func TestEnvironmentWarnsAboutIrreversibleCommandsOnlyWithBash(t *testing.T) {
	if got := environment(withApproval(false), time.Now()); !strings.Contains(got, "git reset --hard") {
		t.Errorf("environment() = %q, want the irreversible commands named when the shell is on", got)
	}

	config := withApproval(false)
	off := false
	config.Bash = &off
	if got := environment(config, time.Now()); strings.Contains(got, "git reset --hard") {
		t.Errorf("environment() = %q, want no shell warning when there is no shell", got)
	}
}

// Every session gets it, including one running with both discovery switches
// off and a system prompt of its own.
func TestAugmentSystemAlwaysAppendsTheEnvironment(t *testing.T) {
	off := false
	config := defaults()
	config.System = "You are something else entirely."
	config.ProjectContext, config.Skills = &off, &off

	augmentSystem(&config)

	if !strings.Contains(config.System, "Working directory:") {
		t.Errorf("System = %q, want the session's own facts appended regardless of the switches", config.System)
	}
}

// run_command is off by default, and explaining a tool the model has not
// been given teaches it to trust the rest of this section less.
func TestEnvironmentMentionsRunCommandOnlyWhenBashIsMounted(t *testing.T) {
	config := withApproval(false)
	off := false
	config.Bash = &off

	if got := environment(config, time.Now()); strings.Contains(got, "run_command") {
		t.Errorf("environment() = %q, want no word about run_command when -bash is off", got)
	}
}
