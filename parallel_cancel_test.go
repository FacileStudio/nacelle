package nacelle

import (
	"context"
	"testing"
)

func TestCancelParallelReportsAnUnknownBatch(t *testing.T) {
	registry := NewParallelRegistry()
	if registry.Cancel("psa-ghost") {
		t.Error("registry.Cancel reported cancelling a batch that never existed")
	}
}

func TestCancelParallelCancelsALiveBatch(t *testing.T) {
	registry := NewParallelRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	registry.Register("psa-live", cancel)
	defer registry.Drop("psa-live")

	if !registry.Cancel("psa-live") {
		t.Fatal("registry.Cancel did not find a registered live batch")
	}
	select {
	case <-ctx.Done():
	default:
		t.Error("cancelling the batch did not cancel its context")
	}
}

func TestParallelCancelToolResponds(t *testing.T) {
	registry := NewParallelRegistry()
	tool, err := NewParallelCancelTool(registry)
	if err != nil {
		t.Fatalf("NewParallelCancelTool: %v", err)
	}
	if tool.Name() != ParallelCancelToolName {
		t.Errorf("name = %q, want %q", tool.Name(), ParallelCancelToolName)
	}
}
