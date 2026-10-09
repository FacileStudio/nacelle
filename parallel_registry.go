package nacelle

import (
	"context"
	"sync"
)

// ParallelRegistry holds the cancel functions of live detached fan-outs by
// batch key, so NewParallelCancelTool can stop them.
type ParallelRegistry struct {
	mu      sync.Mutex
	byBatch map[string]context.CancelFunc
}

// NewParallelRegistry builds an empty registry for parallel cancel coordination.
func NewParallelRegistry() *ParallelRegistry {
	return &ParallelRegistry{
		byBatch: make(map[string]context.CancelFunc),
	}
}

// Register stores a batch's cancel function.
func (r *ParallelRegistry) Register(batch string, cancel context.CancelFunc) {
	r.mu.Lock()
	r.byBatch[batch] = cancel
	r.mu.Unlock()
}

// Drop removes a batch's cancel function.
func (r *ParallelRegistry) Drop(batch string) {
	r.mu.Lock()
	delete(r.byBatch, batch)
	r.mu.Unlock()
}

// Cancel calls the cancel function for a batch if it exists. It returns true
// if the batch was found and cancelled.
func (r *ParallelRegistry) Cancel(batch string) bool {
	r.mu.Lock()
	cancel, ok := r.byBatch[batch]
	r.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}
