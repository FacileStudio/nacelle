package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// stub configures the HTTP MCP server the stateless tests dial.
type stub struct {
	stateful  bool
	cacheable *sdk.Cacheable
	refuse    func(token string) bool
}

// wire is what the stub saw, read from the test goroutine.
type wire struct {
	mu     sync.Mutex
	tokens []string
	lists  int
}

func (w *wire) sawToken(header string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tokens = append(w.tokens, strings.TrimPrefix(header, "Bearer "))
}

func (w *wire) listed() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lists
}

func (w *wire) seenTokens() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.tokens...)
}

// counting counts tools/list requests that reach the server.
func (w *wire) counting(next sdk.MethodHandler) sdk.MethodHandler {
	return func(ctx context.Context, method string, request sdk.Request) (sdk.Result, error) {
		if method == "tools/list" {
			w.mu.Lock()
			w.lists++
			w.mu.Unlock()
		}
		return next(ctx, method, request)
	}
}

// serveStub starts a go-sdk v1.8 server over real HTTP, stateless unless the
// stub asks otherwise, answering a refused token with a 401.
func serveStub(t *testing.T, config stub, mount func(*sdk.Server)) (string, *wire) {
	t.Helper()

	seen := &wire{}
	options := &sdk.ServerOptions{}
	if config.cacheable != nil {
		options.SetCacheable = func(_ context.Context, _ sdk.Request, c *sdk.Cacheable) { *c = *config.cacheable }
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "stub", Version: "0"}, options)
	server.AddReceivingMiddleware(seen.counting)
	mount(server)
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server },
		&sdk.StreamableHTTPOptions{Stateless: !config.stateful})

	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		seen.sawToken(header)
		if config.refuse != nil && config.refuse(strings.TrimPrefix(header, "Bearer ")) {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(listener.Close)
	return listener.URL, seen
}

type userKey struct{}

// asUser is the context a host would build for one user's request.
func asUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, userKey{}, user)
}

func userOf(ctx context.Context) string {
	user, _ := ctx.Value(userKey{}).(string)
	return user
}

// whoami reports the bearer token and protocol version its call arrived with.
func whoami(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "whoami", Description: "Report the caller."},
		func(_ context.Context, request *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
			return text(request.Extra.Header.Get("Authorization") + " " + request.ProtocolVersion()), nil, nil
		})
}

// connectStub connects to a stub and closes the set when the test ends.
func connectStub(t *testing.T, ctx context.Context, remote Remote) *Set {
	t.Helper()

	set, err := Connect(ctx, remote)
	if err != nil {
		t.Fatalf("Connect = %v, want a live session", err)
	}
	t.Cleanup(func() {
		if err := set.Close(); err != nil {
			t.Errorf("Close = %v", err)
		}
	})
	return set
}
