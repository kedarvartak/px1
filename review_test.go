package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestReviewCommentsPersistAndReportStaleAnchors(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	path := filepath.Join(root, "handler.go")
	if err := os.WriteFile(path, []byte("package main\nfunc handler() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newReviewManager(root)
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\nfunc handler() { retry() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	comment, err := m.AddComment("handler.go", 2, 2, "Use exponential backoff.")
	if err != nil {
		t.Fatal(err)
	}
	if comment.Status != "open" || comment.FileHash == "" {
		t.Fatalf("new comment = %#v", comment)
	}
	if _, err := m.SetCommentStatus(comment.ID, "sent"); err != nil {
		t.Fatal(err)
	}
	if got, err := newReviewManager(root).Comments(); err != nil || len(got) != 1 || got[0].Status != "sent" || got[0].Stale {
		t.Fatalf("persisted comments = %#v err=%v", got, err)
	}

	if err := os.WriteFile(path, []byte("package main\nfunc handler() { retryWithBackoff() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	comments, err := m.Comments()
	if err != nil {
		t.Fatal(err)
	}
	if !comments[0].Stale {
		t.Fatalf("changed anchor was not stale: %#v", comments[0])
	}
	if _, err := m.SetCommentStatus(comment.ID, "resolved"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetCommentStatus(comment.ID, "not-a-state"); err == nil {
		t.Fatal("invalid status was accepted")
	}
}

func TestReviewPatchRequiresPreviewHashAndSupportsUndo(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	path := filepath.Join(root, "config.go")
	before := []byte("package main\nconst timeout = 120\nconst retries = 3\n")
	if err := os.WriteFile(path, before, 0o640); err != nil {
		t.Fatal(err)
	}
	m := newReviewManager(root)
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	p, err := m.ApplyPatch("config.go", 2, 2, hashBytes(before), "const timeout = 60")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(after), "package main\nconst timeout = 60\nconst retries = 3\n"; got != want {
		t.Fatalf("patched = %q, want %q", got, want)
	}
	if _, err := m.ApplyPatch("config.go", 2, 2, p.BeforeHash, "const timeout = 30"); err == nil {
		t.Fatal("stale preview was accepted")
	}
	if _, err := m.UndoPatch(p.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(before) {
		t.Fatalf("undo = %q, want %q", restored, before)
	}
	if _, err := m.UndoPatch(p.ID); err == nil {
		t.Fatal("second undo was accepted")
	}
}

func TestReviewPatchEmptyReplacementDeletesSelectedLines(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	path := filepath.Join(root, "lines.txt")
	before := []byte("line one\nline two\ninserted alpha\ninserted beta\nline three\n")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	m := newReviewManager(root)
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ApplyPatch("lines.txt", 3, 4, hashBytes(before), ""); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(after), "line one\nline two\nline three\n"; got != want {
		t.Fatalf("empty replacement = %q, want %q", got, want)
	}
}

func TestReviewHunkRevertUsesTaskBaseline(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	path := filepath.Join(root, "auth.go")
	baseline := []byte("package main\nconst timeout = 30\nconst retries = 3\n")
	if err := os.WriteFile(path, baseline, 0o644); err != nil {
		t.Fatal(err)
	}
	m := newReviewManager(root)
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	changed := []byte("package main\nconst timeout = 120\nconst retries = 3\n")
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := m.RevertHunk("auth.go", 2, 2, 2, 2, hashBytes(changed))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(baseline) {
		t.Fatalf("revert = %q, want %q", got, baseline)
	}
	if _, err := m.UndoPatch(p.ID); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(changed) {
		t.Fatalf("undo revert = %q, want %q", got, changed)
	}
}

func TestReviewBaselineDiffUsesTaskStartSnapshot(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	p := filepath.Join(root, "config.txt")
	if err := os.WriteFile(p, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newReviewManager(root)
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := m.BaselineDiff("config.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-before") || !strings.Contains(diff, "+after") {
		t.Fatalf("unexpected baseline diff:\n%s", diff)
	}
}

func TestReviewSessionSkipsIgnoredPathsAndSymlinks(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "coverage/\n")
	write("backend/app.js", "export const a = 1\n")
	write("backend/node_modules/acorn/bin/acorn", "#!/usr/bin/env node\n")
	write("backend/coverage/report.txt", "old\n")
	if err := os.MkdirAll(filepath.Join(root, "backend", "node_modules", ".bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../acorn/bin/acorn", filepath.Join(root, "backend", "node_modules", ".bin", "acorn")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink("app.js", filepath.Join(root, "backend", "link.js")); err != nil {
		t.Fatal(err)
	}

	m := newReviewManager(root)
	session, err := m.Start()
	if err != nil {
		t.Fatalf("start failed on a repo with node_modules symlinks: %v", err)
	}
	for _, f := range session.Files {
		if strings.Contains(f.Path, "node_modules") || strings.Contains(f.Path, "coverage") || f.Path == "backend/link.js" {
			t.Fatalf("snapshot included %s", f.Path)
		}
	}

	write("backend/app.js", "export const a = 2\n")
	write("backend/node_modules/acorn/bin/acorn", "changed\n")
	write("backend/coverage/report.txt", "new\n")
	q, err := m.Queue()
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Items) != 1 || q.Items[0].Path != "backend/app.js" {
		t.Fatalf("queue = %#v", q.Items)
	}

	if _, err := m.Restore(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "backend", "app.js")); string(b) != "export const a = 1\n" {
		t.Fatalf("app.js not restored: %q", b)
	}
	if _, err := os.Lstat(filepath.Join(root, "backend", "node_modules", ".bin", "acorn")); err != nil {
		t.Fatalf("restore removed an ignored symlink: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "backend", "coverage", "report.txt")); string(b) != "new\n" {
		t.Fatalf("restore touched an ignored file: %q", b)
	}
}
