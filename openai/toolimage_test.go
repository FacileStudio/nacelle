package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/openai"
)

type screenshotInput struct{}

func screenshotTool(t *testing.T) nacelle.Tool {
	t.Helper()
	tool, err := nacelle.NewImageTool("shoot", "takes a screenshot",
		func(context.Context, screenshotInput) (string, []nacelle.Image, error) {
			return "shot", []nacelle.Image{{MediaType: "image/png", Data: []byte("png")}}, nil
		}, nacelle.ToolOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	return tool
}

func secondRequestServer(t *testing.T, second *map[string]any) *httptest.Server {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			writeChunks(w, []string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"shoot","arguments":"{}"}}]}}],"usage":null}` + "\n\n",
				`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":null}` + "\n\n",
				`data: [DONE]` + "\n\n",
			})
			return
		}
		if err := json.NewDecoder(r.Body).Decode(second); err != nil {
			t.Errorf("decoding the request: %v", err)
		}
		writeChunks(w, []string{
			`data: {"choices":[{"finish_reason":"stop","delta":{"content":"ok"}}],"usage":null}` + "\n\n",
			`data: [DONE]` + "\n\n",
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestImageToolResultFollowsAsLabelledUserTurn(t *testing.T) {
	var second map[string]any
	server := secondRequestServer(t, &second)
	backend, err := openai.New(openai.Config{APIKey: "test-key", BaseURL: server.URL, Model: "gpt-5.4"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	collectStream(t, backend.Stream(context.Background(), nacelle.Request{
		System: "test", Messages: []nacelle.Message{nacelle.UserText("go")}, Tools: []nacelle.Tool{screenshotTool(t)},
	}))

	messages := second["messages"].([]any)
	tool := messages[3].(map[string]any)
	user := messages[4].(map[string]any)
	want := []any{
		map[string]any{"type": "text", "text": "Images returned by the shoot tool call call_1:"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,cG5n"}},
	}
	if tool["role"] != "tool" || tool["content"] != "shot" || user["role"] != "user" || !reflect.DeepEqual(user["content"], want) {
		t.Fatalf("messages = %#v", messages)
	}
}
