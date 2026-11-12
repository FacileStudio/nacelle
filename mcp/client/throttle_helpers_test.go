package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// rule answers one request before the MCP handler sees it: a status and a
// Retry-After to refuse it with, or 0 to let it through. seen is how many
// requests of the same kind came before it.
type rule func(kind string, seen int) (int, string)

// limiter records what a limited server was sent and how long the client
// waited between sends.
type limiter struct {
	mu    sync.Mutex
	seen  map[string]int
	kinds []string
	waits []time.Duration
}

// kindOf names a request by method, marking an MRTR resumption apart.
func kindOf(call rpc) string {
	if call.resumes() {
		return call.Method + "+answers"
	}
	return call.Method
}

func (l *limiter) admit(kind string, verdict rule) (int, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := l.seen[kind]
	l.seen[kind]++
	l.kinds = append(l.kinds, kind)
	return verdict(kind, seen)
}

func (l *limiter) waited(_ context.Context, wait time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.waits = append(l.waits, wait)
	return true
}

func (l *limiter) count(kind string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seen[kind]
}

func (l *limiter) slept() []time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]time.Duration(nil), l.waits...)
}

// serveLimited starts a stateless go-sdk server behind verdict and replaces
// sleep with the limiter's recorder, so a retry costs no wall time.
func serveLimited(t *testing.T, verdict rule, mount func(*sdk.Server)) (string, *limiter) {
	t.Helper()
	seen := &limiter{seen: map[string]int{}}
	sleep = seen.waited
	t.Cleanup(func() { sleep = pause })
	server := sdk.NewServer(&sdk.Implementation{Name: "stub", Version: "0"}, nil)
	mount(server)
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server },
		&sdk.StreamableHTTPOptions{Stateless: true})
	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var call rpc
		if json.Unmarshal(raw, &call) != nil {
			call = rpc{}
		}
		if status, after := seen.admit(kindOf(call), verdict); status != 0 {
			if after != "" {
				w.Header().Set("Retry-After", after)
			}
			w.WriteHeader(status)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(listener.Close)
	return listener.URL, seen
}

// refuseFirst refuses the first n requests of kind with status and after.
func refuseFirst(kind string, n, status int, after string) rule {
	return func(got string, seen int) (int, string) {
		if got == kind && seen < n {
			return status, after
		}
		return 0, ""
	}
}
