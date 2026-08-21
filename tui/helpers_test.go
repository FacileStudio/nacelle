package main

import "strings"

// onScreen is everything this client has produced for the terminal: the lines
// it has said and not yet handed over, plus whatever a run is still
// streaming, with styling stripped. It is what "the viewport" used to mean,
// now that the finished half belongs to the terminal instead.
func onScreen(m *model) string {
	both := append(append([]string{}, m.unprinted...), m.streaming()...)
	return visible(strings.Join(both, "\n"))
}

// spoken is the transcript without the opening banner, which every model has
// and no test about what was said cares about.
func spoken(m *model) []string {
	said := make([]string, 0, len(m.unprinted))
	for _, line := range m.unprinted[1:] {
		said = append(said, visible(line))
	}
	return said
}
