package nacelle

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Transient marks an error as a failure worth retrying.
//
// Backends use it to promote the transient errors only they can recognise.
// Classifying a provider's own error vocabulary needs the vocabulary, which is
// exactly the knowledge a backend has and the core deliberately does not.
func Transient(err error) error {
	if err == nil {
		return nil
	}
	return transient{error: err}
}

// transient is what Transient returns. It answers a method rather than
// carrying a bare marker so that a caller with an error type of its own can
// join the scheme by implementing the same one.
type transient struct {
	error
}

func (transient) Retryable() bool { return true }

func (t transient) Unwrap() error { return t.error }

// exhausted is the failure Retry surfaces once it has stopped starting the run
// over, whether it ran out of attempts or the context ended during a backoff.
//
// It exists to answer Retryable() false. The transient marker that earned the
// run its retries is still down on the cause, where errors.As would find it,
// and a caller with a retry layer of its own then starts the same doomed run
// again: three attempts nested under three attempts is nine calls to a
// provider that already said no three times, reported as three. Sitting in
// front of the cause rather than replacing it keeps errors.Is working on what
// actually went wrong, and carries the attempt number Attempt reports.
type exhausted struct {
	error

	attempt int
}

func (exhausted) Retryable() bool { return false }

func (e exhausted) Unwrap() error { return e.error }

func (e exhausted) Attempt() int { return e.attempt }

// Retryable reports whether err is worth starting a run again for.
//
// It is true for anything marked by Transient, for any error implementing
// Retryable() bool, and for a truncated response — a stream that stops
// mid-body is the shape a dropped connection takes once the status line has
// already been read and the request counted as a success.
func Retryable(err error) bool {
	var known interface{ Retryable() bool }
	if errors.As(err, &known) {
		return known.Retryable()
	}
	return errors.Is(err, io.ErrUnexpectedEOF)
}

// Attempt reports which attempt an error was recorded on, or zero for an error
// nothing retried.
//
// A backend cannot fill this in, because it does not know how many times its
// stream has been started — only Retry does, and it stamps the number on the
// failure it finally surfaces. It is exposed so a consumer can say "gave up
// after three tries" instead of reporting a single anonymous failure, which is
// the difference between a run that limped and one that sailed through.
func Attempt(err error) int {
	var counted interface{ Attempt() int }
	if errors.As(err, &counted) {
		return counted.Attempt()
	}
	return 0
}

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
