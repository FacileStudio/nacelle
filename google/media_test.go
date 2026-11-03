package google_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/google"
)

func mediaBackend(t *testing.T, captured *map[string]any) *google.Backend {
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
	backend, err := google.New(google.Config{APIKey: "test-key", BaseURL: server.URL, Model: "gemini-3.7-flash"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return backend
}

func TestImagePartSerialisesAsDataURI(t *testing.T) {
	var captured map[string]any
	backend := mediaBackend(t, &captured)
	collectStream(t, backend.Stream(context.Background(), nacelle.Request{System: "test", Messages: []nacelle.Message{{
		Role:  nacelle.RoleUser,
		Parts: []nacelle.Part{nacelle.Image{MediaType: "image/jpeg", Data: []byte("jpg")}},
	}}}))

	content := captured["messages"].([]any)[1].(map[string]any)["content"]
	want := []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/jpeg;base64,anBn"}}}
	if !reflect.DeepEqual(content, want) {
		t.Fatalf("content = %#v, want %#v", content, want)
	}
}

func TestDocumentPartIsRefused(t *testing.T) {
	var captured map[string]any
	backend := mediaBackend(t, &captured)
	var failure error
	for _, err := range backend.Stream(context.Background(), nacelle.Request{System: "test", Messages: []nacelle.Message{{
		Role:  nacelle.RoleUser,
		Parts: []nacelle.Part{nacelle.Document{Name: "a.pdf", MediaType: "application/pdf", Data: []byte("pdf")}},
	}}}) {
		if err != nil {
			failure = err
		}
	}
	var unsupported *nacelle.Unsupported
	if !errors.As(failure, &unsupported) || unsupported.Backend != "google" {
		t.Fatalf("err = %v, want an Unsupported error from google", failure)
	}
	if captured != nil {
		t.Fatalf("a request was sent: %v", captured)
	}
}
