package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func reviewPost(t *testing.T, s *Server, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Host = "127.0.0.1:7777"
	req.Header.Set("Origin", "http://127.0.0.1:7777")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestReviewSessionRestoresTaskBaselineAndPersists(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	write("already-dirty.txt", "before\n")
	write("nested/keep.txt", "keep\n")
	m := newReviewManager(root)
	session, err := m.Start()
	if err != nil {
		t.Fatal(err)
	}
	if session.ID == "" || len(session.Files) != 2 {
		t.Fatalf("bad session: %#v", session)
	}
	// A new manager simulates a px1 restart and must find the same active task.
	if again, changed := newReviewManager(root).Active(); again == nil || len(changed) != 0 {
		t.Fatalf("restart active=%#v changed=%v", again, changed)
	}

	write("already-dirty.txt", "agent changed it\n")
	if err := os.Remove(filepath.Join(root, "nested", "keep.txt")); err != nil {
		t.Fatal(err)
	}
	write("agent/new.txt", "new\n")
	if err := os.Mkdir(filepath.Join(root, "empty-agent-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, changed := m.Active()
	if !reflect.DeepEqual(changed, []string{"agent/new.txt", "already-dirty.txt", "nested/keep.txt"}) {
		t.Fatalf("changed = %v", changed)
	}

	if _, err := m.Restore(); err != nil {
		t.Fatal(err)
	}
	if got := read("already-dirty.txt"); got != "before\n" {
		t.Fatalf("dirty file = %q", got)
	}
	if got := read("nested/keep.txt"); got != "keep\n" {
		t.Fatalf("restored file = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "agent", "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new file survives restore: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "empty-agent-dir")); !os.IsNotExist(err) {
		t.Fatalf("new directory survives restore: %v", err)
	}
	if _, changed = m.Active(); len(changed) != 0 {
		t.Fatalf("changed after restore = %v", changed)
	}
	if _, err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if active, _ := newReviewManager(root).Active(); active != nil {
		t.Fatal("closed session reappeared after restart")
	}
}

func TestReviewSessionHTTPUsesLocalPostGuard(t *testing.T) {
	isolateSettings(t)
	s, _ := newTestServer(t)
	if code, body := reviewPost(t, s, "/api/review/session/start"); code != http.StatusOK || body["active"] == nil {
		t.Fatalf("start = %d %#v", code, body)
	}
	if code, body := get(t, s, "/api/review/session"); code != http.StatusOK || body["active"] == nil {
		t.Fatalf("status = %d %#v", code, body)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/review/session/restore", nil)
	req.Host = "127.0.0.1:7777"
	req.Header.Set("Origin", "http://evil.example.com")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin restore = %d, want 403", rec.Code)
	}
}

func TestReviewQueueTracksReviewStateAndStaleness(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "before a\n")
	write("b.go", "before b\n")
	m := newReviewManager(root)
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	write("a.go", "agent a\n")
	write("b.go", "agent b\n")

	q, err := m.Queue()
	if err != nil {
		t.Fatal(err)
	}
	if q.Total != 2 || q.Remaining != 2 || q.Reviewed != 0 {
		t.Fatalf("initial queue = %#v", q)
	}
	if _, err := m.Mark("a.go", "reviewed"); err != nil {
		t.Fatal(err)
	}
	q, err = m.Queue()
	if err != nil {
		t.Fatal(err)
	}
	if q.Reviewed != 1 || q.Remaining != 1 || q.Stale != 0 {
		t.Fatalf("reviewed queue = %#v", q)
	}

	// A new write never inherits a previous decision.
	write("a.go", "agent corrected a\n")
	q, err = newReviewManager(root).Queue()
	if err != nil {
		t.Fatal(err)
	}
	if q.Reviewed != 0 || q.Stale != 1 || q.Remaining != 2 {
		t.Fatalf("stale queue = %#v", q)
	}
	if _, err := m.Mark("b.go", "blocked"); err != nil {
		t.Fatal(err)
	}
	q, err = m.Queue()
	if err != nil {
		t.Fatal(err)
	}
	var blocked bool
	for _, item := range q.Items {
		if item.Path == "b.go" && item.State == "blocked" {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("blocked item missing: %#v", q.Items)
	}
}
