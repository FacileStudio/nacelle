package anthropic

import (
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// The typed set is the API's own vocabulary, but the API is not the only thing
// that answers a request: a proxy or a gateway writes its own body, and a 502
// naming no type at all is exactly the failure another attempt fixes. Left
// permanent, it is the one condition this backend gives up on and the
// OpenRouter one retries.
func TestAServerErrorWithNoTypeIsWorthAnotherAttempt(t *testing.T) {
	if !nacelle.Retryable(classify(&sdk.Error{StatusCode: 502})) {
		t.Error("a 502 carrying no error type was treated as permanent")
	}
	if !nacelle.Retryable(classify(&sdk.Error{StatusCode: 500})) {
		t.Error("a 500 carrying no error type was treated as permanent")
	}
}

// The permanent ones stay permanent: retrying a request the API has already
// rejected on its merits spends money to be told the same thing.
func TestARejectedRequestIsNotRetried(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404} {
		if nacelle.Retryable(classify(&sdk.Error{StatusCode: status})) {
			t.Errorf("a %d was promoted to transient; the request is what was wrong", status)
		}
	}
}
