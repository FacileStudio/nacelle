package nacelle

import (
	"context"
	"iter"
	"log/slog"
	"time"
)

// Retry defaults, applied to any RetryOptions field left at zero.
const (
	DefaultRetryAttempts = 3
	DefaultRetryBase     = 500 * time.Millisecond
	DefaultRetryMax      = 8 * time.Second
)

// RetryOptions tunes Retry. Every zero field takes its Default counterpart, so
// the zero value is the recommended policy rather than no policy.
type RetryOptions struct {
	// Attempts is how many times a run may be started, the first one
	// included. One disables retrying without removing the wrapper.
	Attempts int

	// Base is the delay before the second attempt. It doubles from there.
	Base time.Duration

	// Max caps the delay however many attempts have failed.
	Max time.Duration

	// Logger records the attempts nobody else can see. Defaults to
	// slog.Default(), matching Config.Logger, because a retry that says
	// nothing makes a run that limped through three attempts look exactly
	// like one that sailed through on the first.
	Logger *slog.Logger
}

// Retry wraps a backend so a run that fails before producing anything is
// started again.
//
// This is deliberately not a backoff engine, because the backends already sit
// on one. Both SDKs retry at the HTTP level — connection failures, 408, 409,
// 429 and 5xx — honouring Retry-After-Ms and Retry-After, and that already
// covers establishing a stream. Re-implementing it here would be a second,
// worse copy.
//
// What no HTTP-level retry can see is a provider that answers 200 and puts the
// failure in the body. OpenRouter reports a rate limit as an error object
// inside the SSE, and an Anthropic overloaded_error can arrive mid-stream on a
// response whose status was committed long before. Both reach a caller as a
// dead stream carrying a transient failure, and retrying those is what this
// adds.
//
// It retries only while nothing has been yielded. Once a consumer has seen a
// text delta it has already printed it, and no wrapper can un-print it, so a
// failure after the first event ends the run and is reported as it is.
func Retry(backend Backend, options RetryOptions) Backend {
	return retrying{Backend: backend, options: options.withDefaults()}
}

// retrying is the decorator Retry returns. Embedding the backend rather than
// wrapping it field by field keeps Name and Capabilities reporting the real
// backend, which is what an error message should say.
type retrying struct {
	Backend

	options RetryOptions
}

func (o RetryOptions) withDefaults() RetryOptions {
	if o.Attempts <= 0 {
		o.Attempts = DefaultRetryAttempts
	}
	if o.Base <= 0 {
		o.Base = DefaultRetryBase
	}
	if o.Max <= 0 {
		o.Max = DefaultRetryMax
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}

// Stream runs the backend, starting it again while it fails transiently
// without having produced anything.
func (r retrying) Stream(ctx context.Context, request Request) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		for attempt := 1; ; attempt++ {
			produced, err := r.once(ctx, request, yield)
			if err == nil {
				return
			}
			if !r.again(attempt, produced, err) || !pause(ctx, r.options.backoff(attempt)) {
				r.gaveUp(attempt, err)
				yield(Event{}, onAttempt(err, attempt))
				return
			}
			r.willRetry(attempt, err)
		}
	}
}

// once runs the backend a single time. It reports whether anything reached the
// consumer, and how the run ended: a nil error means the run finished or the
// consumer stopped, and in both cases there is nothing left to retry.
func (r retrying) once(ctx context.Context, request Request, yield func(Event, error) bool) (bool, error) {
	var produced bool
	for event, err := range r.Backend.Stream(ctx, request) {
		if err != nil {
			return produced, err
		}
		produced = true
		if !yield(event, nil) {
			return true, nil
		}
	}
	return produced, nil
}

// again reports whether a failed attempt may be started over.
func (r retrying) again(attempt int, produced bool, err error) bool {
	return !produced && attempt < r.options.Attempts && Retryable(err)
}

// willRetry records an attempt that is about to be started over.
//
// It is a warning and not an error because the run has not failed yet: a
// provider that is briefly overloaded costs a few hundred milliseconds and
// nothing else. A stream that goes red every time a backend hiccups is one
// people learn to scroll past, and then miss the outage.
func (r retrying) willRetry(attempt int, err error) {
	r.options.Logger.Warn("nacelle: retrying a transient failure",
		"backend", r.Name(), "attempt", attempt, "attempts", r.options.Attempts, "error", err)
}

// gaveUp records the failure that ended the run, and only when it ended one we
// had been retrying.
//
// A permanent refusal is not logged here at all: it was never a retry, the
// caller receives it unchanged, and reporting it twice teaches the reader that
// this package double-counts. Neither is a first attempt that could not be
// started over — nothing was retried, so this decorator has nothing to add to
// the error the consumer is already holding.
func (r retrying) gaveUp(attempt int, err error) {
	if attempt == 1 || !Retryable(err) {
		return
	}
	r.options.Logger.Error("nacelle: giving up after a transient failure",
		"backend", r.Name(), "attempt", attempt, "attempts", r.options.Attempts, "error", err)
}
