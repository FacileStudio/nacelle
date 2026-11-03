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

func capturingBackend(t *testing.T, captured *map[string]any) *openai.Backend {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(captured); err != nil {
			t.Errorf("decoding the request: %v", err)
		}
		writeChunks(w, []string{
			`data: {"choices":[{"finish_reason":"stop","delta":{"content":"ok"}}],"usage":null}` + "\n\n",
			`data: [DONE]` + "\n\n",
		})
	}))
	t.Cleanup(server.Close)
	backend, err := openai.New(openai.Config{APIKey: "test-key", BaseURL: server.URL, Model: "gpt-5.4"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return backend
}

func TestImageAndDocumentPartsSerialiseAsDataURIs(t *testing.T) {
	var captured map[string]any
	backend := capturingBackend(t, &captured)
	parts := []nacelle.Part{
		nacelle.Text{Text: "look"},
		nacelle.Image{MediaType: "image/png", Data: []byte("png")},
		nacelle.Document{Name: "a.pdf", MediaType: "application/pdf", Data: []byte("pdf")},
	}
	message := nacelle.Message{Role: nacelle.RoleUser, Parts: parts}
	collectStream(t, backend.Stream(context.Background(), nacelle.Request{System: "test", Messages: []nacelle.Message{message}}))

	content := captured["messages"].([]any)[1].(map[string]any)["content"]
	want := []any{
		map[string]any{"type": "text", "text": "look"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,cG5n"}},
		map[string]any{"type": "file", "file": map[string]any{"filename": "a.pdf", "file_data": "data:application/pdf;base64,cGRm"}},
	}
	if !reflect.DeepEqual(content, want) {
		t.Fatalf("content = %#v, want %#v", content, want)
	}
}
