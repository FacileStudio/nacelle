package openrouter

import (
	"context"
	"testing"

	"github.com/FacileStudio/nacelle"
)

func failure(t *testing.T, backend *Backend) error {
	t.Helper()

	for _, err := range backend.Stream(context.Background(), nacelle.Request{System: "s"}) {
		if err != nil {
			return err
		}
	}
	return nil
}

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
