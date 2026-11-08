package client

import (
	"context"
	"slices"
	"testing"
)

// A stateless 2026-07-28 server is spoken to at that version, and a stateful
// one still negotiates down to 2025-11-25.
func TestRemoteSpeaksStatelessAndFallsBackToLegacy(t *testing.T) {
	for want, config := range map[string]stub{"2026-07-28": {}, "2025-11-25": {stateful: true}} {
		endpoint, _ := serveStub(t, config, whoami)
		set := connectStub(t, t.Context(), Remote{Name: "app", URL: endpoint})

		answer, err := callTool(t.Context(), find(t, set.Tools(), "app_whoami"), nil)
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
		if answer != " "+want {
			t.Errorf("Run = %q, want protocol %s", answer, want)
		}
	}
}

// One connection carries each call's own token, minted from the call's
// context, which is how a host injects a per-user delegated token.
func TestATokenSourceAuthorisesEachCallFromItsOwnContext(t *testing.T) {
	endpoint, _ := serveStub(t, stub{}, whoami)
	source := func(ctx context.Context) (string, error) { return "token-" + userOf(ctx), nil }
	set := connectStub(t, asUser(t.Context(), "boot"), Remote{Name: "app", URL: endpoint, Token: source})
	tool := find(t, set.Tools(), "app_whoami")

	for _, user := range []string{"alice", "bob"} {
		answer, err := callTool(asUser(t.Context(), user), tool, nil)
		if err != nil {
			t.Fatalf("Run as %s = %v", user, err)
		}
		if want := "Bearer token-" + user + " 2026-07-28"; answer != want {
			t.Errorf("Run as %s = %q, want %q", user, answer, want)
		}
	}
}

// A 401 asks the source once more with Refreshing set and replays the call.
func TestA401RefreshesTheTokenOnce(t *testing.T) {
	endpoint, _ := serveStub(t, stub{refuse: func(token string) bool { return token == "stale" }}, whoami)
	source := func(ctx context.Context) (string, error) {
		if Refreshing(ctx) {
			return "fresh", nil
		}
		return "stale", nil
	}
	set := connectStub(t, t.Context(), Remote{Name: "app", URL: endpoint, Token: source})

	answer, err := callTool(t.Context(), find(t, set.Tools(), "app_whoami"), nil)
	if err != nil || answer != "Bearer fresh 2026-07-28" {
		t.Errorf("Run = %q, %v, want the refreshed token to carry the call", answer, err)
	}
}

// A server that refuses every token gets one refresh per request, never a
// loop: each refreshed token follows exactly one refused one.
func TestA401AfterARefreshIsTheAnswer(t *testing.T) {
	endpoint, seen := serveStub(t, stub{refuse: func(string) bool { return true }}, whoami)
	source := func(ctx context.Context) (string, error) {
		if Refreshing(ctx) {
			return "fresh", nil
		}
		return "stale", nil
	}
	if _, err := Connect(t.Context(), Remote{Name: "app", URL: endpoint, Token: source}); err == nil {
		t.Fatal("Connect succeeded against a server that refuses every token")
	}

	tokens := seen.seenTokens()
	for i, token := range tokens {
		if token == "fresh" && (i == 0 || tokens[i-1] != "stale") {
			t.Fatalf("tokens = %v, want every refresh to follow one refusal", tokens)
		}
	}
	if slices.Index(tokens, "fresh") < 0 {
		t.Errorf("tokens = %v, want a refresh after the first 401", tokens)
	}
}
