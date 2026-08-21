package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// pieces is the batches one printed() call will hand the terminal, in order.
// A batch that fits comes back as a single Println and a taller one as a
// sequence of them, and the sequence type is the library's own and
// unexported — reflection is what reads it from out here.
func pieces(t *testing.T, cmd tea.Cmd) []string {
	t.Helper()
	if cmd == nil {
		return nil
	}

	msg := cmd()
	value := reflect.ValueOf(msg)
	if value.Kind() != reflect.Slice {
		return []string{visible(fmt.Sprint(msg))}
	}

	out := make([]string, 0, value.Len())
	for i := range value.Len() {
		out = append(out, pieces(t, value.Index(i).Interface().(tea.Cmd))...)
	}
	return out
}

// tea.Println scrolls the screen by however many rows it is about to write,
// and rows the frame is standing on go with them — into the scrollback, where
// the renderer never reaches again. A batch taller than the free rows has to
// be cut, or the reader finds the raw live region and a second prompt up
// there above the rendered answer.
func TestATallBatchIsCutIntoPiecesTheFrameSurvives(t *testing.T) {
	m := sized()
	m.frameRows = m.windowHeight - 4

	lines := make([]string, 14)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i)
	}

	batches := pieces(t, m.printed(strings.Join(lines, "\n")))
	if len(batches) < 2 {
		t.Fatalf("printed %d batches, want a 14-row transcript cut to fit %d rows", len(batches), m.budget())
	}
	for i, batch := range batches {
		if rows := len(strings.Split(batch, "\n")); rows > m.budget() {
			t.Errorf("batch %d is %d rows, want no more than the %d the frame left free", i, rows, m.budget())
		}
	}

	joined := strings.Join(batches, "\n")
	if first, last := strings.Index(joined, "line 00"), strings.Index(joined, "line 13"); first < 0 || last < first {
		t.Errorf("printed = %q, want every line, in the order it was said", joined)
	}
}

// A batch that fits stays one message. Order is the only reason the cutting
// exists, and there is no order to keep in a sequence of one.
func TestABatchThatFitsStaysOneMessage(t *testing.T) {
	m := sized()

	if got := len(pieces(t, m.printed("one\ntwo\nthree"))); got != 1 {
		t.Errorf("printed %d batches, want the one the terminal can take in a single go", got)
	}
}

// The budget is rows on screen, not lines of text. A line wider than the
// window scrolls the screen by every width it overruns, which is the
// arithmetic bubbletea itself does before writing the newlines.
func TestAWrappedLineCostsTheRowsItWrapsOnto(t *testing.T) {
	m := sized()
	m.frameRows = m.windowHeight - 4

	if got := m.fits([]string{strings.Repeat("x", 3*m.width), "the next line"}); got != 1 {
		t.Errorf("fits = %d, want the wrapped line alone to spend the whole budget", got)
	}
}

// A line too tall for the entire budget still has to be printed. The frame
// losing a row is worse than nothing, and a client that stops saying anything
// is worse than that.
func TestALineTooTallForTheBudgetStillGoesOut(t *testing.T) {
	m := sized()
	m.frameRows = m.windowHeight

	if got := m.fits([]string{strings.Repeat("x", 3*m.width)}); got != 1 {
		t.Errorf("fits = %d, want the oversized line printed anyway", got)
	}
}

// The frame is not allowed to stand on every row of the window, because a
// frame with nothing free above it cannot be printed over at all.
func TestTheFrameNeverFillsTheWindow(t *testing.T) {
	m := sized()
	frame := m.liveRows + 2 + m.prompt.Height() + m.menu.height() + m.queuedHeight()

	if frame >= m.windowHeight {
		t.Errorf("frame is %d rows of a %d-row window, want room left for a printed batch", frame, m.windowHeight)
	}
}

// /clear pushes a whole window of blank lines to scroll the finished session
// out of sight, which is the one batch guaranteed to be taller than the frame
// leaves room for. It goes through the same cutting as everything else, and
// the reason is not symmetry: skipping it left the frame stranded, and because
// the frame after a clear is identical to the frame before one, the renderer
// saw no change and never repainted the rows it had lost — the prompt's "> "
// and the status line's "ready · " simply stayed blank.
func TestTheBlankRunClearPushesIsCutLikeEveryOtherBatch(t *testing.T) {
	m := sized()
	m.say(fromReader, "/clear")

	m.View()
	batches := pieces(t, m.clear())
	if len(batches) < 2 {
		t.Fatalf("printed %d batches, want a window of blank lines cut to fit %d rows", len(batches), m.budget())
	}
	for i, batch := range batches {
		if rows := len(strings.Split(batch, "\n")); rows > m.budget() {
			t.Errorf("batch %d is %d rows, want no more than the %d the frame left free", i, rows, m.budget())
		}
	}
}

// printed is the only thing that knows how tall a Println may be, so it has to
// be the only thing that calls one. A direct tea.Println anywhere else is a
// print with no budget, and what it costs is invisible until someone scrolls
// up and finds the frame up there — which is exactly how /clear kept its own
// artifact through the first pass of this fix.
//
// The sources are read rather than the behaviour exercised, because a
// behavioural test only ever covers the path it walks and this has to cover
// every call site at once. Parsed rather than grepped: a doc comment naming
// tea.Println is not a call, and this file is full of them.
func TestOnlyPrintedHandsABatchToTheTerminal(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the client's own sources: %v", err)
	}

	set := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || name == "headroom.go" {
			continue
		}
		parsed, err := parser.ParseFile(set, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		if printsDirectly(parsed) {
			t.Errorf("%s calls tea.Println directly, want it handed to printed so it gets a budget", name)
		}
	}
}

// printsDirectly reports whether one source file calls tea.Println itself.
func printsDirectly(file *ast.File) bool {
	var found bool
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Println" {
			return true
		}
		pkg, isIdent := selector.X.(*ast.Ident)
		found = found || (isIdent && pkg.Name == "tea")
		return true
	})
	return found
}

// Closing the dropdown and emptying the prompt hands their rows back to the
// live region, so a budget worked out from what the frame is *allowed* to be
// jumps the moment they go — while the screen still shows the tall frame,
// which has not been repainted yet. Measured at a 24-row window: 12 rows of
// budget against a frame that still leaves 8, which is four rows of frame in
// the scrollback. The budget has to come from the frame that was drawn.
func TestTheBudgetFollowsTheFrameOnScreenNotTheOnePlanned(t *testing.T) {
	m := sized()
	m.run.answer.WriteString(strings.Repeat("a streamed line\n", 40))
	m.prompt.SetValue(strings.Repeat("a typed line\n", 12))
	m.menu.filtered = m.menu.items
	m.layout(m.windowHeight)
	m.View()

	drawn, free := m.frameRows, m.budget()

	m.prompt.Reset()
	m.menu.filtered = nil
	m.layout(m.windowHeight)

	if got := m.budget(); got != free {
		t.Errorf("budget = %d once the prompt and the dropdown gave their rows back, want the %d a %d-row frame leaves in a %d-row window", got, free, drawn, m.windowHeight)
	}
}
