package openrouter

import (
	"context"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// failure runs the backend and returns however it ended.
func failure(t *testing.T, backend *Backend) error {
	t.Helper()

	for _, err := range backend.Stream(context.Background(), nacelle.Request{System: "s"}) {
		if err != nil {
			return err
		}
	}
	return nil
}

// OpenRouter sends "code": 429 as a number, not a string. A decoder that
// expects a string drops the field without failing the whole parse, so the
// classification quietly reads an empty code and calls a rate limit
// permanent — which is how a retry that looks implemented never fires.
func TestAnInBandRateLimitIsRetryable(t *testing.T) {
	const rateLimited = `data: {"id":"g","error":{"code":429,"message":"Rate limit exceeded","metadata":{"error_type":"rate_limit_exceeded"}},"choices":[{"index":0,"delta":{"content":""},"finish_reason":"error"}]}

data: [DONE]

`
	backend, _ := serve(t, rateLimited)

	err := failure(t, backend)
	if err == nil {
		t.Fatal("an in-band rate limit was swallowed")
	}
	if !nacelle.Retryable(err) {
		t.Errorf("Retryable(%v) = false, want true", err)
	}
}

// A rejected request is rejected however many times it is sent, and the run
// should fail at once rather than three times as slowly.
func TestAnInBandRejectionIsNotRetryable(t *testing.T) {
	const rejected = `data: {"id":"g","error":{"code":400,"message":"model not found"},"choices":[{"index":0,"delta":{"content":""},"finish_reason":"error"}]}

data: [DONE]

`
	backend, _ := serve(t, rejected)

	err := failure(t, backend)
	if err == nil {
		t.Fatal("an in-band rejection was swallowed")
	}
	if nacelle.Retryable(err) {
		t.Errorf("Retryable(%v) = true, want false", err)
	}
}

// A provider failure is worth another attempt whether or not it named itself.
func TestAnInBandServerErrorIsRetryable(t *testing.T) {
	const unavailable = `data: {"id":"g","error":{"code":503,"message":"upstream unavailable"},"choices":[{"index":0,"delta":{"content":""},"finish_reason":"error"}]}

data: [DONE]

`
	backend, _ := serve(t, unavailable)

	err := failure(t, backend)
	if err == nil {
		t.Fatal("an in-band server error was swallowed")
	}
	if !nacelle.Retryable(err) {
		t.Errorf("Retryable(%v) = false, want true", err)
	}
}

// The fallback on metadata.error_type exists for the payloads whose code is
// not a number, and a number-typed decoder failed the whole parse on exactly
// those — so the branch written for this case never ran, and a rate limit
// spelled in words classified as permanent.
func TestAnInBandRateLimitSpelledInWordsIsRetryable(t *testing.T) {
	const rateLimited = `data: {"id":"g","error":{"code":"rate_limit_exceeded","message":"Rate limit exceeded","metadata":{"error_type":"rate_limit_exceeded"}},"choices":[{"index":0,"delta":{"content":""},"finish_reason":"error"}]}

data: [DONE]

`
	backend, _ := serve(t, rateLimited)

	err := failure(t, backend)
	if err == nil {
		t.Fatal("an in-band rate limit was swallowed")
	}
	if !nacelle.Retryable(err) {
		t.Errorf("Retryable(%v) = false, want true", err)
	}
}

// A payload that is not a JSON object carries no code to read, and guessing
// that an unreadable failure is temporary is how a permanent one becomes a
// slow permanent one.
func TestAnUnreadableErrorPayloadFailsClosed(t *testing.T) {
	const malformed = `data: {"id":"g","error":["rate limited"],"choices":[{"index":0,"delta":{"content":""},"finish_reason":"error"}]}

data: [DONE]

`
	backend, _ := serve(t, malformed)

	err := failure(t, backend)
	if err == nil {
		t.Fatal("an in-band failure this package cannot parse was swallowed")
	}
	if nacelle.Retryable(err) {
		t.Errorf("Retryable(%v) = true, want an unparseable failure treated as permanent", err)
	}
}
