package main

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// truncate fits s into max terminal cells, ending in an ellipsis when it had
// to cut something off.
//
// Cells, not bytes, and that is the difference between a guard and a bug. The
// byte-counting version this replaces was wrong twice over. It spent the whole
// budget and *then* appended its ellipsis, so the result came back one cell
// wider than it was asked for — enough to wrap the status line this is called
// on to keep to one row, which is the row layout() reserves exactly one of.
// And it cut on a byte boundary, so "aé" trimmed to two bytes produced invalid
// UTF-8: every queued line carrying an accent was one narrow window away from
// mojibake.
//
// lipgloss.Width is what knows the real answer. It skips ANSI sequences rather
// than counting them as content, and it costs a wide rune the two cells it
// actually occupies, so neither styling nor CJK can talk this over the width
// it was handed.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}

	var kept strings.Builder
	spent := 0
	for _, r := range s {
		cost := lipgloss.Width(string(r))
		if spent+cost > max-1 {
			break
		}
		kept.WriteRune(r)
		spent += cost
	}
	return kept.String() + "…"
}
