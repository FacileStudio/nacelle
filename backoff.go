package nacelle

import (
	"context"
	"math/rand/v2"
	"time"
)

// backoff is how long to wait after the given attempt: Base doubled per
// failure, capped at Max, minus up to a quarter as jitter.
//
// The jitter is the one place this package is deliberately not deterministic.
// It exists so a fleet of agents rate-limited by the same provider at the same
// instant does not march back in step, and it changes when a request is sent
// rather than what is sent, so it cannot move a model's output.
func (o RetryOptions) backoff(attempt int) time.Duration {
	delay := o.Base
	for range attempt - 1 {
		delay *= 2
		if delay >= o.Max {
			return jittered(o.Max)
		}
	}
	return jittered(delay)
}

func jittered(delay time.Duration) time.Duration {
	spread := delay / 4
	if spread <= 0 {
		return delay
	}
	return delay - rand.N(spread)
}

// pause waits out a backoff, reporting whether it finished rather than the
// context ending first.
func pause(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
