package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

// Everything finished is printed to the terminal, so the status line is drawn
// directly beneath the last line of an answer that is no longer this client's
// to redraw. Without a row between them the token count reads as part of the
// sentence above it.
func TestABlankRowSeparatesTheAnswerFromTheStatusLine(t *testing.T) {
	m := sized()

	lines := strings.Split(visible(m.View().Content), "\n")
	status := -1
	for i, line := range lines {
		if strings.Contains(line, "ready") {
			status = i
			break
		}
	}
	if status < 1 {
		t.Fatalf("no status line found in\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[status-1]) != "" {
		t.Errorf("row above the status line = %q, want it blank", lines[status-1])
	}
}

// testedUltraviolet is the github.com/charmbracelet/ultraviolet commit that
// charm.land/bubbletea/v2 v2.0.9 requires, and the newest one this client is
// known to draw correctly on.
//
// A later commit moved curbuf.Resize ahead of the clear pass in Render, so
// TerminalRenderer.move clamps its remembered cursor row against the frame
// that is arriving instead of the one on screen. In inline mode that row is
// the only record of where the cursor physically is, so every shrink climbed
// too few rows and redrew the frame below the rows it had failed to erase:
// closing the / dropdown left its list painted with a stale status line above
// it, and deleting an alt+enter line walked the frame down the pane.
//
// Nothing this client does can detect that from inside, so the version is
// pinned instead. If bubbletea moves to a newer ultraviolet, run the shrink by
// hand before following it: type "/", press backspace, and look for leftover
// dropdown rows.
const testedUltraviolet = "v0.0.0-20260703014108-f5a850f9c2b7"

// The renderer that draws this client's frame is two modules down and cannot
// be reached from a test, so this asserts the version rather than the drawing.
//
// It fails rather than skips when the build info is missing: a tripwire that
// can quietly decide not to fire is not one. Build info is present under the
// workspace, under GOWORK=off, and under -race, which is every way this runs.
func TestTheInlineRendererIsTheCommitBubbleteaWasTestedAgainst(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("no build info in this binary, so the pin below went unchecked")
	}
	for _, dep := range info.Deps {
		if dep.Path != "github.com/charmbracelet/ultraviolet" {
			continue
		}
		if dep.Version != testedUltraviolet {
			t.Errorf("ultraviolet = %s, want %s: re-verify the shrink before moving it",
				dep.Version, testedUltraviolet)
		}
		return
	}
	t.Fatal("ultraviolet is not in the build graph")
}
