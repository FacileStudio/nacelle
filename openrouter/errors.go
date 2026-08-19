package openrouter

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/FacileStudio/nacelle"

	"github.com/openai/openai-go/v3/packages/ssestream"
)

// transientCodes are the HTTP statuses OpenRouter reports in-band that are
// worth another attempt. They mirror what the SDK retries when the same
// condition arrives as a real status line.
var transientCodes = map[int64]bool{
	408: true,
	409: true,
	429: true,
}

// classify promotes the transient failures OpenRouter reports inside a
// successful response.
//
// OpenRouter answers 200 and then puts the failure in the SSE body, so no
// HTTP-level retry can ever see it: by the time {"error":{"code":429}} arrives
// the status line is long gone and the SDK has already decided the request
// succeeded. What reaches us is a StreamError whose only structure is the
// event it was carried in.
//
// The payload has to be parsed rather than the message matched, and the code
// cannot be typed at all. OpenRouter usually sends "code": 429 as a number,
// but a provider behind it may pass its own string through — and a
// number-typed field fails the whole Unmarshal on a string, which took the
// error_type fallback down with it. That fallback exists precisely for the
// payloads whose code is not a number, so it was dead in every case it was
// written for. Code is therefore read as any and interpreted afterwards.
//
// What still fails closed is the shape: an error that is not a JSON object —
// a bare string, an array, nothing at all — leaves the payload unreadable, and
// an unreadable failure is treated as permanent rather than guessed at.
func classify(err error) error {
	var stream *ssestream.StreamError
	if err == nil || !errors.As(err, &stream) {
		return err
	}

	var payload struct {
		Error struct {
			Code     any `json:"code"`
			Metadata struct {
				ErrorType string `json:"error_type"`
			} `json:"metadata"`
		} `json:"error"`
	}
	if json.Unmarshal(stream.Event.Data, &payload) != nil {
		return err
	}

	if transientStatus(payload.Error.Code) || payload.Error.Metadata.ErrorType == "rate_limit_exceeded" {
		return nacelle.Transient(err)
	}
	return err
}

// transientStatus reads the in-band code and reports whether it is worth
// retrying. An unparseable code is not: guessing that an error we cannot read
// is temporary is how a permanent failure becomes a slow permanent failure.
//
// A JSON number decodes to float64 and a quoted status to a string, and both
// mean the same thing on the wire, so both are read. Anything else — a code
// that is a word, an object, or absent — has no status in it and is left to
// the error_type fallback.
func transientStatus(code any) bool {
	var status int64
	switch value := code.(type) {
	case float64:
		status = int64(value)
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return false
		}
		status = parsed
	default:
		return false
	}
	return transientCodes[status] || status >= 500
}
