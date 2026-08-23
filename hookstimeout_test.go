package nacelle_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

func TestWithTimeoutFailsClosedOnABeforeHook(t *testing.T) {
	slow := nacelle.WithTimeout(20*time.Millisecond, func(ctx context.Context, _ nacelle.HookEvent) nacelle.HookResult {
		select {
		case <-time.After(2 * time.Second):
			return nacelle.HookResult{}
		case <-ctx.Done():
			return nacelle.HookResult{}
		}
	})
	started := time.Now()
	_, _, err := runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: {slow},
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v; want a timeout denial", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("RunTool took %s; the timeout should have cut in far earlier", elapsed)
	}
}

// The timeout's whole point is that the work it cut short hears about it:
// a wrapper that returns while the hook keeps running leaks a goroutine,
// and for an exec-based hook, a process, per call.
func TestWithTimeoutCancelsTheHookContext(t *testing.T) {
	cancelled := make(chan struct{})
	slow := nacelle.WithTimeout(20*time.Millisecond, func(ctx context.Context, _ nacelle.HookEvent) nacelle.HookResult {
		<-ctx.Done()
		close(cancelled)
		return nacelle.HookResult{}
	})
	runHookTool(t, map[nacelle.HookPoint][]nacelle.Hook{
		nacelle.BeforeToolCall: {slow},
	})
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("the timed-out hook's context was never cancelled")
	}
}
