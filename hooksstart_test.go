package nacelle_test

import (
	"context"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// drainStream runs one conversation through the agent, failing on the first
// error, so a session_start test reaches its assertions without towing the
// range along.
func drainStream(t *testing.T, agent *nacelle.Agent, conversation []nacelle.Message) {
	t.Helper()
	for _, err := range agent.Stream(context.Background(), conversation) {
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
	}
}

// sessionStartAgent is an agent on the recording stub carrying one
// session_start hook, the shape every test below varies on.
func sessionStartAgent(t *testing.T, hook nacelle.Hook) (*nacelle.Agent, *stub) {
	t.Helper()
	backend := full()
	agent, err := nacelle.New(nacelle.Config{
		Backend: backend,
		System:  "s",
		Hooks:   map[nacelle.HookPoint][]nacelle.Hook{nacelle.SessionStart: {hook}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return agent, backend
}

// A session_start hook is the run's first word: it fires once, on an empty
// event, and what it injects joins the conversation the backend is handed
// while the caller's own slice stays untouched.
func TestSessionStartInjectionReachesTheModel(t *testing.T) {
	seen := nacelle.HookEvent{}
	record := func(_ context.Context, ev nacelle.HookEvent) nacelle.HookResult {
		seen = ev
		return nacelle.HookResult{Inject: "3 unread messages"}
	}
	agent, backend := sessionStartAgent(t, record)
	conversation := []nacelle.Message{nacelle.UserText("hello")}
	drainStream(t, agent, conversation)

	if seen.Point != nacelle.SessionStart || seen.Tool != "" || seen.Input != "" || seen.Result != "" || seen.Err != nil {
		t.Errorf("event = %+v; want an empty session_start event", seen)
	}
	if len(conversation) != 1 {
		t.Errorf("the caller's conversation is now %d messages; want it untouched", len(conversation))
	}
	messages := backend.received.Messages
	if len(messages) != 2 || said(messages[0]) != "hello" || said(messages[1]) != "3 unread messages" {
		t.Errorf("backend received %v; want the conversation with the injection appended", texts(messages))
	}
}

// The agent loop can make several model calls in one run; the hook answers
// on the first of them and is not asked again.
func TestSessionStartFiresOncePerRun(t *testing.T) {
	fires := 0
	count := func(context.Context, nacelle.HookEvent) nacelle.HookResult {
		fires++
		return nacelle.HookResult{}
	}
	agent, err := nacelle.New(nacelle.Config{
		Backend: newLoop([]step{toolStep("search", `{"query":"x"}`), textStep("done")}),
		System:  "s",
		Tools:   []nacelle.Tool{hookTool(t)},
		Hooks:   map[nacelle.HookPoint][]nacelle.Hook{nacelle.SessionStart: {count}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	drainStream(t, agent, []nacelle.Message{nacelle.UserText("go")})

	if fires != 1 {
		t.Errorf("session_start fired %d times, want once for the whole run", fires)
	}
}

// Nothing has happened yet for a session-start refusal to stop, so a Deny
// is dropped and the run starts without the injection.
func TestSessionStartDenyIsIgnored(t *testing.T) {
	deny := func(context.Context, nacelle.HookEvent) nacelle.HookResult {
		return nacelle.HookResult{Deny: "not yet"}
	}
	agent, backend := sessionStartAgent(t, deny)
	drainStream(t, agent, []nacelle.Message{nacelle.UserText("hello")})

	if got := len(backend.received.Messages); got != 1 {
		t.Errorf("backend received %d messages, want the conversation untouched", got)
	}
}

// A session-start hook that crashed has nothing left to guard; the panic is
// heard as silence, as after a tool, and the run starts anyway.
func TestPanickingSessionStartHookIsSilent(t *testing.T) {
	explode := func(context.Context, nacelle.HookEvent) nacelle.HookResult {
		panic("banner exploded")
	}
	agent, backend := sessionStartAgent(t, explode)
	drainStream(t, agent, []nacelle.Message{nacelle.UserText("hello")})

	if got := len(backend.received.Messages); got != 1 {
		t.Errorf("backend received %d messages, want the conversation untouched", got)
	}
}

// The MaxInject cap exists because injected text rides in the context for
// the rest of the conversation; a session_start injection rides no shorter.
func TestSessionStartInjectIsTruncatedToMaxInject(t *testing.T) {
	long := func(context.Context, nacelle.HookEvent) nacelle.HookResult {
		return nacelle.HookResult{Inject: strings.Repeat("x", nacelle.MaxInject+500)}
	}
	agent, backend := sessionStartAgent(t, long)
	drainStream(t, agent, []nacelle.Message{nacelle.UserText("hello")})

	injected := said(backend.received.Messages[1])
	if len(injected) > nacelle.MaxInject {
		t.Errorf("injected %d bytes, want at most %d", len(injected), nacelle.MaxInject)
	}
}
