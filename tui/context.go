package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// instructionFile is one CLAUDE.md or AGENTS.md found on the walk, kept
// alongside its path so the rendered output can say where it came from.
type instructionFile struct{ path, content string }

// projectContext finds every CLAUDE.md and AGENTS.md between root and the
// filesystem root, and returns them concatenated as extra system-prompt
// text — root-to-leaf order, most specific last, empty string if none exist.
//
// It walks up from root rather than down from it, because the file that
// matters is the one describing the project the client was launched inside,
// which is usually an ancestor of the working directory a person actually
// typed, not something buried under it. Every match at every level is kept,
// not just the nearest: a monorepo's root CLAUDE.md carries suite-wide
// convention and a subdirectory's carries what is specific to it, and a
// reader who wrote both meant both read together.
//
// Global, cross-project instruction files (a user's own ~/.claude/CLAUDE.md)
// are deliberately not read here. That file is written for Claude Code
// specifically — it references Claude Code's own tools, hooks and slash
// commands — and folding it into a different agent's system prompt is more
// likely to confuse the model than help it. Scoping to the project tree is
// the safe default; a caller who wants more can read further and pass it
// through Config.System themselves.
func projectContext(root string) string {
	return renderLevels(instructionLevels(root))
}

// instructionLevels walks from root to the filesystem root, returning every
// directory that held a CLAUDE.md or an AGENTS.md, closest to root first and
// the filesystem root last — the reverse of the order the caller wants, so
// renderLevels is what turns it around.
func instructionLevels(root string) [][]instructionFile {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil
	}

	var levels [][]instructionFile
	for dir := abs; ; {
		if here := instructionsIn(dir); len(here) > 0 {
			levels = append(levels, here)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return levels
		}
		dir = parent
	}
}

// instructionsIn reads CLAUDE.md and AGENTS.md from one directory, in that
// order. Either can be absent, and a file that exists but cannot be read is
// treated the same as one that was never there — one unreadable file is not
// a reason to fail the whole walk.
func instructionsIn(dir string) []instructionFile {
	var here []instructionFile
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		here = append(here, instructionFile{path: path, content: string(raw)})
	}
	return here
}

// renderLevels concatenates levels found closest-to-root-first into
// root-to-leaf text, reversing instructionLevels' walk order so the level
// nearest where projectContext was called — the most specific one — ends up
// last, immediately in front of whatever the model reads next.
func renderLevels(levels [][]instructionFile) string {
	var body strings.Builder
	for i := len(levels) - 1; i >= 0; i-- {
		for _, f := range levels[i] {
			fmt.Fprintf(&body, "\n\n## Project instructions from %s\n\n%s", f.path, f.content)
		}
	}
	return body.String()
}
