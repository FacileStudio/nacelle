package nacelle_test

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/mcp"
)

// stub is a backend that records what it was asked and can be told what it
// supports, so the capability rules are testable without a network.
//
// called is separate from received: a zero Request is a valid thing to have
// received, so it cannot stand in for "was this backend ever reached at all".
type stub struct {
	can      nacelle.Capabilities
	received nacelle.Request
	called   bool
}

func (s *stub) Name() string                       { return "stub" }
func (s *stub) Capabilities() nacelle.Capabilities { return s.can }

func (s *stub) Stream(_ context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	s.called = true
	s.received = request
	return func(yield func(nacelle.Event, error) bool) {
		yield(nacelle.Event{Kind: nacelle.KindDone}, nil)
	}
}

func full() *stub {
	return &stub{can: nacelle.Capabilities{MCP: true, Thinking: true, Effort: true}}
}

func TestNewRequiresABackendAndASystemPrompt(t *testing.T) {
	if _, err := New(t, nacelle.Config{System: "s"}); !errors.Is(err, nacelle.ErrNoBackend) {
		t.Errorf("no backend = %v, want ErrNoBackend", err)
	}
	if _, err := New(t, nacelle.Config{Backend: full()}); !errors.Is(err, nacelle.ErrNoSystemPrompt) {
		t.Errorf("no system prompt = %v, want ErrNoSystemPrompt", err)
	}
}

// New helper keeps the table tests readable.
func New(_ *testing.T, cfg nacelle.Config) (*nacelle.Agent, error) { return nacelle.New(cfg) }

// Losing MCP tools silently looks like a model that will not use them, which
// is a bad afternoon. The refusal is the feature.
func TestAskingForWhatABackendLacksIsRefused(t *testing.T) {
	bare := &stub{}

	cases := map[string]nacelle.Config{
		"mcp":      {Backend: bare, System: "s", MCP: []mcp.Server{{Name: "p", URL: "https://p.test"}}},
		"thinking": {Backend: bare, System: "s", Thinking: true},
		"effort":   {Backend: bare, System: "s", Effort: nacelle.EffortHigh},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := nacelle.New(cfg)
			var unsupported *nacelle.Unsupported
			if !errors.As(err, &unsupported) {
				t.Fatalf("New = %v, want an *Unsupported error", err)
			}
			if unsupported.Backend != "stub" {
				t.Errorf("backend = %q, want stub", unsupported.Backend)
			}
		})
	}
}

func TestACapableBackendAcceptsEverything(t *testing.T) {
	agent, err := nacelle.New(nacelle.Config{
		Backend:  full(),
		System:   "s",
		Thinking: true,
		Effort:   nacelle.EffortMax,
		MCP:      []mcp.Server{{Name: "p", URL: "https://p.test"}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if agent.Backend().Name() != "stub" {
		t.Errorf("backend = %q, want stub", agent.Backend().Name())
	}
}

// Two tools with one name means the model's choice is ambiguous and whichever
// the backend indexes last wins.
func TestDuplicateToolNamesAreRefused(t *testing.T) {
	first, _ := nacelle.NewTool("same", "does a thing", func(context.Context, struct{}) (string, error) { return "", nil })
	second, _ := nacelle.NewTool("same", "does another", func(context.Context, struct{}) (string, error) { return "", nil })

	if _, err := nacelle.New(nacelle.Config{Backend: full(), System: "s", Tools: []nacelle.Tool{first, second}}); err == nil {
		t.Fatal("two tools sharing a name were accepted")
	}
}

func TestDuplicateMCPServerNamesAreRefused(t *testing.T) {
	_, err := nacelle.New(nacelle.Config{
		Backend: full(),
		System:  "s",
		MCP:     []mcp.Server{{Name: "p", URL: "https://a.test"}, {Name: "p", URL: "https://b.test"}},
	})
	if err == nil {
		t.Fatal("two MCP servers sharing a name were accepted")
	}
}

func TestTheRequestReachesTheBackendWithDefaultsFilled(t *testing.T) {
	backend := full()
	agent, err := nacelle.New(nacelle.Config{Backend: backend, System: "be useful"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for range agent.Stream(context.Background(), []nacelle.Message{nacelle.UserText("hi")}) {
	}

	if backend.received.System != "be useful" {
		t.Errorf("system = %q, want the configured prompt", backend.received.System)
	}
	if backend.received.MaxTokens != nacelle.DefaultMaxTokens {
		t.Errorf("max tokens = %d, want the default filled in", backend.received.MaxTokens)
	}
	if len(backend.received.Messages) != 1 || !reflect.DeepEqual(backend.received.Messages[0], nacelle.UserText("hi")) {
		t.Errorf("messages = %+v, want the conversation passed through", backend.received.Messages)
	}
}

// A ToolCall only makes sense as the model's own move, and a ToolResult only
// as an answer to one — so a conversation putting either on the wrong side is
// refused before either backend ever sees it, rather than sent and left to the
// two backends to disagree about (one rejects it over the wire, the other
// drops it in silence).
func TestAPartOnTheWrongRoleIsRefusedBeforeTheBackendIsCalled(t *testing.T) {
	toolCallFromTheUser := []nacelle.Message{
		{Role: nacelle.RoleUser, Parts: []nacelle.Part{
			nacelle.ToolCall{ID: "1", Name: "search", Finished: true},
		}},
	}
	toolResultFromTheAssistant := []nacelle.Message{
		{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{
			nacelle.ToolResult{ID: "1", Name: "search", Result: "done"},
		}},
	}

	cases := map[string][]nacelle.Message{
		"a tool call from the user":        toolCallFromTheUser,
		"a tool result from the assistant": toolResultFromTheAssistant,
	}

	for name, conversation := range cases {
		t.Run(name, func(t *testing.T) { assertRefused(t, conversation) })
	}
}

// assertRefused runs one conversation and checks it was rejected before the
// backend was ever reached.
func assertRefused(t *testing.T, conversation []nacelle.Message) {
	t.Helper()

	backend := full()
	agent, err := nacelle.New(nacelle.Config{Backend: backend, System: "s"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	events, errs := drain(agent.Stream(context.Background(), conversation))
	if events != 1 || errs != 1 {
		t.Fatalf("stream produced %d events with %d errors, want exactly one error and nothing else", events, errs)
	}
	if backend.called {
		t.Error("the backend was reached with a conversation the role check should have refused first")
	}
}

// drain counts every event and error a stream produces.
func drain(stream iter.Seq2[nacelle.Event, error]) (events, errs int) {
	for _, err := range stream {
		events++
		if err != nil {
			errs++
		}
	}
	return events, errs
}

func TestUsageAccumulates(t *testing.T) {
	sum := nacelle.Usage{InputTokens: 10, OutputTokens: 3, CacheReadTokens: 1}.
		Add(nacelle.Usage{InputTokens: 5, OutputTokens: 7, CacheCreationTokens: 2})

	want := nacelle.Usage{InputTokens: 15, OutputTokens: 10, CacheReadTokens: 1, CacheCreationTokens: 2}
	if sum != want {
		t.Errorf("sum = %+v, want %+v", sum, want)
	}
	if sum.Total() != 28 {
		t.Errorf("total = %d, want 28", sum.Total())
	}
}

// A write is not a hit. Counting CacheCreationTokens as attempted-but-missed
// would report a low hit rate on a run's very first turn, when every prefix
// is written and none has had the chance to be read back yet.
func TestCacheHitRateExcludesWritesFromTheDenominator(t *testing.T) {
	usage := nacelle.Usage{InputTokens: 50, CacheReadTokens: 50, CacheCreationTokens: 900}
	if rate := usage.CacheHitRate(); rate != 0.5 {
		t.Errorf("rate = %v, want 0.5 — the 900 written tokens should not count against it", rate)
	}
}

// No cacheable input at all is not a miss; there was nothing to hit.
func TestCacheHitRateIsZeroWithNothingToHit(t *testing.T) {
	if rate := (nacelle.Usage{}).CacheHitRate(); rate != 0 {
		t.Errorf("rate = %v, want 0 rather than a divide-by-zero", rate)
	}
}
