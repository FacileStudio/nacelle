package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadNumbersItsLines(t *testing.T) {
	set := newSet(t, map[string]string{"a.txt": "one\ntwo\nthree\n"})

	out, err := call(t, set, "read_file", readInput{Path: "a.txt"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "     1\tone") || !strings.Contains(out, "     3\tthree") {
		t.Errorf("output has no line numbers:\n%s", out)
	}

	windowed, err := call(t, set, "read_file", readInput{Path: "a.txt", Offset: 2, Limit: 1})
	if err != nil {
		t.Fatalf("windowed read: %v", err)
	}
	if !strings.Contains(windowed, "two") || strings.Contains(windowed, "three") {
		t.Errorf("offset and limit were not honoured:\n%s", windowed)
	}
}

// The uniqueness rule is this tool's whole safety property: zero matches means
// the model misremembered the file, more than one means it is about to change
// places it never looked at.
func TestEditRequiresExactlyOneMatch(t *testing.T) {
	cases := map[string]struct {
		content, old string
		wants        string
	}{
		"absent":    {"alpha\n", "beta", "not in the file"},
		"ambiguous": {"x\nx\n", "x", "appears 2 times"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			set := newSet(t, map[string]string{"f.txt": tc.content})
			if _, err := call(t, set, "read_file", readInput{Path: "f.txt"}); err != nil {
				t.Fatalf("read: %v", err)
			}
			_, err := call(t, set, "edit_file", editInput{Path: "f.txt", Old: tc.old, New: "y"})
			if err == nil {
				t.Fatal("the edit was accepted")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wants)
			}
		})
	}
}

func TestEditReplacesAUniqueMatch(t *testing.T) {
	set := newSet(t, map[string]string{"f.txt": "keep\nchange me\nkeep\n"})
	if _, err := call(t, set, "read_file", readInput{Path: "f.txt"}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := call(t, set, "edit_file", editInput{Path: "f.txt", Old: "change me", New: "changed"}); err != nil {
		t.Fatalf("edit: %v", err)
	}

	after, err := os.ReadFile(filepath.Join(set.Dir(), "f.txt"))
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if string(after) != "keep\nchanged\nkeep\n" {
		t.Errorf("file = %q, want only the unique match replaced", after)
	}
}

// An agent that rewrites a file it never read is working from a plausible
// reconstruction of it.
func TestEditRefusesAFileThatWasNeverRead(t *testing.T) {
	set := newSet(t, map[string]string{"f.txt": "hello\n"})
	_, err := call(t, set, "edit_file", editInput{Path: "f.txt", Old: "hello", New: "bye"})
	if err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("edit without a read = %v, want a refusal telling the model to read first", err)
	}
}

// Whitespace is the usual reason an edit misses, and "not found" sends the
// model round the same loop.
func TestEditExplainsAWhitespaceMiss(t *testing.T) {
	_, err := replaceOnce("func main() {\n\tprintln(1)\n}\n", "func main() {\n    println(1)\n}", "x")
	if err == nil || !strings.Contains(err.Error(), "whitespace") {
		t.Fatalf("error = %v, want it to point at the whitespace", err)
	}
}
