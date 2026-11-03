package anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func mediaRequestBody(t *testing.T, request nacelle.Request) map[string]any {
	t.Helper()
	var captured []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the request body: %v", err)
		}
		captured = body
		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := w.Write([]byte(answeringTurn(t))); err != nil {
			t.Errorf("serving the turn: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	client := sdk.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("test"))

	collect(t, New(Config{Client: &client}), request)

	var decoded map[string]any
	if err := json.Unmarshal(captured, &decoded); err != nil {
		t.Fatalf("decoding the captured request: %v", err)
	}
	return decoded
}

func TestImageAndDocumentPartsSerialiseAsBase64Blocks(t *testing.T) {
	parts := []nacelle.Part{
		nacelle.Text{Text: "look"},
		nacelle.Image{MediaType: "image/png", Data: []byte("png")},
		nacelle.Document{Name: "a.pdf", MediaType: "application/pdf", Data: []byte("pdf")},
	}
	message := nacelle.Message{Role: nacelle.RoleUser, Parts: parts}
	body := mediaRequestBody(t, nacelle.Request{MaxTokens: 1024, Messages: []nacelle.Message{message}})

	content := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
	want := []any{
		map[string]any{"type": "text", "text": "look"},
		map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "cG5n"}},
		map[string]any{"type": "document", "title": "a.pdf", "source": map[string]any{"type": "base64", "media_type": "application/pdf", "data": "cGRm"}},
	}
	if !reflect.DeepEqual(content, want) {
		t.Fatalf("content = %#v, want %#v", content, want)
	}
}
