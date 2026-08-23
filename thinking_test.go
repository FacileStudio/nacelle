package nacelle_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// A reasoning budget is written once, in a config file, and spent every time a
// question is asked. Both of these reach the provider as a 400 pointing at the
// question rather than at the setting, which is a bad afternoon on a machine
// where the file was edited weeks ago.
func TestABudgetThatCannotWorkIsRefusedAtConstruction(t *testing.T) {
	floored := &stub{can: nacelle.Capabilities{Thinking: true, Effort: true, MinBudget: 1024}}

	_, err := New(t, nacelle.Config{Backend: floored, System: "s",
		Thinking: nacelle.Thinking{Budget: 512}})
	var unsupported *nacelle.Unsupported
	if !errors.As(err, &unsupported) {
		t.Fatalf("a budget under the floor = %v, want an *Unsupported error", err)
	}
	if !strings.Contains(unsupported.Error(), "1024") {
		t.Errorf("error = %q, want it to name the floor so the reader knows what to change to", unsupported.Error())
	}

	_, err = New(t, nacelle.Config{Backend: floored, System: "s",
		MaxTokens: 4000, Thinking: nacelle.Thinking{Budget: 4000}})
	if err == nil || !strings.Contains(err.Error(), "4000") {
		t.Errorf("a budget filling the whole turn = %v, want a refusal naming both numbers", err)
	}

	if _, err := New(t, nacelle.Config{Backend: floored, System: "s",
		MaxTokens: 4000, Thinking: nacelle.Thinking{Budget: 1024}}); err != nil {
		t.Errorf("a budget on the floor with room to answer = %v, want it accepted", err)
	}
}

// A backend reporting no floor means it, and must not inherit another's.
func TestABackendWithNoFloorTakesAnyBudgetThatFits(t *testing.T) {
	if _, err := New(t, nacelle.Config{Backend: full(), System: "s",
		Thinking: nacelle.Thinking{Budget: 16}}); err != nil {
		t.Errorf("a small budget on a floorless backend = %v, want it accepted", err)
	}
}
