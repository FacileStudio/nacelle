package tools

import (
	"context"
	"strings"
	"testing"
)

// An output-streaming command reports each completed line as it is produced,
// so a consumer can draw progress live instead of waiting for completion.
func TestCommandStreamsItsOutputLineByLine(t *testing.T) {
	set := newSet(t, nil)
	order := make([]string, 0, 4)

	_, err := set.run(context.Background(), "printf 'one\\ntwo\\nthree\\n'", DefaultCommandTimeout, func(line string) {
		order = append(order, line)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Join(order, ","); got != "one,two,three" {
		t.Errorf("streamed lines = %q, want one,two,three", got)
	}
}

// A command that streams also returns its full output to the caller, so a
// consumer that only reads the result keeps working unchanged.
func TestCommandStreamingStillReturnsTheWholeOutput(t *testing.T) {
	set := newSet(t, nil)
	var seen string

	out, err := set.run(context.Background(), "printf 'alpha\\nbeta\\n'", DefaultCommandTimeout, func(line string) {
		seen = line
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("output = %q, want the full output back", out)
	}
	if seen != "beta" {
		t.Errorf("last streamed line = %q, want beta", seen)
	}
}
