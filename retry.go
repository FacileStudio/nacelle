package nacelle

import (
	"context"
	"iter"
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
				yield(Event{}, err)
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

// again reports whether a failed attempt may be started over.
func (r retrying) again(attempt int, produced bool, err error) bool {
	return !produced && attempt < r.options.Attempts && Retryable(err)
}
