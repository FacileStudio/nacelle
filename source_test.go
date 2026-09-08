package nacelle_test

import (
	"github.com/FacileStudio/nacelle"
	"testing"
)

func TestSourceZeroConstant(t *testing.T) {
	if nacelle.ToolSourceLocal != "" {
		t.Errorf("ToolSourceLocal = %q, want the empty string", nacelle.ToolSourceLocal)
	}
	var zero nacelle.Source
	if zero != nacelle.ToolSourceLocal {
		t.Errorf("zero Source = %q, want ToolSourceLocal (%q)", zero, nacelle.ToolSourceLocal)
	}
}
func TestToolEventSourceDefaultsToLocal(t *testing.T) {
	ev := nacelle.ToolEvent{ID: "1", Name: "find"}
	if ev.Source != nacelle.ToolSourceLocal {
		t.Errorf("Source = %q, want ToolSourceLocal", ev.Source)
	}
}
