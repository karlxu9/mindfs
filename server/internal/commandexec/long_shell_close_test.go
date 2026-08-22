package commandexec

import "testing"

type fakeShellProc struct {
	killTreeCalls int
	out           chan []byte
}

func (f *fakeShellProc) Output() <-chan []byte {
	if f.out == nil {
		f.out = make(chan []byte)
		close(f.out)
	}
	return f.out
}
func (f *fakeShellProc) WriteInput(p []byte) (int, error) { return len(p), nil }
func (f *fakeShellProc) Resize(cols, rows int) error      { return nil }
func (f *fakeShellProc) Interrupt() error                 { return nil }
func (f *fakeShellProc) Terminate() error                 { return nil }
func (f *fakeShellProc) KillTree() error                  { f.killTreeCalls++; return nil }
func (f *fakeShellProc) Wait() Result                     { return Result{} }

// Shutdown must take down every long-lived shell tree, and calling it again
// must be a no-op (R-1.1 / T9).
func TestLongShellManagerCloseAllKillsEverySessionOnce(t *testing.T) {
	m := &longShellManager{sessions: make(map[string]*longShellSession)}
	procA := &fakeShellProc{}
	procB := &fakeShellProc{}
	m.sessions["r1::s1::bash"] = &longShellSession{key: "r1::s1::bash", proc: procA}
	m.sessions["r2::s2::zsh"] = &longShellSession{key: "r2::s2::zsh", proc: procB}

	m.closeAll()

	if len(m.sessions) != 0 {
		t.Fatalf("sessions left after closeAll: %d", len(m.sessions))
	}
	if procA.killTreeCalls != 1 || procB.killTreeCalls != 1 {
		t.Fatalf("KillTree calls = %d/%d, want 1/1", procA.killTreeCalls, procB.killTreeCalls)
	}

	m.closeAll()
	if procA.killTreeCalls != 1 || procB.killTreeCalls != 1 {
		t.Fatalf("second closeAll re-killed: %d/%d", procA.killTreeCalls, procB.killTreeCalls)
	}
}

// The package-level entry point must tolerate an empty registry.
func TestCloseAllSessionsOnEmptyRegistryIsNoop(t *testing.T) {
	CloseAllSessions()
	CloseAllSessions()
}
