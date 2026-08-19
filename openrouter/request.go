package openrouter

import (
	"github.com/FacileStudio/nacelle"

	"github.com/openai/openai-go/v3/option"
)

// requestOptions carries the parameters the OpenAI schema has no field for.
//
// Neither stream_options.include_usage nor usage.include appears here, and
// that is deliberate: OpenRouter deprecated both and returns full usage on
// every response regardless. Sending them is inert, and the sample code that
// still does is what makes people believe usage is opt-in.
func (b *Backend) requestOptions(request nacelle.Request) []option.RequestOption {
	options := []option.RequestOption{}
	if len(b.provider) > 0 {
		options = append(options, option.WithJSONSet("provider", b.provider))
	}
	if reasoning := reasoningParam(request); reasoning != nil {
		options = append(options, option.WithJSONSet("reasoning", reasoning))
	}
	return options
}

// reasoningParam asks for the depth and the visibility the caller wanted.
//
// exclude only hides the reasoning from the response; the model still thinks
// and is still billed for it. Setting it when the caller did not ask for
// thinking is therefore not a saving in tokens spent, but in tokens carried:
// reasoning nobody reads still fills the context window.
func reasoningParam(request nacelle.Request) map[string]any {
	if request.Effort == "" && !request.Thinking {
		return nil
	}
	reasoning := map[string]any{"exclude": !request.Thinking}
	if request.Effort != "" {
		reasoning["effort"] = string(request.Effort)
	} else {
		reasoning["enabled"] = true
	}
	return reasoning
}
