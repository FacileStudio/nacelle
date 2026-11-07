package openrouter

import (
	"github.com/FacileStudio/nacelle"
	"github.com/openai/openai-go/v3/option"
)

func (b *Backend) requestOptions(request nacelle.Request) []option.RequestOption {
	var options []option.RequestOption
	if len(b.provider) > 0 {
		options = append(options, option.WithJSONSet("provider", b.provider))
	}
	if reasoning := reasoningParam(request.Thinking); reasoning != nil {
		options = append(options, option.WithJSONSet("reasoning", reasoning))
	}
	return options
}

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
