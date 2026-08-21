package nacelle

import (
	"context"
	"errors"
	"fmt"
)

// ErrRetryBudget is what a run ends with when RetryOptions.Budget ran out.
//
// It exists so the two ways a run can stop short stay tellable apart. Both
// arrive as a dead context and both read as context.DeadlineExceeded, but "the
// provider was down for longer than we were willing to wait" is this policy
// firing and "the caller stopped us" is theirs. A retry layer above this one,
// a message shown to a user and a metric all want to say something different
// about the two, and a bare deadline lets them say only one thing.
var ErrRetryBudget = errors.New("nacelle: the retry budget ran out")

// bounded derives the context a whole run happens under: the parent as it
// stands when there is no budget, and a deadline carrying ErrRetryBudget when
// there is.
//
// The deadline is taken once, before the first attempt, because a deadline
// passed down is the only bound that reaches the sleeps RetryOptions.Max
// cannot see. Both SDKs wait out a Retry-After in a select on ctx.Done()
// inside the attempt this wrapper then repeats, so a deadline interrupts a
// sleep that has already started. A stopwatch read between attempts would
// notice the six minutes only after they had been spent, which is the same
// six minutes.
//
// Zero hands the parent back untouched rather than a deadline far in the
// future, so a caller who asked for no budget keeps exactly the context they
// passed in.
func (o RetryOptions) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	if o.Budget <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeoutCause(ctx, o.Budget, ErrRetryBudget)
}

// outOfBudget reports how a run ends when the budget ran out, and nil for
// every other reason a context can be done.
//
// The question it asks is context.Cause and not ctx.Err, because our deadline
// and a caller's own both answer context.DeadlineExceeded, and blaming a
// five-minute policy for someone else's timeout sends the reader to the wrong
// knob. WithTimeoutCause stamps the reason on the context this wrapper
// derived, so only that one answers to it, and a parent cancelled underneath
// us falls through to abandoned where it belongs.
//
// It is surfaced as exhausted for the reason abandoned is: Retryable must be
// false, because a run that has spent a whole budget is the last run anything
// should start again. The provider's last failure and the deadline are both
// wrapped, so whichever of the two a caller asks about is there.
func (r retrying) outOfBudget(ctx context.Context, attempt int, failure error) error {
	if !errors.Is(context.Cause(ctx), ErrRetryBudget) {
		return nil
	}
	r.options.Logger.Error("nacelle: giving up on the retry budget",
		"backend", r.Name(), "attempt", attempt, "attempts", r.options.Attempts,
		"budget", r.options.Budget, "error", failure)
	return exhausted{
		error:   fmt.Errorf("%w after %s: %w (%w)", ErrRetryBudget, r.options.Budget, failure, ctx.Err()),
		attempt: attempt,
	}
}
