package anthropic

import (
	"github.com/FacileStudio/nacelle"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

func outputEffort(effort nacelle.Effort) (sdk.BetaOutputConfigEffort, bool) {
	switch effort {
	case "", nacelle.EffortNone:
		return "", false
	case nacelle.EffortMinimal:
		return sdk.BetaOutputConfigEffort(nacelle.EffortLow), true
	}
	return sdk.BetaOutputConfigEffort(effort), true
}

func thinkingConfig(t nacelle.Thinking) sdk.BetaThinkingConfigParamUnion {
	switch {
	case t.Effort == nacelle.EffortNone:
		disabled := sdk.NewBetaThinkingConfigDisabledParam()
		return sdk.BetaThinkingConfigParamUnion{OfDisabled: &disabled}
	case t.Budget > 0:
		enabled := sdk.BetaThinkingConfigEnabledParam{BudgetTokens: t.Budget}
		if t.Show {
			enabled.Display = sdk.BetaThinkingConfigEnabledDisplaySummarized
		}
		return sdk.BetaThinkingConfigParamUnion{OfEnabled: &enabled}
	default:
		adaptive := sdk.BetaThinkingConfigAdaptiveParam{}
		if t.Show {
			adaptive.Display = sdk.BetaThinkingConfigAdaptiveDisplaySummarized
		}
		return sdk.BetaThinkingConfigParamUnion{OfAdaptive: &adaptive}
	}
}
