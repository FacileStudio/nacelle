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
//
// Any 5xx counts, whatever the body said. The typed set is the API's own
// vocabulary and it is right about the API, but the API is not the only thing
// that answers: a proxy, a gateway or a load balancer in between writes its
// own body, and a 502 carrying no type at all is exactly the failure another
// attempt fixes. Without this line, that one condition is permanent here and
// retried on the OpenRouter backend, which is a difference nobody chose.
func classify(err error) error {
	var api *sdk.Error
	if err == nil || !errors.As(err, &api) {
		return err
	}
	if transientTypes[api.Type()] || api.StatusCode >= 500 {
		return nacelle.Transient(err)
	}
	return err
}
