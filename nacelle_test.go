package nacelle_test

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/mcp"
)

// stub is a backend that records what it was asked and can be told what it
// supports, so the capability rules are testable without a network.
type stub struct {
	can      nacelle.Capabilities
	received nacelle.Request
}

func (s *stub) Name() string                       { return "stub" }
func (s *stub) Capabilities() nacelle.Capabilities { return s.can }

func (s *stub) Stream(_ context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
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

	for range agent.Stream(context.Background(), []nacelle.Message{{Text: "hi"}}) {
	}

	if backend.received.System != "be useful" {
		t.Errorf("system = %q, want the configured prompt", backend.received.System)
	}
	if backend.received.MaxTokens != nacelle.DefaultMaxTokens {
		t.Errorf("max tokens = %d, want the default filled in", backend.received.MaxTokens)
	}
	if len(backend.received.Messages) != 1 || backend.received.Messages[0].Text != "hi" {
		t.Errorf("messages = %+v, want the conversation passed through", backend.received.Messages)
	}
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
