package nacelle_test

import (
	"context"
	"fmt"
	"iter"
	"sync"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// echoing answers with the text it was handed, which is what lets a test tell
// one caller's answer from another's.
//
// It holds nothing, deliberately. The other fakes here script a sequence of
// attempts and count them, so they are the wrong shape for this: a backend
// that mutates a field would be the thing racing, and the question is whether
// the Agent above it does.
type echoing struct{}

func (echoing) Name() string                                                { return "echoing" }
func (echoing) Capabilities() nacelle.Capabilities                          { return nacelle.Capabilities{} }
func (echoing) CountTokens(context.Context, nacelle.Request) (int64, error) { return 0, nil }

func (echoing) Stream(_ context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	said := spoken(request.Messages)
	return func(yield func(nacelle.Event, error) bool) {
		for _, text := range said {
			if !yield(nacelle.Event{Kind: nacelle.KindText, Text: text}, nil) {
				return
			}
		}
		yield(nacelle.Event{Kind: nacelle.KindDone}, nil)
	}
}

// spoken is every line of text in a conversation, read before the sequence
// starts so the closure has one thing to walk instead of three.
func spoken(messages []nacelle.Message) []string {
	var said []string
	for _, message := range messages {
		for _, part := range message.Parts {
			if text, ok := part.(nacelle.Text); ok {
				said = append(said, text.Text)
			}
		}
	}
	return said
}

// One Agent, many callers, and every answer belongs to the caller that asked
// for it.
//
// This is the test behind the sentence in Agent's own doc comment, and it
// guards one line: Stream copies a.request before setting Messages on it.
// Assigning the conversation to the Agent's own request instead would look
// identical in every other test here — one caller cannot notice — and would
// hand a service one person's question answered with another's, which is the
// worst failure this package could have and the hardest to see. Verified by
// removing that copy: this then fails with crossed answers and data races. How
// many of each is not worth writing down, because a race does not reproduce to
// a number — which is the whole reason fifty callers run and not two.
func TestOneAgentAnswersManyCallersWithoutCrossingThem(t *testing.T) {
	agent, err := nacelle.New(nacelle.Config{Backend: echoing{}, System: "be brief"})
	if err != nil {
		t.Fatalf("New = %v", err)
	}

	answers, failures := askTogether(t, agent, 50)
	for i, answer := range answers {
		want := fmt.Sprintf("caller-%d", i)
		switch {
		case failures[i] != nil:
			t.Errorf("caller %d: %v", i, failures[i])
		case answer != want:
			t.Errorf("caller %d was answered %q, want %q — the conversations crossed", i, answer, want)
		}
	}
}

// askTogether starts every caller at once and waits for all of them, each
// asking something only it asked for.
func askTogether(t *testing.T, agent *nacelle.Agent, callers int) ([]string, []error) {
	t.Helper()

	answers := make([]string, callers)
	failures := make([]error, callers)

	var waiting sync.WaitGroup
	for i := range callers {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			answers[i], failures[i] = ask(t, agent, fmt.Sprintf("caller-%d", i))
		}()
	}
	waiting.Wait()
	return answers, failures
}

// ask runs one conversation to the end and returns the text of it.
func ask(t *testing.T, agent *nacelle.Agent, asked string) (string, error) {
	t.Helper()

	var answer string
	for event, err := range agent.Stream(t.Context(), []nacelle.Message{nacelle.UserText(asked)}) {
		if err != nil {
			return "", err
		}
		if event.Kind == nacelle.KindText {
			answer += event.Text
		}
	}
	return answer, nil
}
