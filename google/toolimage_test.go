package google_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/FacileStudio/nacelle"
)

func TestReplayedToolResultImagesFollowTheToolMessage(t *testing.T) {
	var captured map[string]any
	backend := mediaBackend(t, &captured)
	call := nacelle.Message{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{
		nacelle.ToolCall{ID: "call_1", Name: "shoot", Input: []byte("{}"), Finished: true},
	}}
	result := nacelle.Message{Role: nacelle.RoleUser, Parts: []nacelle.Part{nacelle.ToolResult{
		ID: "call_1", Name: "shoot", Result: "shot",
		Images: []nacelle.Image{{MediaType: "image/png", Data: []byte("png")}},
	}}}
	collectStream(t, backend.Stream(context.Background(), nacelle.Request{
		System: "test", Messages: []nacelle.Message{nacelle.UserText("go"), call, result},
	}))

	messages := captured["messages"].([]any)
	want := []any{
		map[string]any{"type": "text", "text": "Images returned by the shoot tool call call_1:"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,cG5n"}},
	}
	if messages[3].(map[string]any)["role"] != "tool" || !reflect.DeepEqual(messages[4].(map[string]any)["content"], want) {
		t.Fatalf("messages = %#v", messages)
	}
}
