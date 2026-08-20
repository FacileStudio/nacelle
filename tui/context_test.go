package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noGlobalInstructions points HOME at an empty directory, so a test about
// the project walk is not at the mercy of whether the machine running it
// happens to have ~/.agents/AGENTS.md — real content there would otherwise
// leak into every one of these and make them pass or fail depending on
// whose machine ran them.
func noGlobalInstructions(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

// A monorepo's root CLAUDE.md carries suite-wide convention and a
// subdirectory's carries what is specific to it — both should reach the
// model, with the closer, more specific one last where it reads as "this is
// about me" rather than being buried under general context.
func TestProjectContextOrdersRootToLeaf(t *testing.T) {
	noGlobalInstructions(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("suite-wide convention"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "AGENTS.md"), []byte("specific to this package"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, _ := projectContext(sub)

	root, leaf := strings.Index(got, "suite-wide convention"), strings.Index(got, "specific to this package")
	if root < 0 || leaf < 0 {
		t.Fatalf("context = %q, want both files' content present", got)
	}
	if leaf < root {
		t.Errorf("context = %q, want the closer file (sub/AGENTS.md) last, not the root's", got)
	}
}

// No instruction file anywhere in the ancestry is the ordinary case for most
// callers, and it must not manufacture a header with nothing under it.
func TestProjectContextIsEmptyWithNoFilesFound(t *testing.T) {
	noGlobalInstructions(t)
	dir := t.TempDir()

	got, count := projectContext(dir)
	if got != "" {
		t.Errorf("context = %q, want empty with nothing found", got)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 with nothing found", count)
	}
}

// Either filename alone is enough — a project need not have both.
func TestProjectContextAcceptsEitherFilename(t *testing.T) {
	noGlobalInstructions(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("just agents.md here"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, _ := projectContext(dir)

	if !strings.Contains(got, "just agents.md here") {
		t.Errorf("context = %q, want the lone AGENTS.md picked up", got)
	}
	if strings.Contains(got, "CLAUDE.md") {
		t.Errorf("context = %q, want no mention of a file that was never written", got)
	}
}

// ~/.agents/AGENTS.md is the AGENTS.md standard's own global-base path —
// the file Codex, Cursor, Copilot, Gemini and pi already read — so it
// reaches the model too, ahead of anything project-specific: more general
// even than the filesystem root, per projectContext's own doc comment.
func TestProjectContextIncludesTheGlobalAgentsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "AGENTS.md"), []byte("global preference"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("project preference"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, count := projectContext(dir)

	global, project := strings.Index(got, "global preference"), strings.Index(got, "project preference")
	if global < 0 || project < 0 {
		t.Fatalf("context = %q, want both the global and the project file present", got)
	}
	if project < global {
		t.Errorf("context = %q, want the global file first — more general than even the filesystem root", got)
	}
	if !strings.Contains(got, "Global instructions from") {
		t.Errorf("context = %q, want the global file labelled as global, not as a project's own", got)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 — the global file and the project's own", count)
	}
}

// No ~/.agents directory at all is the ordinary case on a machine that has
// not adopted the convention, not a degraded one — it must not error or
// leave a header with nothing under it.
func TestProjectContextToleratesNoAgentsDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("project preference"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, _ := projectContext(dir)

	if !strings.Contains(got, "project preference") {
		t.Errorf("context = %q, want the project file still picked up", got)
	}
	if strings.Contains(got, "Global instructions") {
		t.Errorf("context = %q, want no global section manufactured from nothing", got)
	}
}
