package nacelle_test

import (
	"bytes"
	"context"
	"iter"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

// flaky is a backend that plays a scripted list of runs, one per call, so a
// retry policy can be tested without a provider having a bad day.
type flaky struct {
	runs  []run
	calls int
}

// run is one scripted attempt: the events it produces before the error, if
// any, that ends it.
type run struct {
	events []nacelle.Event
	err    error
}

func (f *flaky) Name() string                       { return "flaky" }
func (f *flaky) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }

func (f *flaky) CountTokens(context.Context, nacelle.Request) (int64, error) { return 0, nil }

func (f *flaky) Stream(context.Context, nacelle.Request) iter.Seq2[nacelle.Event, error] {
	scripted := f.runs[min(f.calls, len(f.runs)-1)]
	f.calls++

	return func(yield func(nacelle.Event, error) bool) {
		for _, event := range scripted.events {
			if !yield(event, nil) {
				return
			}
		}
		if scripted.err != nil {
			yield(nacelle.Event{}, scripted.err)
		}
	}
}

// impatient retries three times with a delay short enough that the tests do
// not spend their lives asleep.
func impatient() nacelle.RetryOptions {
	return nacelle.RetryOptions{Attempts: 3, Base: time.Millisecond, Max: time.Millisecond}
}

// collect drains a wrapped backend and reports what the consumer saw.
func collect(t *testing.T, backend nacelle.Backend) ([]nacelle.Event, error) {
	t.Helper()

	var seen []nacelle.Event
	for event, err := range backend.Stream(context.Background(), nacelle.Request{}) {
		if err != nil {
			return seen, err
		}
		seen = append(seen, event)
	}
	return seen, nil
}

// logged drains a wrapped backend under a logger of its own and reports every
// line it wrote, so a test can assert on levels without touching the process
// logger every other test shares.
func logged(t *testing.T, backend nacelle.Backend) (string, error) {
	t.Helper()

	var recorded bytes.Buffer
	options := impatient()
	options.Logger = slog.New(slog.NewTextHandler(&recorded, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, err := collect(t, nacelle.Retry(backend, options))
	return recorded.String(), err
}

// lines reports how many logged lines were written at the given level.
func lines(recorded, level string) int {
	return strings.Count(recorded, "level="+level)
}

var done = nacelle.Event{Kind: nacelle.KindDone}
