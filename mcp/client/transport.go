package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// TokenSource returns the bearer token for one request to a [Remote].
//
// ctx is the context of the call that sends the request, so a host serving
// many users puts the user in ctx and returns that user's delegated token.
// When [Refreshing] reports true, the previous token was refused with a 401:
// skip any cache and mint a new one. An empty token sends no Authorization
// header.
type TokenSource func(ctx context.Context) (string, error)

type refreshKey struct{}

// Refreshing reports whether a [TokenSource] is being asked again because the
// server refused the token it returned a moment ago.
func Refreshing(ctx context.Context) bool {
	refreshing, _ := ctx.Value(refreshKey{}).(bool)
	return refreshing
}

// secured is an [http.RoundTripper] that SSRF-hardens every connection by
// validating the target IP on every request, catching DNS rebinding and a
// redirect to another host between the check at Connect and the connect
// itself.
type secured struct {
	base    http.RoundTripper
	server  string
	private []string
}

// RoundTrip validates the target before handing the request on.
func (s secured) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL != nil && request.URL.Host != "" {
		if err := checkTarget(request.URL.Host, s.server, s.private); err != nil {
			return nil, err
		}
	}
	return s.base.RoundTrip(request)
}

// headed adds this server's headers to every request.
//
// A RoundTripper is where this has to happen. StreamableClientTransport
// takes an *http.Client and drives the requests itself, and it reconnects a
// dropped stream on its own, so a header set once at handshake time would go
// missing on exactly the retry that needed it. The request is copied first,
// because a RoundTripper must not modify the one it is given.
type headed struct {
	base    http.RoundTripper
	headers map[string]string
}

// RoundTrip sets the headers on a copy of the request.
func (h headed) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	for key, value := range h.headers {
		cloned.Header.Set(key, value)
	}
	base := h.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(cloned)
}

// authorized sets the bearer token a [TokenSource] mints for the request's
// own context, and replays the request once with a fresh token after a 401.
//
// The SDK's OAuthHandler cannot do this: it asks for a token with the
// connection's context, not the call's, so every user would share one token.
type authorized struct {
	base   http.RoundTripper
	token  TokenSource
	server string
}

// RoundTrip sends the request with a token, and once more after a 401.
func (a authorized) RoundTrip(request *http.Request) (*http.Response, error) {
	if a.token == nil {
		return a.base.RoundTrip(request)
	}
	response, err := a.send(request.Context(), request)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		return response, err
	}
	replay := rewind(request)
	if replay == nil {
		return response, nil
	}
	if err := drain(response.Body); err != nil {
		return nil, fmt.Errorf("nacelle/mcp/client: server %q: reading a 401: %w", a.server, err)
	}
	return a.send(context.WithValue(request.Context(), refreshKey{}, true), replay)
}

// drain reads a refused response to the end and closes it, so the
// connection under it can carry the replay.
func drain(body io.ReadCloser) error {
	_, err := io.Copy(io.Discard, body)
	return errors.Join(err, body.Close())
}

// send asks the token source for a token under ctx and sends a copy of the
// request carrying it. The token never appears in an error.
func (a authorized) send(ctx context.Context, request *http.Request) (*http.Response, error) {
	token, err := a.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("nacelle/mcp/client: server %q: getting a token: %w", a.server, err)
	}
	cloned := request.Clone(request.Context())
	if token != "" {
		cloned.Header.Set("Authorization", "Bearer "+token)
	}
	return a.base.RoundTrip(cloned)
}

// rewind is a copy of request with its body back at the start, which is what
// replaying a POST after a 401 needs, or nil when the body cannot be read a
// second time and the 401 has to stand.
func rewind(request *http.Request) *http.Request {
	replay := request.Clone(request.Context())
	if request.Body == nil || request.Body == http.NoBody {
		return replay
	}
	if request.GetBody == nil {
		return nil
	}
	body, err := request.GetBody()
	if err != nil {
		return nil
	}
	replay.Body = body
	return replay
}
