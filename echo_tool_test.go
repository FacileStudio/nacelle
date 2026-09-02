package nacelle_test

import (
	"context"
	"encoding/json"
	"sync"
)

// echoTool records every real run, so a test can tell an approved call from a
// refused one without parsing results back.
type echoTool struct {
	mu  sync.Mutex
	ran []string
}

func (e *echoTool) Name() string { return "echo" }
func (e *echoTool) Description() string {
	return "repeat the task back"
}
func (e *echoTool) Schema() map[string]any {
	return map[string]any{"type": "object"}
}

func (e *echoTool) Run(_ context.Context, input json.RawMessage) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ran = append(e.ran, string(input))
	return "echo ran", nil
}

func (e *echoTool) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.ran)
}
