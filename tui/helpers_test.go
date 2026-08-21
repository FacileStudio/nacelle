package main

import (
	"fmt"
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// printMessage is what bubbletea turns a Println into. The type is the
// library's own and unexported, so the only handle on it out here is its name.
//
// Asking the library for it instead — %T of a tea.Println's own message — is
// the obvious improvement and is deliberately not taken: it puts a direct
// tea.Println in this file, which TestOnlyPrintedHandsABatchToTheTerminal
// forbids for a reason that outranks the tidiness. Written down, the cost is
// bounded anyway: a rename over there makes every test using this fail with
// nothing printed, which is the safe direction to fail in.
const printMessage = "tea.printLineMessage"

// printedBy is what a command hands to the terminal before it does anything
// else, styling stripped.
//
// It stops at the first message that is not a printed line, and the stopping
// is the whole point rather than a limitation. The property being tested is an
// ordering — what the client has already said reaches the screen before the
// command that waits on the model, not merely somewhere in the same tree. An
// earlier version of this stepped over the wait and collected the print behind
// it, which passed just as happily on the bug as on the fix.
//
// Sequences are followed, because that is where the ordering lives. A batch is
// where it stops for a second reason too: the one this meets holds waitFor,
// and entering it would make the test the thing that waits for the answer.
//
// A bare blocking command sequenced after a print — which is what consume
// returns — is run, and returns whatever it returns; give the model a closed
// results channel first, the way the tool-line test does, or it waits on a
// channel nobody sends to.
func printedBy(cmd tea.Cmd) string {
	var said []string

	stopped := false
	var walk func(tea.Cmd)
	walk = func(cmd tea.Cmd) {
		if cmd == nil || stopped {
			return
		}
		message := cmd()

		if fmt.Sprintf("%T", message) == printMessage {
			said = append(said, fmt.Sprint(message))
			return
		}
		sequence, ordered := sequenced(message)
		if !ordered {
			stopped = true
			return
		}
		for _, next := range sequence {
			walk(next)
		}
	}
	walk(cmd)

	return visible(strings.Join(said, "\n"))
}

// sequenced is tea.Sequence's own message as the commands it will run, in
// order. That type is the library's own and unexported, so it is recognised by
// shape instead: a slice of commands that is not the exported BatchMsg.
func sequenced(message tea.Msg) ([]tea.Cmd, bool) {
	if _, batched := message.(tea.BatchMsg); batched {
		return nil, false
	}
	list := reflect.ValueOf(message)
	if list.Kind() != reflect.Slice {
		return nil, false
	}

	cmds := make([]tea.Cmd, 0, list.Len())
	for i := range list.Len() {
		cmd, ok := list.Index(i).Interface().(tea.Cmd)
		if !ok {
			return nil, false
		}
		cmds = append(cmds, cmd)
	}
	return cmds, true
}

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
	if len(m.unprinted) == 0 {
		return nil
	}
	said := make([]string, 0, len(m.unprinted))
	for _, line := range m.unprinted[1:] {
		said = append(said, visible(line))
	}
	return said
}
