package nacelle_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

// interrupted runs a backend that always fails transiently under a context
// that has already ended, so pause abandons its first wait without the test
// racing a timer. It reports what was logged alongside what the consumer saw.
func interrupted(t *testing.T, cause error) (string, error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var recorded bytes.Buffer
	options := impatient()
	options.Base, options.Max = time.Minute, time.Minute
	options.Logger = slog.New(slog.NewTextHandler(&recorded, nil))

	var failure error
	backend := nacelle.Retry(&flaky{runs: []run{{err: nacelle.Transient(cause)}}}, options)
	for _, err := range backend.Stream(ctx, nacelle.Request{}) {
		failure = err
	}
	return recorded.String(), failure
}

// A stream that stops mid-body is a dropped connection dressed as a success:
// the status line was read long before, so no HTTP retry sees it. This is the
// only classification the core makes on its own, and replacing it with a flat
// false left every test in the package green.
func TestATruncatedResponseIsRetryableWithNoMarkerOnIt(t *testing.T) {
	truncated := fmt.Errorf("reading the stream: %w", io.ErrUnexpectedEOF)

	if !nacelle.Retryable(truncated) {
		t.Error("a truncated response was not worth retrying; a dropped connection reaches us in no other shape")
	}
	if nacelle.Retryable(errors.New("invalid request")) {
		t.Error("an unmarked error was retryable, which turns every bad request into a slow one")
	}
}

// A caller wrapping its own retry around ours multiplies if the error we give
// up with still claims to be retryable: three attempts under three attempts is
// nine calls to a provider that has already refused three times, reported to
// the consumer as three.
func TestTheErrorRetryGivesUpWithStopsClaimingToBeRetryable(t *testing.T) {
	overloaded := errors.New("overloaded")
	backend := &flaky{runs: []run{{err: nacelle.Transient(overloaded)}}}

	_, err := collect(t, nacelle.Retry(nacelle.Retry(backend, impatient()), impatient()))
	if nacelle.Retryable(err) {
		t.Error("the exhausted error still says retryable, so the layer above starts the same doomed run over")
	}
	if backend.calls != 3 {
		t.Errorf("calls = %d, want 3 rather than the attempt limit squared", backend.calls)
	}
	if attempt := nacelle.Attempt(err); attempt != 3 {
		t.Errorf("attempt = %d, want the count to survive giving up", attempt)
	}
	if !errors.Is(err, overloaded) {
		t.Errorf("err = %v, want the provider's cause still reachable", err)
	}
}

// Pressing Ctrl-C is not the provider's fault. The consumer used to receive
// the backend's last error instead, so errors.Is could not see the
// cancellation and the error still offered itself for another try.
func TestACancelledBackoffSurfacesTheCancellationAndKeepsTheCause(t *testing.T) {
	overloaded := errors.New("overloaded")
	_, err := interrupted(t, overloaded)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want a cancellation the caller can recognise", err)
	}
	if !errors.Is(err, overloaded) {
		t.Errorf("err = %v, want the pending failure kept as the cause", err)
	}
	if nacelle.Retryable(err) {
		t.Error("a run stopped by a dead context offered itself for another attempt")
	}
}

// Reporting a keystroke as a provider outage teaches the reader to distrust
// the level: nothing went wrong, and the run ended because it was asked to.
func TestACancelledBackoffIsNotLoggedAsGivingUpOnTheProvider(t *testing.T) {
	recorded, _ := interrupted(t, errors.New("overloaded"))

	if lines(recorded, "ERROR") != 0 {
		t.Errorf("log = %q, want no provider blamed for a cancelled context", recorded)
	}
}

// A run that warned "retrying" and then hit a permanent refusal used to end in
// silence with an attempt count of zero: a story that opens and never closes,
// and an error contradicting Attempt's own documentation.
func TestAPermanentFailureAfterARetryIsBothLoggedAndCounted(t *testing.T) {
	refused := errors.New("invalid request")
	backend := &flaky{runs: []run{
		{err: nacelle.Transient(errors.New("overloaded"))},
		{err: refused},
	}}

	recorded, err := logged(t, backend)
	if lines(recorded, "WARN") != 1 || lines(recorded, "ERROR") != 1 {
		t.Errorf("log = %q, want the retry warned and the run it ended reported", recorded)
	}
	if attempt := nacelle.Attempt(err); attempt != 2 {
		t.Errorf("attempt = %d, want the two tries it actually took", attempt)
	}
	if !errors.Is(err, refused) {
		t.Errorf("err = %v, want the refusal still reachable", err)
	}
}
