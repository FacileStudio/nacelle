package nacelle_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

// The first place a budget has to fire is the wrapper's own wait. An attempt
// that fails in a millisecond and a backoff of half a second is a run that is
// asleep for all but a moment of it, and Max never bounded that.
func TestABudgetSpentDuringABackoffEndsTheRun(t *testing.T) {
	overloaded := errors.New("overloaded")
	backend := &flaky{runs: []run{{err: nacelle.Transient(overloaded)}}}

	_, err := collect(t, nacelle.Retry(backend, budgeted(30*time.Millisecond)))
	if !errors.Is(err, nacelle.ErrRetryBudget) {
		t.Fatalf("err = %v, want the budget named", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the deadline to survive the wrapping", err)
	}
	if !errors.Is(err, overloaded) {
		t.Errorf("err = %v, want the provider's last failure kept as a cause", err)
	}
	if nacelle.Retryable(err) {
		t.Errorf("err = %v, want a run that spent its budget to stay given up on", err)
	}
	if attempt := nacelle.Attempt(err); attempt != 1 {
		t.Errorf("attempt = %d, want the one it died on", attempt)
	}
	if backend.calls != 1 {
		t.Errorf("calls = %d, want the budget to have stopped the second", backend.calls)
	}
}

// The sleeps this field exists for happen inside an attempt, where the wrapper
// sees neither an event nor a failure, only time passing. A deadline is what
// reaches them, and what comes back must still say whose deadline it was.
func TestABudgetSpentMidAttemptIsNotReportedAsCancellation(t *testing.T) {
	backend := &stalling{}

	_, err := collect(t, nacelle.Retry(backend, budgeted(30*time.Millisecond)))
	if !errors.Is(err, nacelle.ErrRetryBudget) {
		t.Fatalf("err = %v, want the budget named", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want our own deadline and not a cancellation", err)
	}
	if nacelle.Retryable(err) || nacelle.Attempt(err) != 1 {
		t.Errorf("err = %v, want a given-up run stamped with the attempt it died on", err)
	}
	if backend.calls != 1 {
		t.Errorf("calls = %d, want the budget to have stopped the second", backend.calls)
	}
}

// A budget must not make this wrapper claim every dead context as its own. The
// caller who pressed Ctrl-C is owed their own news, and asking ctx.Err()
// instead of context.Cause is exactly how they stop getting it.
func TestACancelledParentIsReportedAsCancellationAndNotAsTheBudget(t *testing.T) {
	backend := &flaky{runs: []run{{err: nacelle.Transient(errors.New("overloaded"))}}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(20*time.Millisecond, cancel)

	_, err := collectUnder(t, ctx, nacelle.Retry(backend, budgeted(time.Minute)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
	if errors.Is(err, nacelle.ErrRetryBudget) {
		t.Errorf("err = %v, want a keystroke blamed on the caller and not on a policy", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want no deadline reported for a run nothing timed out", err)
	}
}

// The same mistake in the other place the budget is asked about: an attempt
// the caller stopped mid-stream ends with a dead context too, and it is still
// not ours to claim.
func TestACancelledParentMidAttemptIsNotReportedAsTheBudget(t *testing.T) {
	backend := &stalling{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(20*time.Millisecond, cancel)

	_, err := collectUnder(t, ctx, nacelle.Retry(backend, budgeted(time.Minute)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
	if errors.Is(err, nacelle.ErrRetryBudget) {
		t.Errorf("err = %v, want a keystroke blamed on the caller and not on a policy", err)
	}
}

// Zero is the field's whole compatibility story, and it is two promises: no
// deadline of ours anywhere near the caller's context, and a run that retries
// exactly as it did before the field existed.
func TestAZeroBudgetImposesNoDeadline(t *testing.T) {
	backend := &flaky{runs: []run{
		{err: nacelle.Transient(errors.New("rate limited"))},
		{events: []nacelle.Event{done}},
	}}

	seen, err := collect(t, nacelle.Retry(backend, impatient()))
	if err != nil {
		t.Fatalf("err = %v, want the second attempt to succeed", err)
	}
	if backend.bounded {
		t.Error("the backend was handed a deadline, want the caller's context untouched")
	}
	if backend.calls != 2 || len(seen) != 1 {
		t.Errorf("calls = %d, events = %v, want the retry to have happened", backend.calls, seen)
	}
}

// A caller reaching past our prose for the provider's own error type must
// still find it. Reporting the budget is worth nothing if it costs the reader
// the 529 that explains why the provider was unreachable for that long.
func TestTheProvidersOwnErrorSurvivesTheBudgetError(t *testing.T) {
	backend := &flaky{runs: []run{{err: nacelle.Transient(providerFault{code: 529})}}}

	_, err := collect(t, nacelle.Retry(backend, budgeted(30*time.Millisecond)))

	var fault providerFault
	if !errors.As(err, &fault) {
		t.Fatalf("err = %v, want the provider's own error still reachable", err)
	}
	if fault.code != 529 {
		t.Errorf("code = %d, want the one the provider sent", fault.code)
	}
}

// A run that stopped at attempt one because of a budget looks, in a log that
// says nothing, exactly like a run that never started. Giving up on the budget
// is logged for the same reason giving up on the attempts is, and names the
// figure that did it so the reader knows which knob to turn.
func TestGivingUpOnTheBudgetIsLoggedAtErrorNamingTheBudget(t *testing.T) {
	backend := &flaky{runs: []run{{err: nacelle.Transient(errors.New("overloaded"))}}}

	recorded, err := loggedWith(t, backend, budgeted(30*time.Millisecond))
	if err == nil {
		t.Fatal("err = nil, want the budget to surface")
	}
	if count := lines(recorded, "ERROR"); count != 1 {
		t.Errorf("errors = %d, want exactly one\n%s", count, recorded)
	}
	for _, want := range []string{"budget=30ms", "backend=flaky", "attempt=1", "overloaded"} {
		if !strings.Contains(recorded, want) {
			t.Errorf("log = %q, want it to carry %q", recorded, want)
		}
	}
}
