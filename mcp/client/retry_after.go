package client

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// idempotent are the methods that change nothing on the server, so a refused
// one is sent again whatever the refusal was.
var idempotent = map[string]bool{
	"server/discover": true, "initialize": true, "notifications/initialized": true,
	"tools/list": true, "ping": true,
}

// rpc is the part of a JSON-RPC request the retry decision reads.
type rpc struct {
	Method string `json:"method"`
	Params struct {
		InputResponses json.RawMessage `json:"inputResponses"`
	} `json:"params"`
}

// peek reads the request's method from a copy of its body, or returns the
// zero rpc, which nothing retries, when there is no body to read.
func peek(request *http.Request) rpc {
	var call rpc
	if request.GetBody == nil {
		return call
	}
	body, err := request.GetBody()
	if err != nil {
		return call
	}
	raw, err := io.ReadAll(body)
	if errors.Join(err, body.Close()) != nil || json.Unmarshal(raw, &call) != nil {
		return rpc{}
	}
	return call
}

// resumes reports whether this is a tools/call carrying a person's answers,
// the MRTR round after one the server refused before running anything.
func (c rpc) resumes() bool {
	answers := strings.TrimSpace(string(c.Params.InputResponses))
	return c.Method == "tools/call" && answers != "" && answers != "null"
}

// delay is how long to wait before sending the call again after refusal, and
// whether to send it again at all. A resumption is sent again only after a
// 429, which a server answers before running anything; a 503 can come from a
// proxy after the tool ran.
func (c rpc) delay(refusal *RateLimitError) (time.Duration, bool) {
	allowed := idempotent[c.Method] || (c.resumes() && refusal.Status == http.StatusTooManyRequests)
	if !allowed || refusal.Retries >= len(backoffs) {
		return 0, false
	}
	if refusal.RetryAfter > 0 {
		return refusal.RetryAfter, true
	}
	return backoffs[refusal.Retries], true
}

// retryAfter reads a Retry-After header, in delay-seconds or as an HTTP-date,
// capped at maxRetryAfter. It reports false when the header is absent or
// unreadable, and a date already past is a wait of zero.
func retryAfter(header string, now time.Time) (time.Duration, bool) {
	header = strings.TrimSpace(header)
	if seconds, err := strconv.ParseInt(header, 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(min(seconds, int64(maxRetryAfter/time.Second))) * time.Second, true
	}
	at, err := http.ParseTime(header)
	if err != nil {
		return 0, false
	}
	return min(max(at.Sub(now), 0), maxRetryAfter), true
}
