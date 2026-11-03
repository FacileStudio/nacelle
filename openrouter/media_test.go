package openrouter

import (
	"reflect"
	"testing"

	"github.com/FacileStudio/nacelle"
)

func TestImageAndDocumentPartsSerialiseAsDataURIs(t *testing.T) {
	backend, handler := serve(t, "data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	parts := []nacelle.Part{
		nacelle.Text{Text: "look"},
		nacelle.Image{MediaType: "image/webp", Data: []byte("webp")},
		nacelle.Document{Name: "a.pdf", MediaType: "application/pdf", Data: []byte("pdf")},
	}
	message := nacelle.Message{Role: nacelle.RoleUser, Parts: parts}
	collect(t, backend, nacelle.Request{System: "s", Messages: []nacelle.Message{message}})

	content := handler.requests[0]["messages"].([]any)[1].(map[string]any)["content"]
	want := []any{
		map[string]any{"type": "text", "text": "look"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/webp;base64,d2VicA=="}},
		map[string]any{"type": "file", "file": map[string]any{"filename": "a.pdf", "file_data": "data:application/pdf;base64,cGRm"}},
	}
	if !reflect.DeepEqual(content, want) {
		t.Fatalf("content = %#v, want %#v", content, want)
	}
}
