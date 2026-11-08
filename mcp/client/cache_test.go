package client

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// listsFor runs ToolsFor once per user and returns how many tools/list
// requests reached the server in total.
func listsFor(t *testing.T, set *Set, seen *wire, users ...string) int {
	t.Helper()

	for _, user := range users {
		if _, err := set.ToolsFor(asUser(t.Context(), user)); err != nil {
			t.Fatalf("ToolsFor(%s) = %v", user, err)
		}
	}
	return seen.listed()
}

// A public list is fresh for its ttlMs and shared by every caller.
func TestAPublicToolListIsCachedForItsTTL(t *testing.T) {
	endpoint, seen := serveStub(t, stub{cacheable: &sdk.Cacheable{TTLMs: 60_000, CacheScope: "public"}}, whoami)
	set := connectStub(t, t.Context(), Remote{Name: "app", URL: endpoint})

	if got := listsFor(t, set, seen, "alice", "bob"); got != 1 {
		t.Errorf("tools/list reached the server %d times, want 1 inside the ttl", got)
	}
}

// A list with no ttl is stale on arrival and fetched every time.
func TestAToolListWithNoTTLIsNotCached(t *testing.T) {
	endpoint, seen := serveStub(t, stub{cacheable: &sdk.Cacheable{CacheScope: "public"}}, whoami)
	set := connectStub(t, t.Context(), Remote{Name: "app", URL: endpoint})

	if got := listsFor(t, set, seen, "alice", "alice"); got != 3 {
		t.Errorf("tools/list reached the server %d times, want 3 with a zero ttl", got)
	}
}

// A private list is cached per identity and never served to another one.
func TestAPrivateToolListIsCachedPerIdentity(t *testing.T) {
	endpoint, seen := serveStub(t, stub{cacheable: &sdk.Cacheable{TTLMs: 60_000, CacheScope: "private"}}, whoami)
	set := connectStub(t, asUser(t.Context(), "boot"), Remote{
		Name: "app", URL: endpoint, Identity: userOf,
		Token: func(ctx context.Context) (string, error) { return "token-" + userOf(ctx), nil },
	})

	if got := listsFor(t, set, seen, "alice", "alice", "bob"); got != 3 {
		t.Errorf("tools/list reached the server %d times, want boot, alice and bob once each", got)
	}
}

// With a per-call token and no Identity there is nothing safe to key a
// private list by, so it is never cached.
func TestAPrivateToolListWithNoIdentityIsNotCached(t *testing.T) {
	endpoint, seen := serveStub(t, stub{cacheable: &sdk.Cacheable{TTLMs: 60_000, CacheScope: "private"}}, whoami)
	set := connectStub(t, t.Context(), Remote{
		Name: "app", URL: endpoint,
		Token: func(ctx context.Context) (string, error) { return "token-" + userOf(ctx), nil },
	})

	if got := listsFor(t, set, seen, "alice", "alice"); got != 3 {
		t.Errorf("tools/list reached the server %d times, want every private list fetched", got)
	}
}
