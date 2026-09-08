package nacelle_test

import (
	"bytes"
	"context"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

// flaky is a backend that plays a scripted list of runs, one per call, so a
// retry policy can be tested without a provider having a bad day.
//
// It also records whether the context it was handed carries a deadline, which
// is how a test tells a budget that was imposed from one that was not.
type flaky struct {
	runs    []run
	calls   atomic.Int64
	bounded bool
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

func (f *flaky) Stream(ctx context.Context, _ nacelle.Request) iter.Seq2[nacelle.Event, error] {
	scripted := f.runs[min(f.calls.Load(), int64(len(f.runs)-1))]
	f.calls.Add(1)
	_, f.bounded = ctx.Deadline()

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

// stalling is a backend that produces nothing and waits for the context to
// end. It is the shape of an attempt spent inside an SDK sleeping out a
// Retry-After: no event, no failure to classify, only time passing where the
// wrapper cannot see it.
type stalling struct {
	calls atomic.Int64
}

func (s *stalling) Name() string                       { return "stalling" }
func (s *stalling) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }

func (s *stalling) CountTokens(context.Context, nacelle.Request) (int64, error) { return 0, nil }

func (s *stalling) Stream(ctx context.Context, _ nacelle.Request) iter.Seq2[nacelle.Event, error] {
	s.calls.Add(1)

	return func(yield func(nacelle.Event, error) bool) {
		<-ctx.Done()
		yield(nacelle.Event{}, ctx.Err())
	}
}

// providerFault is a backend's own error type, the shape a caller reaches for
// with errors.As when it wants the provider's code rather than our prose.
type providerFault struct {
	code int
}

func (p providerFault) Error() string { return fmt.Sprintf("provider said %d", p.code) }

// impatient retries three times with a delay short enough that the tests do
// not spend their lives asleep.
func impatient() nacelle.RetryOptions {
	return nacelle.RetryOptions{Attempts: 3, Base: time.Millisecond, Max: time.Millisecond}
}

// budgeted is impatient's opposite: a backoff long enough that the budget is
// always what runs out first, which is where a real run spends its time too.
func budgeted(budget time.Duration) nacelle.RetryOptions {
	return nacelle.RetryOptions{
		Attempts: 3,
		Base:     500 * time.Millisecond,
		Max:      500 * time.Millisecond,
		Budget:   budget,
	}
}

// collectUnder runs a wrapped backend under the given context and reports what the
// consumer saw, so a test can stop a run the way a caller would.
func collectUnder(t *testing.T, ctx context.Context, backend nacelle.Backend) ([]nacelle.Event, error) {
	t.Helper()

	var seen []nacelle.Event
	for event, err := range backend.Stream(ctx, nacelle.Request{}) {
		if err != nil {
			return seen, err
		}
		seen = append(seen, event)
	}
	return seen, nil
}

// collect drains a wrapped backend that nothing is going to interrupt.
func collect(t *testing.T, backend nacelle.Backend) ([]nacelle.Event, error) {
	t.Helper()

	return collectUnder(t, context.Background(), backend)
}

// loggedWith drains a wrapped backend under a logger of its own and reports
// every line it wrote, so a test can assert on levels without touching the
// process logger every other test shares.
func loggedWith(t *testing.T, backend nacelle.Backend, options nacelle.RetryOptions) (string, error) {
	t.Helper()

	var recorded bytes.Buffer
	options.Logger = slog.New(slog.NewTextHandler(&recorded, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, err := collect(t, nacelle.Retry(backend, options))
	return recorded.String(), err
}

// logged is loggedWith under the policy most of these tests share.
func logged(t *testing.T, backend nacelle.Backend) (string, error) {
	t.Helper()

	return loggedWith(t, backend, impatient())
}

// lines reports how many logged lines were written at the given level.
func lines(recorded, level string) int {
	return strings.Count(recorded, "level="+level)
}

var done = nacelle.Event{Kind: nacelle.KindDone}

// concurrencyTracker records wall time of each Stream call and checks that
// no two calls overlap, verifying that the concurrency semaphore is enforced.
type concurrencyTracker struct {
	mu     sync.Mutex
	starts []time.Time
	ends   []time.Time
	// overlaps[i] = true if call i overlaps with call i+1
	overlaps []bool
}

func (c *concurrencyTracker) Name() string                       { return "concurrencyTracker" }
func (c *concurrencyTracker) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }
func (c *concurrencyTracker) CountTokens(context.Context, nacelle.Request) (int64, error) {
	return 0, nil
}

func (c *concurrencyTracker) Stream(ctx context.Context, _ nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		select {
		case <-ctx.Done():
			c.mu.Lock()
			c.ends = append(c.ends, time.Now())
			c.mu.Unlock()
			yield(nacelle.Event{}, ctx.Err())
			return
		default:
		}

		c.mu.Lock()
		idx := len(c.starts)
		start := time.Now()
		end := time.Now()
		c.starts = append(c.starts, start)
		c.ends = append(c.ends, end)
		c.overlaps = append(c.overlaps, false)
		if idx > 0 && start.Before(c.ends[idx-1]) {
			c.overlaps[idx-1] = true
		}
		c.mu.Unlock()

		for _, event := range []nacelle.Event{{Kind: nacelle.KindText, Text: "ok"}} {
			if !yield(event, nil) {
				return
			}
		}
	}
}
