package anthropic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// countingServer answers every request with a fixed token count, and hands
// the request body to the test so it can assert on what was actually sent —
// this is a plain JSON endpoint, not the SSE one stream_test.go's stub()
// serves, so it needs its own httptest server rather than sharing that one.
func countingServer(t *testing.T, count int64, seen *string) *sdk.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the request body: %v", err)
		}
		*seen = string(body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"input_tokens": %d, "context_management": {}}`, count)
	}))
	t.Cleanup(server.Close)
	client := sdk.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("test"))
	return &client
}

// The count the API reports has to reach the caller as the plain number it
// is — nothing here should stand between InputTokens and the return value.
func TestCountTokensReturnsTheAPIsFigure(t *testing.T) {
	var sent string
	backend := New(Config{Client: countingServer(t, 1234, &sent), Model: "test-model"})

	got, err := backend.CountTokens(context.Background(), nacelle.Request{
		System:   "be useful",
		Messages: []nacelle.Message{nacelle.UserText("hi")},
	})
	if err != nil {
		t.Fatalf("CountTokens: %v", err)
	}
	if got != 1234 {
		t.Errorf("count = %d, want 1234", got)
	}
	if sent == "" {
		t.Fatal("the request body was never captured")
	}
}

// A tool declared for the run has to be counted too, or the number tells a
// caller their request fits when the one the runner actually sends does not.
func TestCountTokensDeclaresTheToolsToo(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(context.Context, struct{}) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	var sent string
	backend := New(Config{Client: countingServer(t, 10, &sent), Model: "test-model"})

	if _, err := backend.CountTokens(context.Background(), nacelle.Request{
		System: "be useful",
		Tools:  []nacelle.Tool{tool},
	}); err != nil {
		t.Fatalf("CountTokens: %v", err)
	}

	if !strings.Contains(sent, `"name":"search"`) {
		t.Errorf("request body = %s, want the tool's schema declared", sent)
	}
}
