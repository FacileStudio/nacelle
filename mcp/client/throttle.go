package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// maxRetryAfter caps the wait a server can ask for, so a Retry-After of an
// hour cannot hold a caller's goroutine for an hour.
const maxRetryAfter = 30 * time.Second

// backoffs are the waits between sends when a refusal names no Retry-After,
// one per retry, which also bounds the retries.
var backoffs = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// sleep waits out a delay unless ctx ends first. Tests replace it.
var sleep = pause

// RateLimitError is a server refusing a request before handling it: a 429,
// or a 503 that names a Retry-After. The go-sdk transport reports both as a
// bare status text and drops the header, so this is the only place a caller
// learns how long the server asked it to wait.
//
// It is a refusal, not an outage: the connection stays usable. Connect,
// tools/list and the round carrying a person's answer are already retried
// here; a first tools/call never is, and [RateLimitError.Retryable] says
// whether the caller may send it again.
type RateLimitError struct {
	Server string
	Method string
	Status int

	// RetryAfter is the wait the server asked for, capped at 30 seconds,
	// or zero when it named none.
	RetryAfter time.Duration

	// Retries is how many times this package already sent the request
	// again before giving up.
	Retries int

	answered bool
}

// Error says who refused what and how long they asked for.
func (e *RateLimitError) Error() string {
	wait := "it named no wait"
	if e.RetryAfter > 0 {
		wait = "it asks to wait " + e.RetryAfter.String()
	}
	return fmt.Sprintf("nacelle/mcp/client: server %q refused %s with %d %s; %s",
		e.Server, e.Method, e.Status, http.StatusText(e.Status), wait)
}

// Retryable reports whether the caller may send the same call again. It is
// false once this package has retried it, since a second retry layer
// multiplies what a refusing server receives; for the round carrying a
// person's answer, since sending the call again asks the person again; and
// for a tools/call refused with 503, which a proxy can send after the tool
// ran. A 429 on a first tools/call is retryable: the server refused it before
// running anything.
func (e *RateLimitError) Retryable() bool {
	return e.Retries == 0 && !e.answered && (e.Method != "tools/call" || e.Status == http.StatusTooManyRequests)
}

// throttled sends again what is safe to send again after a [RateLimitError],
// honouring Retry-After, and returns the refusal it gives up on as the error.
// Returning it from RoundTrip is what keeps it: the SDK wraps a transport
// error with %w, where it reads a refused response into a status text.
type throttled struct {
	base   http.RoundTripper
	server string
}

// RoundTrip sends the request, and again after a refusal while [again]
// allows it.
func (t throttled) RoundTrip(request *http.Request) (*http.Response, error) {
	sent := request
	for retries := 0; ; retries++ {
		response, err := t.base.RoundTrip(sent)
		if err != nil {
			return response, err
		}
		refusal := refused(response, t.server)
		if refusal == nil {
			return response, nil
		}
		if err := drain(response.Body); err != nil {
			return nil, errors.Join(refusal, err)
		}
		if sent = again(request, refusal, retries); sent == nil {
			return nil, refusal
		}
	}
}

// again names the refused call on refusal, waits as long as it asks, and
// returns the copy of request to send next. It returns nil, without waiting,
// when the method may not be sent again, the retries are spent or the wait
// would outlast the request's deadline.
func again(request *http.Request, refusal *RateLimitError, retries int) *http.Request {
	call := peek(request)
	refusal.Method, refusal.Retries, refusal.answered = call.Method, retries, call.resumes()
	wait, ok := call.delay(refusal)
	if !ok || !fits(request.Context(), wait) {
		return nil
	}
	replay := rewind(request)
	if replay == nil || !sleep(request.Context(), wait) {
		return nil
	}
	return replay
}

// refused is the response as a [RateLimitError], or nil when it is not one.
func refused(response *http.Response, server string) *RateLimitError {
	wait, named := retryAfter(response.Header.Get("Retry-After"), time.Now())
	limited := response.StatusCode == http.StatusTooManyRequests ||
		(response.StatusCode == http.StatusServiceUnavailable && named)
	if !limited {
		return nil
	}
	return &RateLimitError{Server: server, Status: response.StatusCode, RetryAfter: wait}
}

// fits reports whether a wait ends before ctx's deadline, since a retry the
// deadline cuts short only delays the refusal the caller gets anyway.
func fits(ctx context.Context, wait time.Duration) bool {
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) > wait
}

// pause waits out a delay, reporting whether it finished before ctx ended.
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
