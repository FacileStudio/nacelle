package nacelle

import (
	"context"
	"testing"
)

func TestCancelParallelReportsAnUnknownBatch(t *testing.T) {
	if CancelParallel("psa-ghost") {
		t.Error("CancelParallel reported cancelling a batch that never existed")
	}
}

func TestCancelParallelCancelsALiveBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	registerParallelCancel("psa-live", cancel)
	defer dropParallelCancel("psa-live")

	if !CancelParallel("psa-live") {
		t.Fatal("CancelParallel did not find a registered live batch")
	}
	select {
	case <-ctx.Done():
	default:
		t.Error("cancelling the batch did not cancel its context")
	}
}

func TestParallelCancelToolResponds(t *testing.T) {
	tool, err := NewParallelCancelTool()
	if err != nil {
		t.Fatalf("NewParallelCancelTool: %v", err)
	}
	if tool.Name() != ParallelCancelToolName {
		t.Errorf("name = %q, want %q", tool.Name(), ParallelCancelToolName)
	}
}