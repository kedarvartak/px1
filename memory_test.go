package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const cacheV1 = "package cache\n\nfunc getUser(id string) User {\n\treturn lru.GetOrLoad(id, 60*time.Second, load)\n}\n"

func TestLocateSnippetIgnoresIndentAndPrefersNearest(t *testing.T) {
	content := []byte("a\n\n  return lru.GetOrLoad(id)\nb\nreturn lru.GetOrLoad(id)\n")
	if from, to, ok := locateSnippet(content, "return lru.GetOrLoad(id)", 5); !ok || from != 5 || to != 5 {
		t.Fatalf("nearest = %d-%d %v", from, to, ok)
	}
	if from, to, ok := locateSnippet(content, "\treturn   lru.GetOrLoad(id)\n\nb", 1); !ok || from != 3 || to != 4 {
		t.Fatalf("multi-line = %d-%d %v", from, to, ok)
	}
	if _, _, ok := locateSnippet(content, "}", 1); ok {
		t.Fatal("a trivial snippet was anchored")
	}
}

func TestDecisionMemoryPersistsForgetsAndSupersedes(t *testing.T) {
	isolateSettings(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cache.go"), []byte(cacheV1), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newDecisionMemory(root)
	rec, err := m.Record(decisionRecord{Path: "cache.go", LineStart: 4, LineEnd: 4, Decision: "In-memory LRU, not Redis", Status: "accepted", Snippet: "\treturn lru.GetOrLoad(id, 60*time.Second, load)", PinID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Record(decisionRecord{Path: "cache.go", Decision: "LRU v2", Status: "accepted", Snippet: rec.Snippet, PinID: "p1"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cache.go"), []byte("// moved\n\n"+cacheV1), 0o644); err != nil {
		t.Fatal(err)
	}
	views := newDecisionMemory(root).ForFile("cache.go")
	if len(views) != 1 || views[0].Decision != "LRU v2" || !views[0].Located || views[0].CurrentFrom != 6 {
		t.Fatalf("views = %#v", views)
	}
	if _, err := m.Supersede(views[0].ID, "moved to Redis for multi-region"); err != nil {
		t.Fatal(err)
	}
	if got := newDecisionMemory(root).ForFile("cache.go"); len(got) != 0 {
		t.Fatalf("superseded decision still active: %#v", got)
	}
	if _, err := m.Record(decisionRecord{Path: "cache.go", Decision: "x", Status: "accepted", Snippet: rec.Snippet, PinID: "p2"}); err != nil {
		t.Fatal(err)
	}
	if err := m.ForgetPin("p2"); err != nil {
		t.Fatal(err)
	}
	if got := m.ForFile("cache.go"); len(got) != 0 {
		t.Fatalf("forgotten pin still active: %#v", got)
	}
}

func TestAcceptedPinBecomesChallengeInLaterSession(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cache.go")
	if err := os.WriteFile(path, []byte("package cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	isolateSettings(t)
	ix := NewIndex(root)
	ix.Build()
	s := NewServer(ix, nil)

	if code, body := reviewPost(t, s, "/api/review/session/start"); code != 200 {
		t.Fatalf("start = %d %v", code, body)
	}
	if err := os.WriteFile(path, []byte(cacheV1), 0o644); err != nil {
		t.Fatal(err)
	}
	pins, err := s.review.AddPins([]pinInput{{Path: "cache.go", LineStart: 4, LineEnd: 4, Decision: "In-memory LRU, not Redis", Why: "serverless", Alternatives: []string{"Redis"}}}, "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	code, body := pinPost(t, s, "/api/review/pin/status", map[string]string{"id": pins[0].ID, "status": "accepted"})
	if code != 200 || body["remembered"] != true {
		t.Fatalf("accept = %d %v", code, body)
	}
	if got, _ := s.review.Challenges(s.memory); len(got) != 0 {
		t.Fatalf("same-session decision challenged: %#v", got)
	}
	if code, _ := reviewPost(t, s, "/api/review/session/close"); code != 200 {
		t.Fatalf("close = %d", code)
	}

	if code, body := reviewPost(t, s, "/api/review/session/start"); code != 200 {
		t.Fatalf("restart = %d %v", code, body)
	}
	if err := os.WriteFile(path, []byte("package cache\n\nfunc getUser(id string) User {\n\treturn redis.Get(id)\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	challenges, err := s.review.Challenges(s.memory)
	if err != nil || len(challenges) != 1 || challenges[0].Decision != "In-memory LRU, not Redis" || challenges[0].BaselineFrom != 4 {
		t.Fatalf("challenges = %#v err=%v", challenges, err)
	}

	if code, _ := pinPost(t, s, "/api/decisions/resolve", map[string]string{"id": challenges[0].ID, "action": "bogus"}); code != http.StatusBadRequest {
		t.Fatalf("bogus action = %d", code)
	}
	if code, body := pinPost(t, s, "/api/decisions/resolve", map[string]string{"id": challenges[0].ID, "action": "dismiss"}); code != 200 {
		t.Fatalf("dismiss = %d %v", code, body)
	}
	if got, _ := s.review.Challenges(s.memory); len(got) != 0 {
		t.Fatalf("dismissed challenge still listed: %#v", got)
	}
	if len(s.memory.active("cache.go")) != 1 {
		t.Fatal("dismiss retired the decision")
	}
	if code, body := pinPost(t, s, "/api/decisions/resolve", map[string]string{"id": challenges[0].ID, "action": "supersede", "note": "multi-region"}); code != 200 {
		t.Fatalf("supersede = %d %v", code, body)
	}
	if len(s.memory.active("cache.go")) != 0 {
		t.Fatal("supersede kept the decision active")
	}
}

func TestSwitchedPinIsRememberedAsTheChoice(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth.go")
	if err := os.WriteFile(path, []byte("package auth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "switch.sh")
	body := "#!/bin/sh\nprintf 'package auth\\nfunc Login() { sessions.Store(user) }\\n' > auth.go\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	s := agentServer(t, root, script)
	if code, body := reviewPost(t, s, "/api/review/session/start"); code != 200 {
		t.Fatalf("start = %d %v", code, body)
	}
	if err := os.WriteFile(path, []byte("package auth\nfunc Login() { setJWTCookie(user) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pins, err := s.review.AddPins([]pinInput{{Path: "auth.go", LineStart: 2, LineEnd: 2, Decision: "JWT cookie", Alternatives: []string{"server sessions", "localStorage"}}}, "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	if code, body := pinPost(t, s, "/api/review/pin/status", map[string]string{"id": pins[0].ID, "status": "switched", "choice": "server sessions"}); code != 200 {
		t.Fatalf("switch = %d %v", code, body)
	}
	waitIdle(t, s)
	views := s.memory.ForFile("auth.go")
	if len(views) != 1 || views[0].Decision != "server sessions" || views[0].Status != "switched" || !views[0].Located {
		t.Fatalf("remembered = %#v", views)
	}
	if alts := views[0].Alternatives; len(alts) != 2 || alts[0] != "JWT cookie" || alts[1] != "localStorage" {
		t.Fatalf("alternatives = %#v", alts)
	}
	if code, body := pinPost(t, s, "/api/review/pin/status", map[string]string{"id": pins[0].ID, "status": "proposed"}); code != 200 {
		t.Fatalf("reopen = %d %v", code, body)
	}
	if got := s.memory.ForFile("auth.go"); len(got) != 0 {
		t.Fatalf("reopened pin still remembered: %#v", got)
	}
}
