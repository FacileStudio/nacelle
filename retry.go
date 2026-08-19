package nacelle

import (
	"context"
	"fmt"
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
			surfaced := r.afterFailure(ctx, attempt, produced, err)
			if surfaced != nil {
				yield(Event{}, surfaced)
				return
			}
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

// afterFailure decides what a failed attempt becomes: nil to start the run
// over, or the error the consumer should be handed instead.
//
// The two ways of stopping are kept apart here because they are not the same
// news. Running out of attempts is the provider having failed; a context that
// ended mid-backoff is the caller having asked us to stop, and reporting the
// second as the first blames a backend for a keystroke.
func (r retrying) afterFailure(ctx context.Context, attempt int, produced bool, err error) error {
	if produced || attempt >= r.options.Attempts || !Retryable(err) {
		return r.giveUp(attempt, err)
	}
	if !pause(ctx, r.options.backoff(attempt)) {
		return abandoned(ctx.Err(), err, attempt)
	}
	r.willRetry(attempt, err)
	return nil
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

// giveUp ends the run, reporting the failure that ended it and returning what
// the consumer should receive.
//
// A first attempt is handed back untouched and in silence. Nothing was
// retried, so this decorator has neither a log line nor an attempt number to
// add to the error the consumer is already holding, and a permanent refusal
// that arrived straight away must reach the caller exactly as the backend
// wrote it rather than twice over.
//
// Anything later is logged and stamped, a permanent failure that followed a
// transient one included. That run warns "retrying" and used to end in total
// silence with an attempt count of zero — a story that opens and never closes,
// and the only record that the first failure happened at all.
func (r retrying) giveUp(attempt int, err error) error {
	if attempt == 1 {
		return err
	}
	r.options.Logger.Error("nacelle: giving up after a transient failure",
		"backend", r.Name(), "attempt", attempt, "attempts", r.options.Attempts, "error", err)
	return exhausted{error: err, attempt: attempt}
}

// abandoned is the error a run ends with when the context ended while a
// backoff was still being waited out.
//
// The provider's failure is kept as a cause, because it is still why a retry
// was pending, but it must not be what the consumer reads first. Surfacing it
// alone made errors.Is(err, context.Canceled) false, left the error claiming
// to be retryable when the context will never be live again, and printed
// "giving up after a transient failure" at a user who had pressed Ctrl-C.
// Wrapping both puts the cancellation and the cause on one error, so whichever
// the caller asks about is there.
func abandoned(interrupted, failure error, attempt int) error {
	return exhausted{
		error:   fmt.Errorf("%w while waiting to retry: %w", interrupted, failure),
		attempt: attempt,
	}
}
