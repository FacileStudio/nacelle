package client

import (
	"context"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// uncacheable is the identity that turns private caching off. It holds a
// byte no host identity can produce by accident.
const uncacheable = "\x00uncacheable"

// listCache keeps tools/list results for as long as the server's ttlMs says,
// under the scope its cacheScope names: one shared entry for "public", one
// per identity for "private".
//
// It replaces the SDK session's own cache rather than sitting beside it. That
// one is keyed by cursor alone, so on a connection that carries a different
// user's token per call it would hand one user's private tool list to the
// next. The middleware zeroes the ttl on what it passes up, which leaves the
// SDK entry stale on arrival and never served.
type listCache struct {
	mu       sync.Mutex
	entries  map[string]listEntry
	identity func(context.Context) string
	now      func() time.Time
}

type listEntry struct {
	result  sdk.ListToolsResult
	expires time.Time
}

func newListCache(identity func(context.Context) string) *listCache {
	return &listCache{entries: map[string]listEntry{}, identity: identity, now: time.Now}
}

// middleware serves tools/list from the cache while it is fresh and records
// every result the server sends.
func (c *listCache) middleware(next sdk.MethodHandler) sdk.MethodHandler {
	return func(ctx context.Context, method string, request sdk.Request) (sdk.Result, error) {
		params, ok := request.GetParams().(*sdk.ListToolsParams)
		if method != "tools/list" || !ok {
			return next(ctx, method, request)
		}
		private := c.privateKey(ctx, params.Cursor)
		if hit, ok := c.lookup("public\x00"+params.Cursor, private); ok {
			return hit, nil
		}
		result, err := next(ctx, method, request)
		listed, ok := result.(*sdk.ListToolsResult)
		if err != nil || !ok {
			return result, err
		}
		c.store(listed, "public\x00"+params.Cursor, private)
		return listed, nil
	}
}

// privateKey is the key a private result for this caller lives under, or ""
// when the caller has no identity to key it by.
func (c *listCache) privateKey(ctx context.Context, cursor string) string {
	identity := ""
	if c.identity != nil {
		identity = c.identity(ctx)
	}
	if identity == uncacheable {
		return ""
	}
	return "private\x00" + identity + "\x00" + cursor
}

// lookup returns a fresh copy of the first live entry among keys.
func (c *listCache) lookup(keys ...string) (*sdk.ListToolsResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		entry, ok := c.entries[key]
		if key == "" || !ok {
			continue
		}
		if c.now().Before(entry.expires) {
			served := entry.result
			served.TTLMs = 0
			return &served, true
		}
		delete(c.entries, key)
	}
	return nil, false
}

// store records listed under the key its scope calls for, drops expired
// entries so a host with many users does not grow without bound, then zeroes
// the ttl so the SDK session never serves it from its own cache.
func (c *listCache) store(listed *sdk.ListToolsResult, public, private string) {
	key := public
	if listed.CacheScope == "private" {
		key = private
	}
	if listed.TTLMs > 0 && key != "" {
		now := c.now()
		c.mu.Lock()
		for stale, entry := range c.entries {
			if !now.Before(entry.expires) {
				delete(c.entries, stale)
			}
		}
		c.entries[key] = listEntry{result: *listed, expires: now.Add(time.Duration(listed.TTLMs) * time.Millisecond)}
		c.mu.Unlock()
	}
	listed.TTLMs = 0
}
