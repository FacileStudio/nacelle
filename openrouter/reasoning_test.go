package openrouter

import (
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/mcp"
)

func TestReasoningIsForwardedOnlyWhenAskedFor(t *testing.T) {
	const withReasoning = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","reasoning":"thinking about it","content":""},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}

data: {"id":"g","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0.1}}

data: [DONE]

`
	backend, handler := serve(t, withReasoning)

	silent := collect(t, backend, nacelle.Request{System: "s"})
	if got := kinds(silent, nacelle.KindThinking); len(got) != 0 {
		t.Errorf("reasoning was streamed without being asked for: %+v", got)
	}
	if _, sent := handler.requests[0]["reasoning"]; sent {
		t.Error("a reasoning parameter was sent for a request that wanted none")
	}

	loud := collect(t, backend, nacelle.Request{System: "s", Thinking: nacelle.Thinking{Show: true}})
	thinking := kinds(loud, nacelle.KindThinking)
	if len(thinking) != 1 || thinking[0].Text != "thinking about it" {
		t.Errorf("thinking = %+v, want the reasoning delta", thinking)
	}
}

// exclude is the parameter this backend must never send. It does not stop the
// model reasoning and does not stop the reasoning being billed, it stops the
// reasoning coming back, and what comes back is what the tool loop replays to
// keep the model's train of thought intact.
func TestDepthReachesTheRequestAndExclusionNeverDoes(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		thinking nacelle.Thinking
		want     map[string]any
	}{
		{"effort alone", nacelle.Thinking{Effort: nacelle.EffortHigh}, map[string]any{"effort": "high"}},
		{"budget alone", nacelle.Thinking{Budget: 2048}, map[string]any{"max_tokens": 2048.0}},
		{"a budget beats an effort", nacelle.Thinking{Effort: nacelle.EffortMax, Budget: 4096}, map[string]any{"max_tokens": 4096.0}},
		{"off", nacelle.Thinking{Effort: nacelle.EffortNone}, map[string]any{"enabled": false}},
		{"off beats a ceiling", nacelle.Thinking{Effort: nacelle.EffortNone, Budget: 4096}, map[string]any{"enabled": false}},
		{"watching without a depth", nacelle.Thinking{Show: true}, map[string]any{"enabled": true}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			backend, handler := serve(t, withKeepalive)
			collect(t, backend, nacelle.Request{System: "s", Thinking: testCase.thinking})

			reasoning, ok := handler.requests[0]["reasoning"].(map[string]any)
			if !ok {
				t.Fatalf("no reasoning parameter: %+v", handler.requests[0])
			}
			if _, sent := reasoning["exclude"]; sent {
				t.Errorf("exclude was sent: %+v", reasoning)
			}
			assertOneDepth(t, reasoning)
			if !maps.Equal(reasoning, testCase.want) {
				t.Errorf("reasoning = %+v, want %+v", reasoning, testCase.want)
			}
		})
	}
}

// A reasoning block arrives one fragment at a time and every fragment looks
// complete, which is what makes keeping the last one seem reasonable until you
// read what goes back out. Measured against stealth/ox-alpha on 2026-08-23:
// fourteen chunks, all index 0, the last of them the single token "27.".
func TestFragmentedReasoningIsRejoinedBeforeItGoesBack(t *testing.T) {
	backend, handler := serve(t, fragmentedReasoning, finalAnswer)
	tool, err := nacelle.NewTool("search", "Find things", func(context.Context, searchInput) (string, error) {
		return "2026-08-23", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	collect(t, backend, nacelle.Request{System: "s", Tools: []nacelle.Tool{tool}})

	blocks := reasoningDetails(t, handler.requests[1])
	if len(blocks) != 1 {
		t.Fatalf("sent %d reasoning blocks, want the three fragments rejoined into 1: %+v", len(blocks), blocks)
	}
	block := blocks[0].(map[string]any)
	if block["text"] != "Check the date first." {
		t.Errorf("text = %q, want the whole chain of thought", block["text"])
	}
	if block["format"] != "unknown" {
		t.Errorf("format = %v, want the value the first fragment carried", block["format"])
	}
}

// Two blocks at different positions are two thoughts, and the API refuses a
// sequence that does not match what the model produced, so they must neither
// merge nor swap.
func TestSeparateReasoningBlocksKeepTheirOrder(t *testing.T) {
	backend, handler := serve(t, twoReasoningBlocks, finalAnswer)
	tool, err := nacelle.NewTool("search", "Find things", func(context.Context, searchInput) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	collect(t, backend, nacelle.Request{System: "s", Tools: []nacelle.Tool{tool}})

	blocks := reasoningDetails(t, handler.requests[1])
	if len(blocks) != 2 {
		t.Fatalf("sent %d reasoning blocks, want 2: %+v", len(blocks), blocks)
	}
	first := blocks[0].(map[string]any)
	second := blocks[1].(map[string]any)
	if first["text"] != "first thought" || second["summary"] != "second thought" {
		t.Errorf("blocks = %+v, want them in the order the model produced", blocks)
	}
}

// reasoningDetails digs the blocks out of the assistant message a follow-up
// request replays, which is the only place their shape can be checked: the
// OpenAI schema has no field for them, so they travel as an extra field and
// there is no typed accessor to read them back through.
func reasoningDetails(t *testing.T, request map[string]any) []any {
	t.Helper()
	messages, _ := request["messages"].([]any)
	for _, entry := range messages {
		message, _ := entry.(map[string]any)
		if message["role"] != "assistant" {
			continue
		}
		blocks, ok := message["reasoning_details"].([]any)
		if !ok {
			t.Fatalf("the replayed assistant message carried no reasoning: %+v", message)
		}
		return blocks
	}
	t.Fatalf("no assistant message was replayed: %+v", request)
	return nil
}

const fragmentedReasoning = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","reasoning":"Check ","reasoning_details":[{"type":"reasoning.text","text":"Check ","format":"unknown","index":0}],"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"search","arguments":""}}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"reasoning":"the date","reasoning_details":[{"type":"reasoning.text","text":"the date","index":0}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"reasoning":" first.","reasoning_details":[{"type":"reasoning.text","text":" first.","index":0}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"query\":\"date\"}"}}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: {"id":"g","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10,"cost":0.0001}}

data: [DONE]

`

const twoReasoningBlocks = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","reasoning_details":[{"type":"reasoning.text","text":"first ","index":0}],"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"search","arguments":"{}"}}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.text","text":"thought","index":0}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.summary","summary":"second thought","index":1}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: {"id":"g","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10,"cost":0.0001}}

data: [DONE]

`

// Once the first token ships the status is committed, so a provider failure
// arrives in-band rather than as an HTTP error.
func TestAMidStreamErrorEndsTheRun(t *testing.T) {
	const withError = `data: {"id":"g","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}

data: {"id":"g","error":{"code":429,"message":"Rate limit exceeded","metadata":{"error_type":"rate_limit_exceeded"}},"choices":[{"index":0,"delta":{"content":""},"finish_reason":"error"}]}

data: [DONE]

`
	backend, _ := serve(t, withError)

	var failed error
	for _, err := range backend.Stream(context.Background(), nacelle.Request{System: "s"}) {
		if err != nil {
			failed = err
			break
		}
	}
	if failed == nil {
		t.Fatal("a mid-stream error was swallowed")
	}
	if !strings.Contains(failed.Error(), "Rate limit") {
		t.Errorf("error = %v, want the provider's message", failed)
	}
}

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	if _, err := New(Config{APIKey: "k"}); err == nil {
		t.Error("a backend with no model was accepted")
	}
	t.Setenv("OPENROUTER_API_KEY", "")
	if _, err := New(Config{Model: "m"}); err == nil {
		t.Error("a backend with no API key was accepted")
	}
}

// MCP is the capability this backend cannot have, and saying so is the point:
// an agent that needs it fails at construction instead of running without it.
func TestMCPIsRefusedAtConstruction(t *testing.T) {
	backend, _ := serve(t, withKeepalive)

	_, err := nacelle.New(nacelle.Config{
		Backend: backend,
		System:  "s",
		MCP:     []mcp.Server{{Name: "p", URL: "https://p.test"}},
	})
	if err == nil {
		t.Fatal("an agent asking for MCP was built on a backend without it")
	}
	if !strings.Contains(err.Error(), "MCP") {
		t.Errorf("error = %v, want it to name MCP", err)
	}
}

// assertOneDepth holds the rule the fake server cannot: OpenRouter answers a
// request naming both reasoning.effort and reasoning.max_tokens with a 400,
// "Only one of reasoning.effort and reasoning.max_tokens can be specified".
//
// A test server that accepts any JSON will never catch that, which is exactly
// how it shipped once. Asserting the rule rather than the shape is the closest
// a test without a network can get to the provider's own validation.
func assertOneDepth(t *testing.T, reasoning map[string]any) {
	t.Helper()
	_, byEffort := reasoning["effort"]
	_, byBudget := reasoning["max_tokens"]
	if byEffort && byBudget {
		t.Errorf("both spellings of the depth were sent, which the gateway rejects: %+v", reasoning)
	}
}
