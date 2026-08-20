package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// -skill-dir is how another tool's skills reach the model without moving or
// copying them — one directory per occurrence, each read the same way
// ~/.agents/skills is.
func TestExtraSkillsReadsEveryDirNamed(t *testing.T) {
	claude := t.TempDir()
	writeSkill(t, filepath.Join(claude, "filet"), "name: filet\ndescription: runs the linter")
	codex := t.TempDir()
	writeSkill(t, filepath.Join(codex, "review"), "name: review\ndescription: reviews a diff")

	found := extraSkills([]string{claude, codex})

	if len(found) != 2 {
		t.Fatalf("found = %+v, want one skill from each directory", found)
	}
}

// A directory named in -skill-dir that was never there is the same ordinary
// case as ~/.agents/skills missing — nothing to report, not an error.
func TestExtraSkillsToleratesAMissingDirectory(t *testing.T) {
	if found := extraSkills([]string{filepath.Join(t.TempDir(), "nope")}); found != nil {
		t.Errorf("found = %+v, want nil for a directory that was never there", found)
	}
}

// A flag's own argument is already expanded by the shell before nacelle
// sees it, but ~/.nacelle.yml and NACELLE_SKILL_DIRS go through no shell at
// all — this is the one place "~/.claude/skills" has to work from either.
func TestExpandHomeResolvesALeadingTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got, want := expandHome("~/.claude/skills"), filepath.Join(home, ".claude", "skills"); got != want {
		t.Errorf("expandHome(~/.claude/skills) = %q, want %q", got, want)
	}
	if got := expandHome("/already/absolute"); got != "/already/absolute" {
		t.Errorf("expandHome(/already/absolute) = %q, want it left alone", got)
	}
}

// The end-to-end path a real -skill-dir run takes: an extra directory's
// skill has to reach the rendered system prompt from one loadSkills call,
// not just from extraSkills in isolation.
func TestLoadSkillsIncludesExtraDirs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	other := t.TempDir()
	writeSkill(t, filepath.Join(other, "deploy"), "name: deploy\ndescription: ships the app")

	result := loadSkills(t.TempDir(), false, []string{other})

	if !strings.Contains(result.system, "deploy") {
		t.Errorf("system = %q, want the extra directory's skill included", result.system)
	}
}
