package nacelle

import (
	"testing"
	"time"
)

// wanted is one attempt and the delay it should wait, before jitter takes its
// slice off the top.
type wanted struct {
	attempt int
	delay   time.Duration
}

// check reports whether a delay landed in the band jitter leaves: at most the
// intended delay, and never more than a quarter under it.
func check(t *testing.T, options RetryOptions, want wanted) {
	t.Helper()

	got := options.backoff(want.attempt)
	if got > want.delay || got <= want.delay-want.delay/4 {
		t.Errorf("backoff(%d) = %v, want a jittered %v", want.attempt, got, want.delay)
	}
}

// Doubling is the whole point of a backoff, and every test in this package
// used Base and Max both at one millisecond, which flattens the curve into a
// constant: the delay is the same on attempt one and attempt five whether the
// doubling works, is off by one, or has been deleted.
func TestTheDelayDoublesForEveryAttemptThatFailed(t *testing.T) {
	options := RetryOptions{Base: 100 * time.Millisecond, Max: time.Hour}

	for _, want := range []wanted{
		{attempt: 1, delay: 100 * time.Millisecond},
		{attempt: 2, delay: 200 * time.Millisecond},
		{attempt: 3, delay: 400 * time.Millisecond},
		{attempt: 4, delay: 800 * time.Millisecond},
	} {
		check(t, options, want)
	}
}

// Max is documented to cap the delay however many attempts have failed, and
// the first attempt doubles nothing, so the cap was never reached: Max at
// 100ms with Base at three seconds measured a 2.79 second wait. A caller
// setting RetryOptions{Base: 30 * time.Second} reaches this with the default
// eight second cap and no idea it did.
func TestMaxCapsTheDelayIncludingBeforeAnythingHasDoubled(t *testing.T) {
	options := RetryOptions{Base: 3 * time.Second, Max: 100 * time.Millisecond}

	for attempt := 1; attempt <= 4; attempt++ {
		if delay := options.backoff(attempt); delay > options.Max {
			t.Errorf("backoff(%d) = %v, want no more than the %v cap", attempt, delay, options.Max)
		}
	}
	check(t, options, wanted{attempt: 1, delay: options.Max})
}
