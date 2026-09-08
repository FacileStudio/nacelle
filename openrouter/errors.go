package openrouter

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/FacileStudio/nacelle"

	"github.com/openai/openai-go/v3/packages/ssestream"
)

var transientCodes = map[int64]bool{
	408: true,
	409: true,
	429: true,
}

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
