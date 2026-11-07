package nacelle_test

import (
	"context"
	"sync"

	"github.com/FacileStudio/nacelle"
)

// echoTool records every real run, so a test can tell an approved call from a
// refused one without parsing results back.
type echoTool struct {
	mu  sync.Mutex
	ran []string
}

func (e *echoTool) Spec() nacelle.Spec {
	return nacelle.Spec{Name: "echo", Description: "repeat the task back", Schema: map[string]any{"type": "object"}}
}

func (e *echoTool) Run(_ context.Context, call nacelle.Call, _ func(string)) (nacelle.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ran = append(e.ran, string(call.Input))
	return nacelle.Result{Text: "echo ran"}, nil
}
