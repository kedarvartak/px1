package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pinPost(t *testing.T, s *Server, url string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Host = "127.0.0.1:7777"
	req.Header.Set("Origin", "http://127.0.0.1:7777")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func pinWorkspace(t *testing.T) (string, *reviewManager) {
	t.Helper()
	isolateSettings(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte("package auth\nfunc Login() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newReviewManager(root)
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte("package auth\nfunc Login() { setCookie() }\nfunc hash() { argon2() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, m
}

func TestPinsValidateSortPersistAndGoStale(t *testing.T) {
	root, m := pinWorkspace(t)
	if _, err := m.AddPins([]pinInput{{Path: "auth.go", LineStart: 9, LineEnd: 9, Decision: "x"}}, "agent", false); err == nil {
		t.Fatal("pin outside the file was accepted")
	}
	if _, err := m.AddPins([]pinInput{{Path: "auth.go", LineStart: 2, LineEnd: 2, Decision: "   "}}, "agent", false); err == nil {
		t.Fatal("empty decision was accepted")
	}
	pins, err := m.AddPins([]pinInput{
		{Path: "auth.go", LineStart: 3, LineEnd: 3, Decision: "argon2 over bcrypt", Impact: 1, Alternatives: []string{"bcrypt", "", "scrypt", "pbkdf2", "md5", "sha1"}},
		{Path: "auth.go", LineStart: 2, LineEnd: 2, Decision: "JWT in   httpOnly cookie", Why: "stateless", Impact: 3},
	}, "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 2 || pins[0].Impact != 3 || pins[0].Decision != "JWT in httpOnly cookie" || pins[0].Status != "proposed" {
		t.Fatalf("pins = %#v", pins)
	}
	if got := pins[1].Alternatives; len(got) != 4 || got[0] != "bcrypt" || got[1] != "scrypt" {
		t.Fatalf("alternatives = %#v", got)
	}
	if _, err := m.SetPinStatus(pins[0].ID, "accepted", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetPinStatus(pins[0].ID, "bogus", ""); err == nil {
		t.Fatal("invalid status was accepted")
	}
	reloaded, err := newReviewManager(root).Pins()
	if err != nil || len(reloaded) != 2 || reloaded[0].Status != "accepted" || reloaded[0].Stale {
		t.Fatalf("reloaded = %#v err=%v", reloaded, err)
	}

	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte("package auth\nfunc Login() { setSession() }\nfunc hash() { argon2() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, _ := m.Pins()
	if !after[0].Stale || after[1].Stale {
		t.Fatalf("staleness = %v %v", after[0].Stale, after[1].Stale)
	}

	replaced, err := m.AddPins([]pinInput{{Path: "auth.go", LineStart: 3, LineEnd: 3, Decision: "fresh"}}, "agent", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(replaced) != 2 || replaced[0].Status != "accepted" || replaced[1].Decision != "fresh" {
		t.Fatalf("replace kept = %#v", replaced)
	}
}

func TestParsePinsOutputToleratesProse(t *testing.T) {
	out := "Here you go [not json]:\n```json\n[{\"path\":\"a.go\",\"lineStart\":1,\"lineEnd\":2,\"decision\":\"d\"}]\n```\n"
	pins, err := parsePinsOutput(out)
	if err != nil || len(pins) != 1 || pins[0].Path != "a.go" || pins[0].LineEnd != 2 {
		t.Fatalf("pins = %#v err=%v", pins, err)
	}
	if _, err := parsePinsOutput("no decisions here"); err == nil {
		t.Fatal("prose without JSON was accepted")
	}
}

func TestExplainPromptCoversDiffsAndNewFiles(t *testing.T) {
	root, m := pinWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "limit.go"), []byte("package auth\nconst attempts = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, err := m.ExplainPrompt()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Do not edit any files", "### Diff auth.go", "+func hash() { argon2() }", "### New file limit.go", "2: const attempts = 5"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestExplainEndpointPinsHarnessDecisions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte("package auth\nfunc Login() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "explain.sh")
	reply := `Sure. [{"path":"auth.go","lineStart":2,"lineEnd":2,"decision":"Cookie session","alternatives":["localStorage"],"impact":3},{"path":"../escape.go","lineStart":1,"lineEnd":1,"decision":"bad"}]`
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat <<'EOF'\n"+reply+"\nEOF\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := agentServer(t, root, script)
	if code, body := reviewPost(t, s, "/api/review/session/start"); code != 200 {
		t.Fatalf("start = %d %v", code, body)
	}
	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte("package auth\nfunc Login() { cookie() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := pinPost(t, s, "/api/review/pins/explain", nil); code != 200 {
		t.Fatalf("explain = %d %v", code, body)
	}
	waitIdle(t, s)
	pins, err := s.review.Pins()
	if err != nil || len(pins) != 1 || pins[0].Decision != "Cookie session" || pins[0].Source != "explain" {
		t.Fatalf("pins = %#v err=%v", pins, err)
	}
	if st := s.explainState(); st["error"] != "" {
		t.Fatalf("explain error = %v", st["error"])
	}

	code, body := pinPost(t, s, "/api/review/pin/status", map[string]string{"id": pins[0].ID, "status": "switched"})
	if code != http.StatusBadRequest {
		t.Fatalf("switch without choice = %d %v", code, body)
	}
	code, body = pinPost(t, s, "/api/review/pin/status", map[string]string{"id": pins[0].ID, "status": "switched", "choice": "localStorage"})
	if code != 200 || body["job"] == nil {
		t.Fatalf("switch = %d %v", code, body)
	}
	waitIdle(t, s)
	if got, _ := s.review.Pin(pins[0].ID); got.Status != "switched" || got.Choice != "localStorage" {
		t.Fatalf("switched pin = %#v", got)
	}
}

func TestPushPinsEndpointRejectsEscapingPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte("package auth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	isolateSettings(t)
	ix := NewIndex(root)
	ix.Build()
	s := NewServer(ix, nil)
	if code, _ := pinPost(t, s, "/api/review/pins", map[string]any{"pins": []pinInput{}}); code != http.StatusConflict {
		t.Fatalf("push without session = %d", code)
	}
	if code, body := reviewPost(t, s, "/api/review/session/start"); code != 200 {
		t.Fatalf("start = %d %v", code, body)
	}
	if code, _ := pinPost(t, s, "/api/review/pins", map[string]any{"pins": []pinInput{{Path: "../x.go", LineStart: 1, LineEnd: 1, Decision: "d"}}}); code != http.StatusBadRequest {
		t.Fatalf("escaping path = %d", code)
	}
	code, body := pinPost(t, s, "/api/review/pins", map[string]any{"pins": []pinInput{{Path: "auth.go", LineStart: 1, LineEnd: 1, Decision: "package name"}}})
	if code != 200 || len(body["pins"].([]any)) != 1 {
		t.Fatalf("push = %d %v", code, body)
	}
}
