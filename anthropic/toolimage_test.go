package anthropic

import (
	"reflect"
	"testing"

	"github.com/FacileStudio/nacelle"
)

func TestToolResultImagesTravelInsideTheToolResultBlock(t *testing.T) {
	call := nacelle.Message{Role: nacelle.RoleAssistant, Parts: []nacelle.Part{
		nacelle.ToolCall{ID: "toolu_1", Name: "browser_screenshot", Input: []byte("{}"), Finished: true},
	}}
	result := nacelle.Message{Role: nacelle.RoleUser, Parts: []nacelle.Part{nacelle.ToolResult{
		ID: "toolu_1", Name: "browser_screenshot", Result: "shot",
		Images: []nacelle.Image{{MediaType: "image/png", Data: []byte("png")}},
	}}}
	body := mediaRequestBody(t, nacelle.Request{MaxTokens: 1024, Messages: []nacelle.Message{nacelle.UserText("go"), call, result}})

	block := body["messages"].([]any)[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	want := []any{
		map[string]any{"type": "text", "text": "shot"},
		map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "cG5n"}},
	}
	if block["type"] != "tool_result" || !reflect.DeepEqual(block["content"], want) {
		t.Fatalf("block = %#v, want tool_result with content %#v", block, want)
	}
}
