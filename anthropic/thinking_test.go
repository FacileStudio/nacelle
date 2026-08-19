package anthropic

import (
	"testing"

	"github.com/FacileStudio/nacelle"
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

	asked := collect(t, New(Config{Client: stub(t, turn())}), nacelle.Request{MaxTokens: 1024, Thinking: true})
	var reasoned bool
	for _, event := range asked {
		reasoned = reasoned || event.Kind == nacelle.KindThinking
	}
	if !reasoned {
		t.Errorf("a run that asked for reasoning got %s", kinds(asked))
	}
}
