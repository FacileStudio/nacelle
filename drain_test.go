package nacelle_test

import (
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// drainText runs a stream to completion, returning the accumulated text and
// failing on any stream error.
func drainText(t *testing.T, seq func(yield func(nacelle.Event, error) bool)) string {
	t.Helper()
	var b strings.Builder
	seq(func(event nacelle.Event, err error) bool {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		if event.Kind == nacelle.KindText {
			b.WriteString(event.Text)
		}
		return true
	})
	return b.String()
}
