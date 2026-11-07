package nacelle_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// dispatcher routes {"to": name, "with": input} to one of its tools.
type dispatcher struct{ tools map[string]nacelle.Tool }

func (d dispatcher) Spec() nacelle.Spec {
	return nacelle.Spec{Name: "dispatch", Description: "Run a tool by name"}
}

func (d dispatcher) Run(context.Context, nacelle.Call, func(string)) (nacelle.Result, error) {
	return nacelle.Result{}, errors.New("the loop routes dispatch; it never runs it")
}

func (d dispatcher) Route(input json.RawMessage) (nacelle.Tool, json.RawMessage, error) {
	var in struct {
		To   string          `json:"to"`
		With json.RawMessage `json:"with"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, nil, err
	}
	tool, ok := d.tools[in.To]
	if !ok {
		return nil, nil, errors.New("nothing is called " + in.To)
	}
	return tool, in.With, nil
}

// A routed call reaches Approve, the hooks and the result event as the tool
// that runs, with the input that tool reads.
func TestARoutedCallIsSeenAsTheToolItRunsTo(t *testing.T) {
	inner := echoInput(t, "search")
	router := dispatcher{tools: map[string]nacelle.Tool{"search": inner}}
	var approved, hooked nacelle.Call
	hook := func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
		hooked = ev.Call
		return nacelle.HookResult{}
	}
	request := nacelle.Request{
		Approve: func(_ context.Context, call nacelle.Call) nacelle.Decision {
			approved = call
			return nacelle.Decision{}
		},
		Hooks: map[nacelle.HookPoint][]nacelle.Hook{nacelle.BeforeToolCall: {hook}},
	}
	got := runTool(t, context.Background(), request, router, `{"to":"search","with":{"query":"x"}}`)

	if got.err != nil || got.result != "search:x" {
		t.Fatalf("ran = %q, %v, want the inner tool's answer", got.result, got.err)
	}
	for who, call := range map[string]nacelle.Call{"Approve": approved, "the hook": hooked} {
		if call.Name != "search" || string(call.Input) != `{"query":"x"}` {
			t.Errorf("%s saw %+v, want the routed call", who, call)
		}
	}
	if got.tool.Name != "search" {
		t.Errorf("the result event names %q, want the tool that ran", got.tool.Name)
	}
}

// A route to nothing is answered with the router's own error, and nothing runs.
func TestARouteToNothingIsAnswered(t *testing.T) {
	got := runTool(t, context.Background(), nacelle.Request{}, dispatcher{}, `{"to":"missing"}`)
	if got.err == nil || got.result != "nothing is called missing" {
		t.Fatalf("ran = %q, %v, want the routing error", got.result, got.err)
	}
}
