package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// writeHarness creates an executable stand-in for a coding harness, outside the
// workspace so that it does not show up as a change the run made.
func writeHarness(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "harness.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// isolateSettings points the settings file at a temp dir, so a test never reads
// or overwrites the choice the developer running it has made.
func isolateSettings(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// agentPost speaks the way the browser does: POST, with an Origin that matches
// a Host localPost will accept.
func agentPost(t *testing.T, s *Server, url string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, url, nil)
	req.Host = "127.0.0.1:7777"
	req.Header.Set("Origin", "http://127.0.0.1:7777")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func agentServer(t *testing.T, root, harness string) *Server {
	t.Helper()
	isolateSettings(t)
	m, err := newAgentManager(root, harness+" {prompt}", nil)
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(root)
	ix.Build()
	s := NewServer(ix, nil)
	s.SetAgent(m)
	return s
}

// waitIdle waits for the most recently started job to finish. Tests that keep
// several jobs in flight at once poll a specific id instead.
func waitIdle(t *testing.T, s *Server) *agentJob {
	t.Helper()
	return waitIdleID(t, s, 0)
}

func waitIdleID(t *testing.T, s *Server, id int64) *agentJob {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if j := s.agent.Job(id); j != nil && !j.Running {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("harness did not finish")
	return nil
}

func startAgentJob(t *testing.T, s *Server, root, rel string, l1, l2 int, instruction string) *agentJob {
	t.Helper()
	job, err := s.agent.Start(filepath.Join(root, filepath.FromSlash(rel)), rel, l1, l2, instruction)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestAgentSpecResolution(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()

	if _, err := newAgentManager(root, "echo hello", nil); err == nil {
		t.Fatal("a template without {prompt} should be refused")
	}
	if _, err := newAgentManager(root, "px1-not-a-real-binary {prompt}", nil); err == nil {
		t.Fatal("a missing binary should be refused at startup, not on first use")
	}

	m, err := newAgentManager(root, "echo {prompt}", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name() != "echo" {
		t.Fatalf("name = %q, want echo", m.Name())
	}
	if err := m.Select("echo {prompt}"); err == nil {
		t.Fatal("a pinned harness must not be changeable from the UI")
	}

	// No flag and no saved choice: editing is available, nothing is selected.
	idle, err := newAgentManager(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if idle.Name() != "" {
		t.Fatalf("fresh manager = %q, want unselected", idle.Name())
	}
}

func TestReviewFeedbackInstructionIncludesAnchors(t *testing.T) {
	got := reviewFeedbackInstruction([]reviewCommentView{
		{reviewComment: reviewComment{Path: "src/auth.go", LineStart: 4, LineEnd: 6, Text: "Handle expiry."}},
		{reviewComment: reviewComment{Path: "tests/auth_test.go", LineStart: 10, LineEnd: 10, Text: "Cover the failure."}},
	})
	for _, want := range []string{"src/auth.go:4-6", "Handle expiry.", "tests/auth_test.go:10", "Cover the failure."} {
		if !strings.Contains(got, want) {
			t.Fatalf("feedback prompt missing %q:\n%s", want, got)
		}
	}
}

func TestAgentSelectPersistsOutsideWorkspace(t *testing.T) {
	cfg := isolateSettings(t)
	root := t.TempDir()

	m, err := newAgentManager(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Select("px1-not-a-real-binary"); err == nil {
		t.Fatal("selecting something that is not installed should fail")
	}

	// echo stands in for a harness binary that exists on every machine.
	if err := m.Select("echo {prompt}"); err != nil {
		t.Fatal(err)
	}
	if m.Name() != "echo" {
		t.Fatalf("selected = %q, want echo", m.Name())
	}

	if _, err := os.Stat(filepath.Join(cfg, "px1", "settings.json")); err != nil {
		t.Fatalf("settings file not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "settings.json")); !os.IsNotExist(err) {
		t.Fatal("settings must never be written into the workspace")
	}

	// A later run restores the choice, template and all.
	restored, err := newAgentManager(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Name() != "echo" {
		t.Fatalf("restored = %q, want echo", restored.Name())
	}

	if err := restored.Select(""); err != nil {
		t.Fatal(err)
	}
	if restored.Name() != "" {
		t.Fatalf("cleared = %q, want empty", restored.Name())
	}
	if again, _ := newAgentManager(root, "", nil); again.Name() != "" {
		t.Fatalf("clearing did not persist, got %q", again.Name())
	}
}

func TestAgentHarnessEndpointsRequireAvailability(t *testing.T) {
	s, _ := newTestServer(t) // no SetAgent: editing unavailable

	if code, _ := get(t, s, "/api/agent/job"); code != http.StatusNotFound {
		t.Fatalf("job = %d, want 404", code)
	}
	if code, _ := get(t, s, "/api/agent/harnesses"); code != http.StatusNotFound {
		t.Fatalf("removed harnesses route = %d, want 404", code)
	}
	if code, _ := agentPost(t, s, "/api/agent/edit?path=main.go&l1=1&l2=1&instruction=hi"); code != http.StatusNotFound {
		t.Fatalf("removed edit route = %d, want 404", code)
	}
	if code, _ := agentPost(t, s, "/api/agent/select?name=echo"); code != http.StatusNotFound {
		t.Fatalf("removed select route = %d, want 404", code)
	}
	if code, _ := agentPost(t, s, "/api/agent/cancel"); code != http.StatusNotFound {
		t.Fatalf("removed cancel route = %d, want 404", code)
	}

	_, meta := get(t, s, "/api/meta")
	for _, key := range []string{"agent", "agentModel", "agentPinned", "agents"} {
		if _, ok := meta[key]; ok {
			t.Fatalf("meta unexpectedly exposes provider control-plane field %q: %v", key, meta[key])
		}
	}
}

func TestLegacyAgentRoutesRemoved(t *testing.T) {
	s := agentServer(t, t.TempDir(), writeHarness(t, "exit 0\n"))
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/agent/harnesses"},
		{http.MethodPost, "/api/agent/select?name=echo"},
		{http.MethodPost, "/api/agent/edit?path=main.go&l1=1&l2=1&instruction=hi"},
		{http.MethodPost, "/api/agent/cancel"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Host = "127.0.0.1:7777"
		req.Header.Set("Origin", "http://127.0.0.1:7777")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("removed %s %s = %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
}

func TestAgentJobEndpointPollsConfiguredProviderJob(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := agentServer(t, root, writeHarness(t, "sleep 1\n"))
	_, meta := get(t, s, "/api/meta")
	for _, key := range []string{"agent", "agentModel", "agentPinned", "agents"} {
		if _, ok := meta[key]; ok {
			t.Fatalf("configured provider leaked into meta field %q: %v", key, meta[key])
		}
	}
	job := startAgentJob(t, s, root, "a.go", 1, 1, "explain the change")

	if code, body := get(t, s, "/api/agent/job?id="+strconv.FormatInt(job.ID, 10)); code != http.StatusOK || body["id"] == nil {
		t.Fatalf("running job snapshot = %d %#v, want an identified job", code, body)
	}
	waitIdleID(t, s, job.ID)
	code, body := get(t, s, "/api/agent/job?id="+strconv.FormatInt(job.ID, 10))
	if code != http.StatusOK || body["running"] != false {
		t.Fatalf("completed job snapshot = %d %#v, want running=false", code, body)
	}
}

func TestAgentEditRunsHarnessAndReportsChange(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	out := t.TempDir()
	// keep.go is committed and clean, so no force is needed.
	s := agentServer(t, root, writeHarness(t,
		"printf 'touched\\n' >> keep.go\nprintf '%s' \"$1\" > "+filepath.Join(out, "prompt.txt")+"\n"))

	startAgentJob(t, s, root, "keep.go", 1, 1, "add a line")

	job := waitIdle(t, s)
	if job.Error != "" {
		t.Fatalf("harness failed: %s (log: %s)", job.Error, job.Log)
	}

	body, err := os.ReadFile(filepath.Join(root, "keep.go"))
	if err != nil || !strings.Contains(string(body), "touched") {
		t.Fatalf("harness did not edit the file: %q %v", body, err)
	}
	if len(job.Changed) != 1 || job.Changed[0] != "keep.go" {
		t.Fatalf("changed = %v, want [keep.go]", job.Changed)
	}
	if !job.Tracked {
		t.Fatal("tracked should be true inside a repository")
	}

	// The prompt must carry the anchor and the instruction the user wrote.
	prompt, err := os.ReadFile(filepath.Join(out, "prompt.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@keep.go line 1", "### Instruction", "add a line"} {
		if !strings.Contains(string(prompt), want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// Outside a repository px1 cannot name what a harness touched. The job must say
// so, because an empty change list would otherwise read as "nothing happened"
// and the client would skip the reload after a real edit.
func TestAgentOutsideGitReportsUnknownChanges(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := agentServer(t, root, writeHarness(t, "printf 'touched\\n' >> a.go\n"))

	// With no git there is also no uncommitted-work guard to satisfy.
	startAgentJob(t, s, root, "a.go", 1, 1, "hi")
	job := waitIdle(t, s)
	if job.Error != "" {
		t.Fatalf("run failed: %s (log: %s)", job.Error, job.Log)
	}
	if job.Tracked {
		t.Fatal("tracked should be false outside a repository")
	}
	if len(job.Changed) != 0 {
		t.Fatalf("changed = %v, want empty outside a repository", job.Changed)
	}
	body, _ := os.ReadFile(filepath.Join(root, "a.go"))
	if !strings.Contains(string(body), "touched") {
		t.Fatalf("harness did not edit the file: %q", body)
	}
}

func TestAgentRefusesSecondEditWhileRunning(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	s := agentServer(t, root, writeHarness(t, "sleep 2\n"))

	startAgentJob(t, s, root, "keep.go", 1, 1, "one")
	if _, err := s.agent.Start(filepath.Join(root, "keep.go"), "keep.go", 1, 1, "two"); !errors.Is(err, errAgentBusy) {
		t.Fatalf("second edit error = %v, want busy", err)
	}
	waitIdle(t, s)
}

func TestAgentAllowsEditOverUncommittedFileWithoutForce(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	// sub/mod.go is modified but not committed by gitRepo.
	s := agentServer(t, root, writeHarness(t, "printf 'touched\\n' >> keep.go\n"))

	startAgentJob(t, s, root, "sub/mod.go", 1, 1, "hi")
	if job := waitIdle(t, s); job.Error != "" {
		t.Fatalf("run failed: %s", job.Error)
	}
}

func TestPresetArgvOrder(t *testing.T) {
	for _, p := range agentPresets {
		n := len(p.Args)
		if n < 2 {
			t.Fatalf("preset %s args too short: %v", p.Name, p.Args)
		}
		if p.Args[n-1] != "{prompt}" {
			t.Fatalf("preset %s args %v: want {prompt} at the very end", p.Name, p.Args)
		}

		// When resolved with default model, {prompt} must remain at the very end
		_, resolved, _, err := resolveAgentSpec(p.Name, "")
		if err != nil {
			continue // tool may not be installed in test env
		}
		rn := len(resolved)
		if rn < 2 || resolved[rn-1] != "{prompt}" {
			t.Fatalf("resolved %s args %v: want {prompt} at the very end", p.Name, resolved)
		}
	}
}

func TestCodexPresetUsesCurrentUnattendedFlag(t *testing.T) {
	for _, p := range agentPresets {
		if p.Name != "codex" {
			continue
		}
		want := []string{"codex", "exec", "--approve-for-me", "{prompt}"}
		if !reflect.DeepEqual(p.Args, want) {
			t.Fatalf("codex args = %v, want %v", p.Args, want)
		}
		return
	}
	t.Fatal("codex preset not found")
}

// Editing in the diff view means editing a file that is already modified. Its
// git status reads M before and after, so the change has to be seen some other way.
func TestAgentReportsEditToAlreadyModifiedFile(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	s := agentServer(t, root, writeHarness(t, "printf 'touched\\n' >> sub/mod.go\n"))

	startAgentJob(t, s, root, "sub/mod.go", 1, 1, "hi")
	job := waitIdle(t, s)
	if job.Error != "" {
		t.Fatalf("harness failed: %s", job.Error)
	}
	if len(job.Changed) != 1 || job.Changed[0] != "sub/mod.go" {
		t.Fatalf("changed = %v, want [sub/mod.go]", job.Changed)
	}
}

// Two edits on disjoint line ranges of the same file run at the same time;
// only an overlapping range is refused.
func TestAgentAllowsNonOverlappingEditsInParallel(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	s := agentServer(t, root, writeHarness(t, "sleep 1\n"))

	first := startAgentJob(t, s, root, "keep.go", 1, 1, "one")
	startAgentJob(t, s, root, "keep.go", 2, 2, "two")
	if _, err := s.agent.Start(filepath.Join(root, "keep.go"), "keep.go", 1, 2, "three"); !errors.Is(err, errAgentBusy) {
		t.Fatalf("overlapping edit error = %v, want busy", err)
	}

	firstID := first.ID
	if job := waitIdleID(t, s, firstID); job.Error != "" {
		t.Fatalf("first edit failed: %s", job.Error)
	}
}

func TestReadLineRange(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		l1, l2 int
		want   string
	}{
		{2, 3, "two\nthree"},
		{1, 1, "one"},
		{3, 99, "three\nfour\n"}, // clamped to the end, trailing blank line included
		{0, 1, "one"},            // l1 below 1 is clamped
	} {
		got, err := readLineRange(p, tc.l1, tc.l2)
		if err != nil {
			t.Fatalf("%d-%d: %v", tc.l1, tc.l2, err)
		}
		if got != tc.want {
			t.Fatalf("%d-%d = %q, want %q", tc.l1, tc.l2, got, tc.want)
		}
	}
	if _, err := readLineRange(p, 50, 60); err == nil {
		t.Fatal("a range past the end should fail")
	}
}

func TestChangedSinceReportsBothDirections(t *testing.T) {
	before := map[string]string{"stays.go": "M", "reverted.go": "M"}
	after := map[string]string{"stays.go": "M", "new.go": "U"}

	got := map[string]bool{}
	for _, p := range changedSinceMaps(before, after) {
		got[p] = true
	}
	if got["stays.go"] {
		t.Fatal("an unchanged status should not be reported")
	}
	if !got["new.go"] {
		t.Fatal("a newly dirty file should be reported")
	}
	if !got["reverted.go"] {
		t.Fatal("a file restored to its committed state should be reported")
	}
}

func TestLineRefAndPrompt(t *testing.T) {
	if lineRef(4, 4) != "4" {
		t.Fatalf("single line ref = %q", lineRef(4, 4))
	}
	if lineRef(4, 9) != "4-9" {
		t.Fatalf("range ref = %q", lineRef(4, 9))
	}
	p := agentPrompt("web/src/app.js", 2, 5, "const x = 1;", "rename x to count")
	for _, want := range []string{"@web/src/app.js lines 2-5", "```js", "const x = 1;", "rename x to count"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
	pSingle := agentPrompt("a.go", 4, 4, "pkg a", "fix")
	if !strings.Contains(pSingle, "@a.go line 4") {
		t.Fatalf("single line prompt missing @a.go line 4:\n%s", pSingle)
	}
}

func TestShellQuoteAndCommand(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{
			in:   []string{"agy", "--mode", "accept-edits", "-p", "hello world"},
			want: "agy --mode accept-edits -p 'hello world'",
		},
		{
			in:   []string{"echo", "it's working"},
			want: "echo 'it'\\''s working'",
		},
		{
			in:   []string{"tool", ""},
			want: "tool ''",
		},
	}
	for _, tc := range cases {
		got := shellCommand(tc.in)
		if got != tc.want {
			t.Errorf("shellCommand(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAllPresetArgvFormatting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	for _, p := range agentPresets {
		binPath := filepath.Join(dir, p.Args[0])
		if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	for _, p := range agentPresets {
		name, resolved, model, err := resolveAgentSpec(p.Name, "")
		if err != nil {
			t.Fatalf("resolveAgentSpec(%q) error: %v", p.Name, err)
		}
		if name != p.Name {
			t.Errorf("name = %q, want %q", name, p.Name)
		}
		if model != p.DefaultModel {
			t.Errorf("model = %q, want %q", model, p.DefaultModel)
		}
		if resolved[len(resolved)-1] != "{prompt}" {
			t.Errorf("%s final arg = %q, want {prompt}", p.Name, resolved[len(resolved)-1])
		}
		// Ensure model flag was inserted properly
		hasModel := false
		for i, a := range resolved {
			if a == p.ModelFlag && i+1 < len(resolved) && resolved[i+1] == p.DefaultModel {
				hasModel = true
				break
			}
		}
		if !hasModel {
			t.Errorf("%s resolved args %v missing model flag %q %q", p.Name, resolved, p.ModelFlag, p.DefaultModel)
		}
	}
}
