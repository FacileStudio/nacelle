package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// seen records what a request carried, from the server's goroutine, for a
// test that reads it from its own.
type seen struct {
	mu      sync.Mutex
	headers http.Header
}

func (s *seen) record(header http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.headers == nil {
		s.headers = header.Clone()
	}
}

func (s *seen) get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headers.Get(key)
}

// remoteServer is a real MCP server behind a real HTTP listener, which is the
// only way to prove a transport works: an in-memory pair would exercise the
// protocol and skip everything this transport is.
func remoteServer(t *testing.T) (string, *seen) {
	t.Helper()

	server := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	register(server)
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil)

	watched := &seen{}
	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		watched.record(r.Header)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(listener.Close)
	return listener.URL, watched
}

// The transport nothing else in this package exercises: a server reached over
// HTTP has to hand back tools that call as readily as a subprocess's.
func TestARemoteServersToolsAreCallableOverHTTP(t *testing.T) {
	endpoint, _ := remoteServer(t)

	set, err := Connect(t.Context(), Remote{Name: "docs", URL: endpoint})
	if err != nil {
		t.Fatalf("Connect = %v, want a live session", err)
	}
	defer func() { _ = set.Close() }()

	answer, err := find(t, set.Tools(), "docs_echo").Run(t.Context(), json.RawMessage(`{"text":"over the wire"}`))
	if err != nil {
		t.Fatalf("Run = %v, want the echo back", err)
	}
	if answer != "over the wire" {
		t.Errorf("Run = %q, want %q", answer, "over the wire")
	}
}

// A bearer token is the whole reason Headers exists, and the transport
// reconnects on its own, so the header has to ride every request rather than
// be set once by hand at handshake time.
func TestRemoteHeadersRideEveryRequest(t *testing.T) {
	endpoint, watched := remoteServer(t)

	set, err := Connect(t.Context(), Remote{
		Name: "docs", URL: endpoint,
		Headers: map[string]string{"Authorization": "Bearer sekrit"},
	})
	if err != nil {
		t.Fatalf("Connect = %v, want a live session", err)
	}
	defer func() { _ = set.Close() }()

	if got := watched.get("Authorization"); got != "Bearer sekrit" {
		t.Errorf("Authorization = %q, want the token the caller configured", got)
	}
}

// An endpoint this client cannot dial is refused before a session is opened,
// naming the scheme so the reader knows which line to change.
func TestARemoteIsRefusedForASchemeThisClientDoesNotSpeak(t *testing.T) {
	for name, server := range map[string]Remote{
		"no URL":    {Name: "docs"},
		"websocket": {Name: "docs", URL: "ws://example.invalid/mcp"},
		"stdio-ish": {Name: "docs", URL: "file:///tmp/socket"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := (server).check(); err == nil {
				t.Fatal("check accepted an endpoint it cannot dial")
			}
		})
	}
}

// A RoundTripper must not modify the request it is handed, so the headers go
// onto a copy. Asserting it because the bug it prevents — a request mutated
// under a retry that is replaying it — is invisible until it is not.
func TestHeadersGoOntoACopyOfTheRequest(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "https://example.invalid/mcp", strings.NewReader(""))
	tripper := headed{
		base:    roundTripperFunc(func(*http.Request) (*http.Response, error) { return &http.Response{Body: http.NoBody}, nil }),
		headers: map[string]string{"Authorization": "Bearer sekrit"},
	}

	if _, err := tripper.RoundTrip(request); err != nil {
		t.Fatalf("RoundTrip = %v", err)
	}
	if got := request.Header.Get("Authorization"); got != "" {
		t.Errorf("the original request carries %q, want it untouched", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
