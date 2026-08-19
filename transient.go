package nacelle

import (
	"errors"
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

	attempt int
}

func (transient) Retryable() bool { return true }

func (t transient) Unwrap() error { return t.error }

func (t transient) Attempt() int { return t.attempt }

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

// onAttempt records which attempt a failure happened on.
//
// It refuses anything not already retryable rather than marking it, because
// the wrapper it returns answers Retryable() true: stamping a bad request
// would turn a permanent refusal into something the next layer starts over.
func onAttempt(err error, attempt int) error {
	if !Retryable(err) {
		return err
	}
	return transient{error: err, attempt: attempt}
}
