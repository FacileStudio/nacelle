package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A monorepo's root CLAUDE.md carries suite-wide convention and a
// subdirectory's carries what is specific to it — both should reach the
// model, with the closer, more specific one last where it reads as "this is
// about me" rather than being buried under general context.
func TestProjectContextOrdersRootToLeaf(t *testing.T) {
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

	got := projectContext(sub)

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
	dir := t.TempDir()

	if got := projectContext(dir); got != "" {
		t.Errorf("context = %q, want empty with nothing found", got)
	}
}

// Either filename alone is enough — a project need not have both.
func TestProjectContextAcceptsEitherFilename(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("just agents.md here"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := projectContext(dir)

	if !strings.Contains(got, "just agents.md here") {
		t.Errorf("context = %q, want the lone AGENTS.md picked up", got)
	}
	if strings.Contains(got, "CLAUDE.md") {
		t.Errorf("context = %q, want no mention of a file that was never written", got)
	}
}
