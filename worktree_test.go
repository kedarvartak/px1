package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// worktreeRepo builds a repository with one committed file and a second
// checkout added through `git worktree add`, the shape an agent working on a
// branch leaves behind. It returns the main root and the worktree path.
func worktreeRepo(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	if r, err := filepath.EvalSymlinks(base); err == nil {
		base = r
	}
	root := filepath.Join(base, "main")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(root, "init", "-q", "-b", "main")
	run(root, "add", ".")
	run(root, "commit", "-qm", "init")
	wt := filepath.Join(base, "feature")
	run(root, "worktree", "add", "-q", "-b", "fix/bug", wt)
	if err := os.WriteFile(filepath.Join(wt, "fix.go"), []byte("package main\n\nfunc fix() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, wt
}

func serverAt(t *testing.T, root string) *Server {
	t.Helper()
	isolateSettings(t)
	ix := NewIndex(root)
	ix.Build()
	return NewServer(ix, newLSPManager(root, false))
}

func TestWorktreesListsEveryCheckout(t *testing.T) {
	root, wt := worktreeRepo(t)
	list := worktrees(root)
	if len(list) != 2 {
		t.Fatalf("worktrees = %#v", list)
	}
	if !list[0].Main || !list[0].Current || list[0].Branch != "main" {
		t.Fatalf("main worktree = %#v", list[0])
	}
	if list[1].Main || list[1].Current || list[1].Branch != "fix/bug" || list[1].Path != wt || list[1].Name != "feature" {
		t.Fatalf("second worktree = %#v", list[1])
	}
	// Seen from the other checkout, "current" moves with it.
	from := worktrees(wt)
	if from[0].Current || !from[1].Current {
		t.Fatalf("current flags from the worktree: %#v", from)
	}
	if got := worktrees(t.TempDir()); len(got) != 0 {
		t.Fatalf("worktrees outside a repo = %#v", got)
	}
}

func TestWorktreeSwitchRepointsWorkspace(t *testing.T) {
	root, wt := worktreeRepo(t)
	s := serverAt(t, root)

	post := func(path string) (int, map[string]any) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"path": path})
		req := httptest.NewRequest(http.MethodPost, "/api/worktree/switch", bytes.NewReader(body))
		req.Host = "127.0.0.1:7777"
		req.Header.Set("Origin", "http://127.0.0.1:7777")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	if code, _ := post(filepath.Join(t.TempDir(), "elsewhere")); code != http.StatusBadRequest {
		t.Fatalf("switch to a directory git does not list = %d", code)
	}

	code, out := post(wt)
	if code != 200 || out["switched"] != true {
		t.Fatalf("switch = %d %v", code, out)
	}
	if s.ix.Root() != wt {
		t.Fatalf("index root = %q, want %q", s.ix.Root(), wt)
	}
	kids, _ := s.ix.Children("")
	var names []string
	for _, k := range kids {
		names = append(names, k.Name)
	}
	found := false
	for _, n := range names {
		if n == "fix.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tree after switch = %v, want the worktree's files", names)
	}
	for _, n := range names {
		// In a linked worktree .git is a file, not a directory.
		if n == ".git" {
			t.Fatalf("tree lists the worktree gitfile: %v", names)
		}
	}

	// A review session started here belongs to this checkout, not the one px1
	// was launched in.
	if _, err := s.review.Start(); err != nil {
		t.Fatal(err)
	}
	if active, _ := s.review.Active(); active == nil || active.Root != wt {
		t.Fatalf("review session root = %#v", active)
	}
	if code, out := post(wt); code != 200 || out["switched"] != false {
		t.Fatalf("switching to the current worktree = %d %v", code, out)
	}
	if code, _ := post(root); code != 200 || s.ix.Root() != root {
		t.Fatalf("switch back = %d root=%q", code, s.ix.Root())
	}
	if active, _ := s.review.Active(); active != nil {
		t.Fatalf("session followed the switch: %#v", active)
	}
}
