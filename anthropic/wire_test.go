package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// requestBody runs one turn against a server that keeps the request body, and
// returns the JSON the SDK actually put on the socket.
//
// thinking_test.go already asserts the sdk.BetaToolRunnerParams this package
// builds, and that is a weaker claim than it looks. It proves what this
// package intends, not what the SDK serialises, and a provider validates the
// second. The OpenRouter backend shipped a request the live gateway refused
// outright, twice on 2026-08-23, with every params-level test green, because
// nothing in the suite had ever read the bytes. This reads the bytes.
//
// The body is decoded into plain maps on purpose. Decoding it back into the
// SDK's own param types would only prove that its marshaller and its
// unmarshaller agree with each other, which they do by construction and which
// tells a reader nothing about the key names Anthropic reads. A map is the
// closest a Go test gets to the JSON a proxy would log.
//
// The server answers with a complete scripted turn rather than letting the
// call fail after the capture. Both would capture the request, which is the
// whole point, but an erroring run costs a real error path: collect fails the
// test on any stream error, so the alternative is a second mechanism for
// swallowing an error the test deliberately caused, and a run that dies mid
// stream is a worse place to be sure the request was fully written. A clean
// turn also costs nothing here, because answeringTurn already exists.
func requestBody(t *testing.T, thinking nacelle.Thinking) map[string]any {
	t.Helper()
	var captured []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the request body: %v", err)
			return
		}
		captured = body
		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := w.Write([]byte(answeringTurn(t))); err != nil {
			t.Errorf("serving the turn: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	client := sdk.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("test"))

	collect(t, New(Config{Client: &client}), nacelle.Request{MaxTokens: 4096, Thinking: thinking})

	var decoded map[string]any
	if err := json.Unmarshal(captured, &decoded); err != nil {
		t.Fatalf("decoding the captured request: %v", err)
	}
	return decoded
}

// effortOf is the level the request carries, or the empty string when it
// carries none.
//
// Absent is measured, not assumed: with no effort to send, the SDK leaves
// output_config off the request entirely rather than sending an empty object,
// so a test that looked for output_config.effort inside a body with no
// output_config would panic on the type assertion instead of reporting the
// absence it was checking for. Both spellings of absent read as "" here.
func effortOf(body map[string]any) string {
	config, ok := body["output_config"].(map[string]any)
	if !ok {
		return ""
	}
	effort, _ := config["effort"].(string)
	return effort
}

// The whole thinking block is compared, not just the fields each case cares
// about, because an extra key is as wrong as a missing one: the API reads
// budget_tokens on the enabled member and rejects it on the other two, and a
// display setting sent beside disabled thinking is a request arguing with
// itself. Equality is the only assertion that catches a key nobody meant to
// send.
//
// The numbers are float64 because encoding/json decodes every JSON number into
// one when the destination is any. 2048 written as an int would compare
// unequal to the 2048 that came off the wire, and the failure would read as a
// serialisation bug rather than the test's own type error.
func TestEveryThinkingVariantSerialisesToTheBlockTheAPIDefines(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		thinking nacelle.Thinking
		want     map[string]any
	}{
		{"asking for no reasoning sends the disabled member alone",
			nacelle.Thinking{Effort: nacelle.EffortNone, Show: true}, map[string]any{"type": "disabled"}},
		{"a budget travels as the enabled member carrying the number",
			nacelle.Thinking{Budget: 2048}, map[string]any{"type": "enabled", "budget_tokens": float64(2048)}},
		{"watching a budgeted run adds the summary and nothing else", nacelle.Thinking{Budget: 2048, Show: true},
			map[string]any{"type": "enabled", "budget_tokens": float64(2048), "display": "summarized"}},
		{"naming neither a depth nor a budget is adaptive",
			nacelle.Thinking{}, map[string]any{"type": "adaptive"}},
		{"watching an adaptive run adds the summary",
			nacelle.Thinking{Show: true}, map[string]any{"type": "adaptive", "display": "summarized"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := requestBody(t, testCase.thinking)["thinking"]
			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("thinking = %v, want %v", got, testCase.want)
			}
		})
	}
}

// output_config.effort is a closed enum of five values, so the only two things
// that can leave this package are one of those five and nothing at all.
// Anything else is a 400 from a provider this test cannot call, which is
// exactly why the value is read off the wire here rather than off the params.
func TestTheEffortOnTheWireIsOneOfTheFiveOrAbsent(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		effort nacelle.Effort
		want   string
	}{
		{"an unset level asks for nothing", "", ""},
		{"no effort travels beside disabled thinking", nacelle.EffortNone, ""},
		{"minimal clamps to the lowest level the enum has", nacelle.EffortMinimal, "low"},
		{"a level the enum has goes through unchanged", nacelle.EffortXHigh, "xhigh"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := effortOf(requestBody(t, nacelle.Thinking{Effort: testCase.effort})); got != testCase.want {
				t.Errorf("output_config.effort = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestCompactionBetaHeaderSentWhenRequested(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      Config
		ctxFunc  func(context.Context) context.Context
		wantBeta bool
	}{
		{
			name:     "compact requested via config",
			cfg:      Config{Compact: true},
			wantBeta: true,
		},
		{
			name:     "compact requested via context WithCompact",
			ctxFunc:  WithCompact,
			wantBeta: true,
		},
		{
			name:     "not requested",
			wantBeta: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var betaHeader string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				betaHeader = r.Header.Get("Anthropic-Beta")
				w.Header().Set("Content-Type", "text/event-stream")
				if _, err := w.Write([]byte(answeringTurn(t))); err != nil {
					t.Errorf("serving the turn: %v", err)
				}
			}))
			t.Cleanup(server.Close)

			client := sdk.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("test"))
			cfg := tc.cfg
			cfg.Client = &client
			backend := New(cfg)

			ctx := context.Background()
			if tc.ctxFunc != nil {
				ctx = tc.ctxFunc(ctx)
			}

			for _, err := range backend.Stream(ctx, nacelle.Request{MaxTokens: 4096}) {
				if err != nil {
					t.Fatalf("stream: %v", err)
				}
			}

			hasCompact := strings.Contains(betaHeader, BetaCompaction)
			if hasCompact != tc.wantBeta {
				t.Errorf("header %q contains %q = %v, want %v", betaHeader, BetaCompaction, hasCompact, tc.wantBeta)
			}
		})
	}
}
