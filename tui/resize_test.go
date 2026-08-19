package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A resize-drag burst sends many WindowSizeMsg in quick succession, and most
// of them do not change the width. Rebuilding the markdown renderer and
// re-rendering every historical answer on each one is the exact cost this
// guards against.
func TestAHeightOnlyResizeDoesNotRebuildTheMarkdownRenderer(t *testing.T) {
	m := sized()
	before := m.pretty

	m.resize(tea.WindowSizeMsg{Width: 80, Height: 30})

	if m.pretty != before {
		t.Error("the markdown renderer was rebuilt for a resize that did not change the width")
	}
}

// A width change has to rebuild it — the renderer's word-wrap is baked in at
// construction, so the old one would keep wrapping at the old width.
func TestAWidthChangeRebuildsTheMarkdownRenderer(t *testing.T) {
	m := sized()
	before := m.pretty

	m.resize(tea.WindowSizeMsg{Width: 100, Height: 24})

	if m.pretty == before {
		t.Error("the markdown renderer was not rebuilt after the width changed")
	}
}
