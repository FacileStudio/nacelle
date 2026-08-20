package main

import (
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
