package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func both(server *sdk.Server) {
	whoami(server)
	confirming(server)
}

// A 429 on the first request of Connect is waited out for its Retry-After,
// and the session still speaks 2026-07-28 rather than falling back.
func TestARefusedConnectIsRetriedAfterItsRetryAfter(t *testing.T) {
	endpoint, seen := serveLimited(t, refuseFirst("server/discover", 1, http.StatusTooManyRequests, "2"), both)
	set := connectStub(t, t.Context(), Remote{Name: "app", URL: endpoint})

	answer, err := callTool(t.Context(), find(t, set.Tools(), "app_whoami"), nil)
	if err != nil || answer != " 2026-07-28" {
		t.Errorf("Run = %q, %v, want a 2026-07-28 session", answer, err)
	}
	if got := seen.slept(); !slices.Equal(got, []time.Duration{2 * time.Second}) {
		t.Errorf("waits = %v, want the server's 2s", got)
	}
}

// A refused tools/list with no Retry-After backs off and lists again, so the
// server's tools are not left out of the run.
func TestARefusedToolsListBacksOffAndListsAgain(t *testing.T) {
	endpoint, seen := serveLimited(t, refuseFirst("tools/list", 3, http.StatusTooManyRequests, ""), both)
	set := connectStub(t, t.Context(), Remote{Name: "app", URL: endpoint})

	tools, err := set.ToolsFor(t.Context())
	if err != nil || len(tools) != 2 {
		t.Fatalf("ToolsFor = %d tools, %v, want both", len(tools), err)
	}
	if got := seen.slept(); !slices.Equal(got, backoffs) {
		t.Errorf("waits = %v, want %v", got, backoffs)
	}
}

// A server that keeps refusing fails Connect with the typed error, which
// says nacelle already retried and must not be retried on top.
func TestARefusalThatOutlastsTheRetriesIsTyped(t *testing.T) {
	always := func(kind string, _ int) (int, string) {
		if kind == "tools/list" {
			return http.StatusServiceUnavailable, "Wed, 21 Oct 2015 07:28:00 GMT"
		}
		return 0, ""
	}
	endpoint, seen := serveLimited(t, always, both)
	_, err := Connect(t.Context(), Remote{Name: "app", URL: endpoint})

	refusal, ok := errors.AsType[*RateLimitError](err)
	if !ok {
		t.Fatalf("Connect = %v, want a *RateLimitError", err)
	}
	if refusal.Status != http.StatusServiceUnavailable || refusal.Method != "tools/list" || refusal.Retries != 3 || refusal.Retryable() {
		t.Errorf("refusal = %+v, want a spent 503 on tools/list", refusal)
	}
	if got := seen.count("tools/list"); got != 4 {
		t.Errorf("tools/list sent %d times, want 4", got)
	}
}

// A first tools/call that is refused is never sent again: the caller gets the
// typed error with the server's wait, and the connection stays usable.
func TestARefusedCallIsNotRetried(t *testing.T) {
	endpoint, seen := serveLimited(t, refuseFirst("tools/call", 1, http.StatusTooManyRequests, "7"), both)
	tool := find(t, connectStub(t, t.Context(), Remote{Name: "app", URL: endpoint}).Tools(), "app_whoami")

	_, err := callTool(t.Context(), tool, nil)
	refusal, ok := errors.AsType[*RateLimitError](err)
	if !ok || refusal.RetryAfter != 7*time.Second || !refusal.Retryable() {
		t.Fatalf("Run = %v, want a retryable *RateLimitError asking for 7s", err)
	}
	if got := seen.count("tools/call"); got != 1 || len(seen.slept()) != 0 {
		t.Errorf("tools/call sent %d times after %v, want once and no wait", got, seen.slept())
	}
	if answer, err := callTool(t.Context(), tool, nil); err != nil || answer != " 2026-07-28" {
		t.Errorf("next Run = %q, %v, want the connection still up", answer, err)
	}
}

// A 429 on the round that carries the person's answer sends that round again
// with the same answer, so the person is asked once.
func TestARefusedResumptionIsSentAgainWithTheSameAnswer(t *testing.T) {
	endpoint, seen := serveLimited(t, refuseFirst("tools/call+answers", 1, http.StatusTooManyRequests, "1"), both)
	host := &asked{}
	tools := connectStub(t, t.Context(), Remote{Name: "app", URL: endpoint, Elicit: host.hook(Answer{Accept: true})}).Tools()

	reply, err := callTool(t.Context(), find(t, tools, "app_send"), json.RawMessage(`{"to":"ana"}`))
	if err != nil || reply != "sent to ana" {
		t.Fatalf("Run = %q, %v, want the send to go through", reply, err)
	}
	if len(host.seen) != 1 || seen.count("tools/call") != 1 || seen.count("tools/call+answers") != 2 {
		t.Errorf("asked %d times, sends %v, want one question and the answer sent twice", len(host.seen), seen.kinds)
	}
}

// A 503 on that round may come from a proxy after the tool ran, so it stands.
func TestA503OnAResumptionIsNotSentAgain(t *testing.T) {
	endpoint, seen := serveLimited(t, refuseFirst("tools/call+answers", 1, http.StatusServiceUnavailable, "1"), both)
	host := &asked{}
	tools := connectStub(t, t.Context(), Remote{Name: "app", URL: endpoint, Elicit: host.hook(Answer{Accept: true})}).Tools()

	_, err := callTool(t.Context(), find(t, tools, "app_send"), json.RawMessage(`{"to":"ana"}`))
	refusal, ok := errors.AsType[*RateLimitError](err)
	if !ok || refusal.Status != http.StatusServiceUnavailable {
		t.Fatalf("Run = %v, want the 503 as a *RateLimitError", err)
	}
	if got := seen.count("tools/call+answers"); got != 1 {
		t.Errorf("the answer was sent %d times, want once", got)
	}
}

// A wait longer than the request's deadline is not slept: the refusal comes
// back at once.
func TestAWaitPastTheDeadlineIsNotSlept(t *testing.T) {
	always := func(string, int) (int, string) { return http.StatusTooManyRequests, "30" }
	endpoint, seen := serveLimited(t, always, both)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_, err := Connect(ctx, Remote{Name: "app", URL: endpoint})
	if _, ok := errors.AsType[*RateLimitError](err); !ok {
		t.Fatalf("Connect = %v, want a *RateLimitError", err)
	}
	if got := seen.slept(); len(got) != 0 {
		t.Errorf("waits = %v, want none", got)
	}
}

// Retry-After is read in seconds or as an HTTP-date, capped at 30 seconds.
func TestRetryAfterReadsSecondsAndDates(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		wait  time.Duration
		named bool
	}{
		"5": {5 * time.Second, true}, " 0 ": {0, true}, "3600": {maxRetryAfter, true},
		"99999999999999999": {maxRetryAfter, true}, "-1": {0, false}, "": {0, false}, "soon": {0, false},
		"Thu, 12 Nov 2026 10:00:12 GMT": {12 * time.Second, true},
		"Thu, 12 Nov 2026 11:00:00 GMT": {maxRetryAfter, true},
		"Thu, 12 Nov 2026 09:00:00 GMT": {0, true},
	}
	for header, want := range cases {
		if wait, named := retryAfter(header, now); wait != want.wait || named != want.named {
			t.Errorf("retryAfter(%q) = %v, %v, want %v, %v", header, wait, named, want.wait, want.named)
		}
	}
}
