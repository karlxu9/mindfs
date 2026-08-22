package relay

import (
	"context"
	"testing"
)

// Stop must be safe before Start, idempotent, and must cancel the run
// context that keeps the relay WS connection alive (R-1.1 / T10). The test is
// self-contained on purpose: this package has a known ordering-sensitive
// upstream test, so nothing here depends on package-level state.
func TestManagerStopIsSafeAndCancelsRunContext(t *testing.T) {
	m := &Manager{}
	m.Stop()
	m.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()

	m.Stop()
	if ctx.Err() == nil {
		t.Fatal("Stop did not cancel the relay run context")
	}
	m.Stop()

	var nilManager *Manager
	nilManager.Stop()
}
