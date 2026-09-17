package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func httpGet(t *testing.T, s *Server, url string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func TestGlobRegexp(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"*.ts", "src/api/users.ts", true},
		{"*.ts", "src/api/users.tsx", false},
		{"src/**/*.go", "src/a/b/c.go", true},
		{"src/**/*.go", "src/c.go", true},
		{"src/*.go", "src/a/c.go", false},
		{"api/?.js", "api/x.js", true},
	}
	for _, c := range cases {
		re, err := globRegexp(c.glob)
		if err != nil {
			t.Fatal(err)
		}
		if got := re.MatchString(c.path); got != c.want {
			t.Errorf("glob %q on %q = %v", c.glob, c.path, got)
		}
	}
	if re, _ := globRegexp(""); re != nil {
		t.Fatal("empty glob should match everything")
	}
}

func TestValidateRuleRejectsUselessPatterns(t *testing.T) {
	for _, in := range []ruleInput{
		{Pattern: "", Message: "m"},
		{Pattern: "(", Message: "m"},
		{Pattern: ".*", Message: "m"},
		{Pattern: `\bfetch\(`, Message: " "},
		{Pattern: `\bfetch\(`, Message: "m", Glob: strings.Repeat("a", ruleGlobMax+1)},
	} {
		if _, err := validateRule(in); err == nil {
			t.Errorf("accepted %#v", in)
		}
	}
}

func TestAddedLinesTracksNewFileNumbers(t *testing.T) {
	diff := "--- a/x\n+++ b/x\n@@ -1,3 +1,4 @@\n keep\n-old\n+new one\n+new two\n keep\n@@ -10 +11,2 @@\n ctx\n+tail\n"
	got := addedLines(diff)
	if len(got) != 3 || got[2] != "new one" || got[3] != "new two" || got[12] != "tail" {
		t.Fatalf("added = %#v", got)
	}
}

func TestParseRuleOutput(t *testing.T) {
	in, err := parseRuleOutput("Here: {\"note\": 1} then {\"pattern\":\"\\\\bfetch\\\\(\",\"glob\":\"*.ts\",\"message\":\"Use apiClient, not fetch\"}")
	if err != nil || in.Pattern != `\bfetch\(` || in.Glob != "*.ts" {
		t.Fatalf("rule = %#v err=%v", in, err)
	}
	if _, err := parseRuleOutput(`{"pattern":".*","message":"x"}`); err == nil {
		t.Fatal("match-everything suggestion was accepted")
	}
}

func TestRulesFlagAddedLinesAcrossSessions(t *testing.T) {
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
	write("api/users.ts", "const a = await fetch('/old')\n")
	isolateSettings(t)
	ix := NewIndex(root)
	ix.Build()
	s := NewServer(ix, nil)

	code, body := pinPost(t, s, "/api/rules", ruleInput{Pattern: `\bfetch\(`, Glob: "*.ts", Message: "Use apiClient, not fetch", Origin: "api/users.ts:1"})
	if code != 200 {
		t.Fatalf("add rule = %d %v", code, body)
	}
	ruleID := body["rule"].(map[string]any)["id"].(string)
	if code, _ := pinPost(t, s, "/api/rules", ruleInput{Pattern: "(", Message: "bad"}); code != http.StatusBadRequest {
		t.Fatalf("invalid rule = %d", code)
	}

	if code, body := reviewPost(t, s, "/api/review/session/start"); code != 200 {
		t.Fatalf("start = %d %v", code, body)
	}
	write("api/users.ts", "const a = await fetch('/old')\nconst b = await fetch('/new')\n")
	write("api/orders.ts", "export const load = () => fetch('/orders')\n")
	write("api/notes.md", "call fetch(url) directly\n")
	write(".px1/rules.json", `{"rules":[{"pattern":"console\\.log\\(","message":"No console.log in API code","glob":"api/**"}]}`)
	write("api/orders.ts", "export const load = () => fetch('/orders')\nconsole.log('x')\n")

	rules, err := s.rules.All()
	if err != nil || len(rules) != 2 || rules[0].Source != "team" {
		t.Fatalf("rules = %#v err=%v", rules, err)
	}
	hits, err := s.review.RuleHits(rules)
	if err != nil {
		t.Fatal(err)
	}
	type key struct {
		path string
		line int
		src  string
	}
	got := map[key]bool{}
	for _, h := range hits {
		got[key{h.Path, h.Line, h.Source}] = true
	}
	want := []key{{"api/users.ts", 2, "review"}, {"api/orders.ts", 1, "review"}, {"api/orders.ts", 2, "team"}}
	if len(hits) != len(want) {
		t.Fatalf("hits = %#v", hits)
	}
	for _, k := range want {
		if !got[k] {
			t.Fatalf("missing hit %v in %#v", k, hits)
		}
	}

	var usersHit ruleHit
	for _, h := range hits {
		if h.Path == "api/users.ts" {
			usersHit = h
		}
	}
	if code, _ := pinPost(t, s, "/api/review/rule-hits/dismiss", map[string]string{"key": usersHit.Key}); code != 200 {
		t.Fatalf("dismiss = %d", code)
	}
	write("api/users.ts", "const a = await fetch('/old')\n\n\nconst b = await fetch('/new')\n")
	after, _ := s.review.RuleHits(rules)
	for _, h := range after {
		if h.Path == "api/users.ts" {
			t.Fatalf("dismissed hit returned after the line moved: %#v", h)
		}
	}

	s.rules.noteHits(after)
	s.rules.noteHits(after)
	own, _ := newRuleMemory(root).All()
	for _, r := range own {
		if r.ID == ruleID && r.Hits != 1 {
			t.Fatalf("hits counted %d times", r.Hits)
		}
	}

	if code, _ := pinPost(t, s, "/api/rules/update", map[string]any{"id": ruleID, "enabled": false}); code != 200 {
		t.Fatalf("disable = %d", code)
	}
	rules, _ = s.rules.All()
	if hits, _ := s.review.RuleHits(rules); len(hits) != 1 || hits[0].Source != "team" {
		t.Fatalf("disabled rule still hit: %#v", hits)
	}
	if code, _ := pinPost(t, s, "/api/rules/update", map[string]any{"id": ruleID, "delete": true}); code != 200 {
		t.Fatalf("delete = %d", code)
	}
	if rules, _ := newRuleMemory(root).All(); len(rules) != 1 {
		t.Fatalf("rules after delete = %#v", rules)
	}
}

func TestRuleSuggestAndSendHitsUseHarness(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "users.ts"), []byte("const a = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "harness.sh")
	body := "#!/bin/sh\ncase \"$1\" in\n*\"reusable lint rule\"*) cat <<'EOF'\n" + `{"pattern":"\\bfetch\\(","glob":"*.ts","message":"Use apiClient"}` + "\nEOF\n;;\n*) printf 'const a = await apiClient.get(\"/x\")\\n' > users.ts ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	s := agentServer(t, root, script)

	code, resp := pinPost(t, s, "/api/rules/suggest", map[string]any{"path": "users.ts", "lineStart": 1, "lineEnd": 1, "comment": "Use apiClient instead of fetch"})
	if code != 200 {
		t.Fatalf("suggest = %d %v", code, resp)
	}
	job := waitIdle(t, s)
	req := httpGet(t, s, "/api/rules/suggest?job="+strconv.FormatInt(job.ID, 10))
	if req["pattern"] != `\bfetch\(` || req["glob"] != "*.ts" {
		t.Fatalf("suggestion = %v", req)
	}

	if _, err := s.rules.Add(ruleInput{Pattern: `\bfetch\(`, Glob: "*.ts", Message: "Use apiClient"}); err != nil {
		t.Fatal(err)
	}
	if code, body := reviewPost(t, s, "/api/review/session/start"); code != 200 {
		t.Fatalf("start = %d %v", code, body)
	}
	if err := os.WriteFile(filepath.Join(root, "users.ts"), []byte("const a = await fetch('/x')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := pinPost(t, s, "/api/review/rule-hits/send", nil); code != 200 {
		t.Fatalf("send = %d %v", code, body)
	}
	waitIdle(t, s)
	comments, _ := s.review.Comments()
	if len(comments) != 1 || comments[0].Text != "Rule: Use apiClient" {
		t.Fatalf("comments = %#v", comments)
	}
	rules, _ := s.rules.All()
	if hits, _ := s.review.RuleHits(rules); len(hits) != 0 {
		t.Fatalf("hits after agent fix = %#v", hits)
	}
}
