package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// asSettled fills the pointer fields settings() always fills, so a test can
// name only what it cares about.
//
// banner dereferences Config.Bash, the way localTools does and the way every
// consumer of a Config does — a Config that never went through settings() is
// not a lighter version of one, it is an invalid one. A literal that omits a
// pointer field therefore panics rather than failing, taking the whole test
// binary with it, and it has now caught two tests written by two people who
// each had no reason to know. This is cheaper than either of them finding out
// again.
func asSettled(c Config) Config {
	if c.Bash == nil {
		off := false
		c.Bash = &off
	}
	if c.Search == nil {
		none := ""
		c.Search = &none
	}
	if c.Fetch == nil {
		on := true
		c.Fetch = &on
	}
	return c
}

func TestCountedNounPluralizes(t *testing.T) {
	cases := map[int]string{0: "0 skills", 1: "1 skill", 2: "2 skills", 17: "17 skills"}
	for n, want := range cases {
		if got := countedNoun(n, "skill"); got != want {
			t.Errorf("countedNoun(%d, \"skill\") = %q, want %q", n, got, want)
		}
	}
}

// The banner is the one place all three real "is that actually on"
// questions get answered at once — the model billed, the directory tools
// can reach, and what actually got loaded into the system prompt.
func TestBannerShowsBackendModelRootSkillsAndContextFiles(t *testing.T) {
	off := false
	got := banner(&answeringStub{}, asSettled(Config{Model: "claude-opus-5", Root: ".", Bash: &off}),
		loaded{skills: []skill{{name: "deploy"}, {name: "filet"}}, contextFiles: 2}, connected{})

	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("banner = %q, want exactly two lines", got)
	}
	if !strings.Contains(lines[0], "stub") || !strings.Contains(lines[0], "claude-opus-5") {
		t.Errorf("first line = %q, want the backend and model", lines[0])
	}
	if !strings.Contains(lines[1], "2 skills") || !strings.Contains(lines[1], "2 context files") {
		t.Errorf("second line = %q, want the skill count and the context file count", lines[1])
	}
	if !strings.Contains(lines[1], "bash off") {
		t.Errorf("second line = %q, want it to say whether the model can run commands", lines[1])
	}
}

// The symptom of bash being off arrives from the model — "I have no terminal"
// — not from this client, so the banner has to be the thing that connects it
// back to the switch that caused it.
func TestBannerSaysWhenBashIsOn(t *testing.T) {
	on := true
	got := banner(&answeringStub{}, asSettled(Config{Root: ".", Bash: &on}), loaded{}, connected{})

	if !strings.Contains(got, "bash on") {
		t.Errorf("banner = %q, want it to say bash is on", got)
	}
}

// "-root ." reads the same from any directory this happens to be launched
// from, which answers nothing — resolving it is the whole point of putting
// root in the banner at all.
func TestBannerResolvesRootToAnAbsolutePath(t *testing.T) {
	off := false
	got := banner(&answeringStub{}, asSettled(Config{Root: ".", Bash: &off}), loaded{}, connected{})

	if strings.Contains(got, "\n.") || strings.HasSuffix(strings.Split(got, "\n")[1], " . ") {
		t.Errorf("banner = %q, want root resolved, not echoed as \".\"", got)
	}
}

// augmentSystem is what actually produces the counts banner() reports —
// this is the one place a real project context file and a real skill both
// have to survive being loaded and counted from the same call.
func TestAugmentSystemCountsContextFilesAndSkills(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("project preference"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	skillDir := t.TempDir()
	writeSkill(t, filepath.Join(skillDir, "deploy"), "name: deploy\ndescription: ships the app")

	config := defaults()
	config.Root = root
	config.SkillDirs = []string{skillDir}

	found := augmentSystem(&config)

	if found.contextFiles != 1 {
		t.Errorf("contextFiles = %d, want 1", found.contextFiles)
	}
	if len(found.skills) != 1 || found.skills[0].name != "deploy" {
		t.Errorf("skills = %+v, want the one skill under -skill-dir", found.skills)
	}
}

// Search earns a word in the banner only when an instance is configured, so
// a launch that silently has no search still reads as an ordinary launch.
func TestTheBannerNamesSearchOnlyWhenAnInstanceIsSet(t *testing.T) {
	without := banner(&answeringStub{}, asSettled(Config{Root: "."}), loaded{}, connected{})
	if strings.Contains(without, "search on") {
		t.Errorf("banner = %q, want no mention of search when none is configured", without)
	}

	with := banner(&answeringStub{}, asSettled(Config{Root: ".", Web: Web{Search: ptr("https://furet.example")}}), loaded{}, connected{})
	if !strings.Contains(with, "search on") {
		t.Errorf("banner = %q, want it to confirm search is on", with)
	}
}

// "nacelle: nacelle: backend ..." is what a misconfigured budget printed before
// this, and a doubled name is the first thing a person sees on the first run
// that fails.
func TestTheLibraryPrefixIsNotPrintedTwice(t *testing.T) {
	if got := unprefixed(errors.New("nacelle: backend \"anthropic\" does not support x")); got != `backend "anthropic" does not support x` {
		t.Errorf("unprefixed = %q, want the library's own name dropped", got)
	}
	if got := unprefixed(errors.New("nacelle/openrouter: no API key")); got != "nacelle/openrouter: no API key" {
		t.Errorf("unprefixed = %q, want a backend's prefix left alone", got)
	}
}
