package anthropic

import (
	"errors"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared"
)

// transientTypes are the API's own names for a failure that is about load or
// timing rather than about the request. Classifying on the type rather than on
// the status is what makes a mid-stream failure legible: an error delivered
// inside an already-successful response carries the status of the response it
// arrived in, which is 200, and says nothing about what went wrong.
var transientTypes = map[shared.ErrorType]bool{
	shared.ErrorTypeOverloadedError: true,
	shared.ErrorTypeRateLimitError:  true,
	shared.ErrorTypeTimeoutError:    true,
	shared.ErrorTypeAPIError:        true,
}

// classify promotes the failures worth another attempt.
//
// The SDK has already had its turn by the time an error gets here: it retries
// connection failures and the retryable statuses on its own, so anything
// reaching this point either survived that or was never visible to it. The
// second case is the one that matters — an error event arriving mid-stream is
// delivered on a response that was a success when its headers were written.
func classify(err error) error {
	var api *sdk.Error
	if err == nil || !errors.As(err, &api) {
		return err
	}
	if transientTypes[api.Type()] {
		return nacelle.Transient(err)
	}
	return err
}
