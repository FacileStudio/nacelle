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
	if reasoning := reasoningParam(request.Thinking); reasoning != nil {
		options = append(options, option.WithJSONSet("reasoning", reasoning))
	}
	return options
}

// reasoningParam asks for the depth the caller wanted, and never for silence.
//
// There used to be an exclude here, set whenever the caller had not asked to
// watch the model think, and it was the most expensive line in this package.
// exclude does not stop the model reasoning and does not stop the reasoning
// being billed; it stops the reasoning coming back. What comes back is what
// this backend replays on the next request so that a model resuming from a
// tool result finds its own train of thought where it left it, which means the
// default configuration asked every provider to destroy the one thing the tool
// loop depends on, and the saving it bought was zero. It is gone rather than
// demoted to an option: a switch whose only effect is to corrupt a tool loop
// is not a preference.
//
// A budget beats an effort when both were given, because the gateway refuses
// to take them together: "Only one of reasoning.effort and reasoning.max_tokens
// can be specified", 400, measured 2026-08-23. They are two spellings of one
// idea and OpenRouter documents the levels as percentages of the budget, so a
// budget is an effort said precisely and dropping the coarser one loses
// nothing. The Anthropic backend keeps both, because there they are different
// fields on the request and the API takes them together; a caller who sets
// both and swaps backend gets the ceiling honoured either way.
//
// Sending both is what this did until a real request was made with a real
// config. The fake server the tests run against accepts any JSON, so the shape
// was asserted and the rule was not, which is why assertOneDepth exists.
//
// A caller who asked for nothing gets no reasoning object at all, which hands
// the decision to the provider. That is not the same as off, and on a model
// whose reasoning is mandatory it resolves to that model's own maximum. Off is
// EffortNone, and it is the only thing that says so.
func reasoningParam(thinking nacelle.Thinking) map[string]any {
	if thinking.Effort == nacelle.EffortNone {
		return map[string]any{"enabled": false}
	}

	reasoning := map[string]any{}
	switch {
	case thinking.Budget > 0:
		reasoning["max_tokens"] = thinking.Budget
	case thinking.Effort != "":
		reasoning["effort"] = string(thinking.Effort)
	}
	if len(reasoning) == 0 {
		if !thinking.Show {
			return nil
		}
		reasoning["enabled"] = true
	}
	return reasoning
}
