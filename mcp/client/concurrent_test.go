package client

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// One server, one connection, many callers at once.
//
// This is the only part of nacelle where concurrent runs share something that
// talks: a session is one pipe to one process, and every tool bridged from it
// writes down the same one. MCP is built for that — every message carries an
// id so an answer can find the call it belongs to — but "the protocol allows
// it" and "this client gets it right" are different claims, and only the
// second one matters to a service answering ten requests at once.
//
// The failure it guards against is the quiet kind: not a crash, but caller
// three reading caller seven's answer.
func TestOneServerAnswersManyCallersWithoutCrossingThem(t *testing.T) {
	set, err := Connect(t.Context(), helperCommand(t, "demo", nil))
	if err != nil {
		t.Fatalf("Connect = %v", err)
	}
	defer func() { _ = set.Close() }()

	echo := find(t, set.Tools(), "demo_echo")

	const callers = 20
	answers := make([]string, callers)
	failures := make([]error, callers)

	var waiting sync.WaitGroup
	for i := range callers {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			asked := fmt.Sprintf("caller-%d", i)
			answers[i], failures[i] = echo.Run(t.Context(), json.RawMessage(`{"text":"`+asked+`"}`))
		}()
	}
	waiting.Wait()

	for i := range callers {
		want := fmt.Sprintf("caller-%d", i)
		switch {
		case failures[i] != nil:
			t.Errorf("caller %d: %v", i, failures[i])
		case answers[i] != want:
			t.Errorf("caller %d was answered %q, want %q — the calls crossed", i, answers[i], want)
		}
	}
}
