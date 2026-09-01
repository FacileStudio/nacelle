package google

import (
	"github.com/FacileStudio/nacelle"

	"github.com/openai/openai-go/v3/option"
)

func (b *Backend) requestOptions(request nacelle.Request) []option.RequestOption {
	var options []option.RequestOption
	if effort := reasoningEffort(request.Thinking); effort != "" {
		options = append(options, option.WithJSONSet("reasoning_effort", effort))
	}
	return options
}

func reasoningEffort(thinking nacelle.Thinking) string {
	switch thinking.Effort {
	case nacelle.EffortNone, "":
		return ""
	case nacelle.EffortMinimal, nacelle.EffortLow:
		return "low"
	case nacelle.EffortMedium:
		return "medium"
	case nacelle.EffortHigh, nacelle.EffortXHigh, nacelle.EffortMax:
		return "high"
	default:
		return string(thinking.Effort)
	}
}
