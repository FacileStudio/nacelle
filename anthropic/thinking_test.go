package anthropic

import (
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// nacelle.Config documents Thinking as streaming the model's reasoning as
// KindThinking events, off by default. A consumer that never opted in getting
// reasoning here and not on the OpenRouter backend is a difference nobody
// chose, and the promise is the one this backend was breaking.
func TestAConsumerThatDidNotAskForReasoningNeverSeesIt(t *testing.T) {
	turn := func() string {
		return sse(t, messageStart(),
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"weighing it up"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"done"}}`,
			`{"type":"content_block_stop","index":1}`,
			messageDelta("end_turn"), `{"type":"message_stop"}`)
	}

	silent := collect(t, New(Config{Client: stub(t, turn())}), nacelle.Request{MaxTokens: 1024})
	for _, event := range silent {
		if event.Kind == nacelle.KindThinking {
			t.Fatalf("a run that did not ask for reasoning was sent %s", kinds(silent))
		}
	}

	asked := collect(t, New(Config{Client: stub(t, turn())}), nacelle.Request{MaxTokens: 1024, Thinking: nacelle.Thinking{Show: true}})
	var reasoned bool
	for _, event := range asked {
		reasoned = reasoned || event.Kind == nacelle.KindThinking
	}
	if !reasoned {
		t.Errorf("a run that asked for reasoning got %s", kinds(asked))
	}
}

// EffortNone is the caller asking for no reasoning at all, and the disabled
// variant is the only member of the union that can say it. Adaptive with the
// display turned off would read the same from outside this package and cost
// the same money, because the model still reasons and the tokens are still on
// the invoice. Show is set here to prove which of the two decides: a request
// that asked for no thinking has nothing to display, whoever is watching.
func TestEffortNoneSendsTheDisabledVariant(t *testing.T) {
	params := New(Config{}).params(nacelle.Request{
		Thinking: nacelle.Thinking{Effort: nacelle.EffortNone, Show: true},
	})
	if params.Thinking.OfDisabled == nil {
		t.Fatalf("thinking = %+v, want the disabled variant", params.Thinking)
	}
}

// BudgetTokens exists on the enabled variant and nowhere else in the union, so
// a request carrying a budget has to travel as that one. Sent as adaptive, the
// ceiling is not rejected, it is dropped: the run reasons as deeply as it likes
// and the caller is billed for exactly the tokens they tried to cap.
func TestABudgetSendsTheEnabledVariantCarryingIt(t *testing.T) {
	params := New(Config{}).params(nacelle.Request{Thinking: nacelle.Thinking{Budget: 2048}})
	if params.Thinking.OfEnabled == nil {
		t.Fatalf("thinking = %+v, want the enabled variant", params.Thinking)
	}
	if params.Thinking.OfEnabled.BudgetTokens != 2048 {
		t.Errorf("budget = %d, want the 2048 the request asked for", params.Thinking.OfEnabled.BudgetTokens)
	}
	if params.Thinking.OfEnabled.Display != "" {
		t.Error("a run that did not ask to watch was sent a display setting anyway")
	}

	shown := New(Config{}).params(nacelle.Request{Thinking: nacelle.Thinking{Budget: 2048, Show: true}})
	if shown.Thinking.OfEnabled.Display != sdk.BetaThinkingConfigEnabledDisplaySummarized {
		t.Errorf("display = %q, want the summary the consumer asked to watch", shown.Thinking.OfEnabled.Display)
	}
}

// Adaptive is what a request that named neither a depth nor a budget gets,
// because it is the only mode current models accept without a budget and the
// one they are already in when nothing is named. Naming it anyway is what makes
// the display setting reachable, which is the only part of this a caller
// controls once they have asked for no particular depth.
func TestNamingNeitherDepthNorBudgetIsAdaptive(t *testing.T) {
	params := New(Config{}).params(nacelle.Request{})
	if params.Thinking.OfAdaptive == nil {
		t.Fatalf("thinking = %+v, want the adaptive variant", params.Thinking)
	}
	if params.Thinking.OfAdaptive.Display != "" {
		t.Error("the reasoning summary was requested by a run that never asked to watch")
	}

	shown := New(Config{}).params(nacelle.Request{Thinking: nacelle.Thinking{Show: true}})
	if shown.Thinking.OfAdaptive.Display != sdk.BetaThinkingConfigAdaptiveDisplaySummarized {
		t.Errorf("display = %q, want the summary the consumer asked to watch", shown.Thinking.OfAdaptive.Display)
	}
}

// output_config.effort is a closed enum of five values and nacelle.Effort is
// the union of what every provider takes, so two of its levels have to be
// answered here rather than cast through. Sending "minimal" is rejected;
// sending "none" contradicts the disabled thinking block beside it.
func TestTheTwoLevelsAnthropicLacksAreAnsweredNotForwarded(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		effort nacelle.Effort
		want   string
	}{
		{"none carries no effort at all", nacelle.EffortNone, ""},
		{"minimal clamps to the lowest that exists", nacelle.EffortMinimal, "low"},
		{"a level anthropic has goes through", nacelle.EffortXHigh, "xhigh"},
		{"unset stays unset", "", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			params := New(Config{}).params(nacelle.Request{
				Thinking: nacelle.Thinking{Effort: testCase.effort},
			})
			if got := string(params.OutputConfig.Effort); got != testCase.want {
				t.Errorf("effort = %q, want %q", got, testCase.want)
			}
		})
	}
}

// A ceiling on reasoning that was asked not to happen is two settings
// disagreeing, and off has to win: the enabled variant carrying a budget is a
// request to think, so honouring the budget here would turn "do not think"
// into "think, but only this much". The other backend resolves the same pair
// the same way.
func TestOffBeatsACeiling(t *testing.T) {
	params := New(Config{}).params(nacelle.Request{
		Thinking: nacelle.Thinking{Effort: nacelle.EffortNone, Budget: 4096},
	})
	if params.Thinking.OfDisabled == nil {
		t.Fatalf("thinking = %+v, want the disabled variant despite the budget", params.Thinking)
	}
}
