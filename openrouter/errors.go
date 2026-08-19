package openrouter

import (
	"encoding/json"
	"errors"

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
// has to be read as a number: OpenRouter sends "code": 429, not "429", which
// is enough to make a string-typed decoder drop it and every classification
// keyed on it fail open.
func classify(err error) error {
	var stream *ssestream.StreamError
	if err == nil || !errors.As(err, &stream) {
		return err
	}

	var payload struct {
		Error struct {
			Code     json.Number `json:"code"`
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
func transientStatus(code json.Number) bool {
	status, err := code.Int64()
	if err != nil {
		return false
	}
	return transientCodes[status] || status >= 500
}
