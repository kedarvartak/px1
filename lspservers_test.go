package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A crashed client is replaced by a fresh spawn, a bounded number of times.
func TestCrashedServerIsRespawned(t *testing.T) {
	def := lspServerDef{Name: "gone", Cmd: []string{"px1-test-no-such-language-server"}, Exts: []string{".go"}}
	dead := func() *lspClient {
		c := newLSPClient(def, t.TempDir())
		c.fail(errors.New("gone exited"))
		return c
	}
	m := &lspManager{
		root: t.TempDir(), enabled: true,
		byExt:    map[string]*lspServerDef{".go": &def},
		clients:  map[string]*lspClient{"gone": dead()},
		starting: map[string]chan struct{}{},
		failed:   map[string]string{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The respawn is attempted; the binary does not exist, so it fails to start.
	if _, err := m.client(ctx, "x.go"); !isErrFailed(err) {
		t.Fatalf("client() = %v, want a failed respawn rather than the stale crash", err)
	}
	if m.restarts["gone"] != 1 {
		t.Errorf("restarts = %d, want 1", m.restarts["gone"])
	}

	// Out of budget: the crash is reported and nothing is spawned.
	m.mu.Lock()
	delete(m.failed, "gone")
	m.clients["gone"] = dead()
	m.restarts["gone"] = maxLSPRestarts
	m.mu.Unlock()
	if _, err := m.client(ctx, "x.go"); err == nil || isErrFailed(err) {
		t.Errorf("client() past the restart budget = %v, want the crash error", err)
	}
}

func isErrFailed(err error) bool {
	_, ok := err.(errFailed)
	return ok
}
