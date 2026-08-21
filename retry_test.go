package nacelle_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

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

	_, err := collect(t, nacelle.Retry(backend, impatient()))
	if !errors.Is(err, refused) {
		t.Errorf("err = %v, want the original", err)
	}
	if backend.calls != 1 {
		t.Errorf("calls = %d, want 1", backend.calls)
	}
	if nacelle.Attempt(err) != 0 || nacelle.Retryable(err) {
		t.Errorf("err = %v, want no attempt stamped on a failure nothing retried", err)
	}
}

// The failure that ends the run carries how many tries it took, because the
// consumer reporting it is rarely the one reading our logs — and it has to do
// that on an error errors.Is can still see the cause through.
func TestRetryingGivesUpAtTheAttemptLimit(t *testing.T) {
	overloaded := errors.New("overloaded")
	backend := &flaky{runs: []run{{err: nacelle.Transient(overloaded)}}}

	_, err := collect(t, nacelle.Retry(backend, impatient()))
	if !errors.Is(err, overloaded) {
		t.Errorf("err = %v, want the original to survive wrapping", err)
	}
	if backend.calls != 3 {
		t.Errorf("calls = %d, want 3", backend.calls)
	}
	if attempt := nacelle.Attempt(err); attempt != 3 {
		t.Errorf("attempt = %d, want the last one tried", attempt)
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

// A run that limped through two attempts and one that sailed through must not
// look the same afterwards, and the attempt number is what tells them apart.
func TestARetriedAttemptIsLoggedAtWarnWithItsNumberAndTheBackend(t *testing.T) {
	backend := &flaky{runs: []run{
		{err: nacelle.Transient(errors.New("rate limited"))},
		{events: []nacelle.Event{done}},
	}}

	recorded, err := logged(t, backend)
	if err != nil {
		t.Fatalf("err = %v, want the second attempt to succeed", err)
	}
	if count := lines(recorded, "WARN"); count != 1 {
		t.Errorf("warnings = %d, want one per retried attempt\n%s", count, recorded)
	}
	for _, want := range []string{"attempt=1", "backend=flaky", "rate limited"} {
		if !strings.Contains(recorded, want) {
			t.Errorf("log = %q, want it to carry %q", recorded, want)
		}
	}
	if lines(recorded, "ERROR") != 0 {
		t.Errorf("log = %q, want nothing at error for a run that recovered", recorded)
	}
}

// Giving up is the one thing here that deserves red, and exactly once: a
// stream that goes red on every provider hiccup is one people learn to scroll
// past, and one that goes red three times per failure is worse.
func TestGivingUpIsLoggedAtErrorExactlyOnceAfterWarningPerAttempt(t *testing.T) {
	backend := &flaky{runs: []run{{err: nacelle.Transient(errors.New("overloaded"))}}}

	recorded, err := logged(t, backend)
	if err == nil {
		t.Fatal("err = nil, want the failure to surface")
	}
	if count := lines(recorded, "ERROR"); count != 1 {
		t.Errorf("errors = %d, want exactly one\n%s", count, recorded)
	}
	if count := lines(recorded, "WARN"); count != 2 {
		t.Errorf("warnings = %d, want one per attempt that was tried again\n%s", count, recorded)
	}
	if !strings.Contains(recorded, "attempt=3") {
		t.Errorf("log = %q, want the attempt it gave up on", recorded)
	}
}

// A bad request was never a retry, so neither level applies: warning about it
// is a lie, and reporting it at error duplicates what the caller is already
// holding.
func TestAPermanentFailureIsLoggedAsNeitherARetryNorAGiveUp(t *testing.T) {
	backend := &flaky{runs: []run{{err: errors.New("invalid request")}}}

	recorded, _ := logged(t, backend)
	if recorded != "" {
		t.Errorf("log = %q, want silence for a failure nothing retried", recorded)
	}
}
