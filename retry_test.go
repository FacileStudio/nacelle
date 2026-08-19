package nacelle_test

import (
	"context"
	"errors"
	"iter"
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

var done = nacelle.Event{Kind: nacelle.KindDone}

// A rate limit answered before the model said anything costs nothing to try
// again, and is the failure this wrapper exists for.
func TestATransientFailureBeforeAnyEventIsTriedAgain(t *testing.T) {
	backend := &flaky{runs: []run{
		{err: nacelle.Transient(errors.New("rate limited"))},
		{events: []nacelle.Event{done}},
	}}

	seen, err := collect(t, nacelle.Retry(backend, impatient()))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if backend.calls != 2 {
		t.Errorf("calls = %d, want 2", backend.calls)
	}
	if len(seen) != 1 || seen[0].Kind != nacelle.KindDone {
		t.Errorf("events = %v, want one KindDone", seen)
	}
}

// Once a delta has reached the consumer it has been printed, and no wrapper
// can un-print it. Starting again would replay the answer from the top.
func TestAFailureAfterAnEventEndsTheRun(t *testing.T) {
	spoken := nacelle.Event{Kind: nacelle.KindText, Text: "half an answer"}
	backend := &flaky{runs: []run{
		{events: []nacelle.Event{spoken}, err: nacelle.Transient(errors.New("dropped"))},
		{events: []nacelle.Event{done}},
	}}

	seen, err := collect(t, nacelle.Retry(backend, impatient()))
	if err == nil {
		t.Fatal("err = nil, want the failure to surface")
	}
	if backend.calls != 1 {
		t.Errorf("calls = %d, want 1", backend.calls)
	}
	if len(seen) != 1 || seen[0].Text != "half an answer" {
		t.Errorf("events = %v, want the one text delta", seen)
	}
}

// A bad request is bad however many times it is sent. Retrying it turns a
// clear failure into a slow one.
func TestAPermanentFailureIsNotRetried(t *testing.T) {
	refused := errors.New("invalid request")
	backend := &flaky{runs: []run{{err: refused}}}

	if _, err := collect(t, nacelle.Retry(backend, impatient())); !errors.Is(err, refused) {
		t.Errorf("err = %v, want the original", err)
	}
	if backend.calls != 1 {
		t.Errorf("calls = %d, want 1", backend.calls)
	}
}

func TestRetryingGivesUpAtTheAttemptLimit(t *testing.T) {
	overloaded := errors.New("overloaded")
	backend := &flaky{runs: []run{{err: nacelle.Transient(overloaded)}}}

	if _, err := collect(t, nacelle.Retry(backend, impatient())); !errors.Is(err, overloaded) {
		t.Errorf("err = %v, want the original to survive wrapping", err)
	}
	if backend.calls != 3 {
		t.Errorf("calls = %d, want 3", backend.calls)
	}
}

// Attempts of one is how a caller turns retrying off, and it must not be
// mistaken for an unset field and replaced with the default.
func TestOneAttemptDisablesRetrying(t *testing.T) {
	backend := &flaky{runs: []run{{err: nacelle.Transient(errors.New("overloaded"))}}}
	options := nacelle.RetryOptions{Attempts: 1, Base: time.Millisecond, Max: time.Millisecond}

	if _, err := collect(t, nacelle.Retry(backend, options)); err == nil {
		t.Fatal("err = nil, want the failure to surface")
	}
	if backend.calls != 1 {
		t.Errorf("calls = %d, want 1", backend.calls)
	}
}

// The wrapper must not rename the backend: an error saying "retrying" instead
// of "openrouter" tells the reader about our plumbing and not their problem.
func TestRetryKeepsTheBackendsIdentity(t *testing.T) {
	wrapped := nacelle.Retry(&flaky{runs: []run{{}}}, impatient())

	if wrapped.Name() != "flaky" {
		t.Errorf("name = %q, want %q", wrapped.Name(), "flaky")
	}
}
