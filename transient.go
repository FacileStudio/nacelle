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
	return transient{err}
}

// transient is what Transient returns. It answers a method rather than
// carrying a bare marker so that a caller with an error type of its own can
// join the scheme by implementing the same one.
type transient struct{ error }

func (transient) Retryable() bool { return true }

func (t transient) Unwrap() error { return t.error }

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
