package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

const (
	fakeKey    = "test-key-do-not-leak"
	fakeSecret = "hunter2-super-secret-value"
)

// Key-shaped fixtures are built at run time so no literal in the source matches
// the secret signatures that px1's own privacy check blocks reports for.
var fakeOpenAIKey = "sk-proj-" + strings.Repeat("a", 32)

func fakePrivateKey() string {
	return "-----BEGIN " + "RSA PRIVATE KEY-----\nMIIEow\n-----END " + "RSA PRIVATE KEY-----"
}

// fakeOpenAI answers the Responses API for each pipeline role and records
// every request, so tests can assert on what would have left the machine.
type fakeOpenAI struct {
	mu       sync.Mutex
	prompts  []string
	auths    map[string]bool
	requests int
	server   *httptest.Server
}

func newFakeOpenAI(t *testing.T) *fakeOpenAI {
	t.Helper()
	f := &fakeOpenAI{auths: map[string]bool{}}
	idPattern := regexp.MustCompile(`"ID":"([^"]+)"`)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var request struct {
			Input []struct{ Content string } `json:"input"`
		}
		_ = json.Unmarshal(body, &request)
		system, user := "", ""
		if len(request.Input) == 2 {
			system, user = request.Input[0].Content, request.Input[1].Content
		}
		f.mu.Lock()
		f.requests++
		f.prompts = append(f.prompts, string(body))
		f.auths[r.Header.Get("Authorization")] = true
		f.mu.Unlock()
		finding := `{"path":"src/refund.ts","start_line":3,"end_line":3,"severity":"P1","type":"logic","title":"Refund rounds down","body":"Integer division drops cents.","evidence":"amount / 100","confidence":0.8}`
		var reply string
		switch {
		case strings.Contains(system, "You are the planner"):
			id := ""
			if m := idPattern.FindStringSubmatch(user); m != nil {
				id = m[1]
			}
			reply = `{"tasks":[{"block_id":"` + id + `","focus":"rounding"}]}`
		case strings.Contains(system, "review sub-agent"):
			switch strings.Count(user, "<untrusted_tool_step>") {
			case 0:
				reply = `{"tool":"read_file","arguments":{"path":".env","start_line":1,"end_line":3}}`
			case 1:
				reply = `{"tool":"read_file","arguments":{"path":"src/refund.ts","start_line":1,"end_line":10}}`
			default:
				reply = `{"findings":[` + finding + `]}`
			}
		case strings.Contains(system, "You are the px1 judge"):
			reply = "[" + finding + "]"
		case strings.Contains(system, "Verify each proposed"):
			reply = "```json\n" + `[{"id":"f1","supported":true,"confidence":0.9,"reason":"the quoted line divides integers"}]` + "\n```"
		default:
			http.Error(w, `{"error":{"message":"unexpected prompt"}}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "resp_test",
			"output": []map[string]any{{"content": []map[string]string{{"text": reply}}}},
			"usage":  map[string]int{"input_tokens": 300, "output_tokens": 50},
		})
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeOpenAI) allPrompts() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.prompts, "\n")
}

func newAgenticRepo(t *testing.T, headSource string) (root, base, head string) {
	t.Helper()
	root = t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "px1 test")
	writeExportTestFile(t, root, "src/refund.ts", "export function refund(amount: number) {\n  return amount;\n}\n")
	writeExportTestFile(t, root, "package-lock.json", "{}\n")
	writeExportTestFile(t, root, ".env", "KEY=old\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "base")
	base = strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	writeExportTestFile(t, root, "src/refund.ts", headSource)
	writeExportTestFile(t, root, "package-lock.json", "{\"changed\": true}\n")
	writeExportTestFile(t, root, ".env", "KEY="+fakeSecret+"\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "Fix refunds")
	head = strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	return root, base, head
}

const cleanHead = "export function refund(amount: number) {\n  const dollars = amount / 100;\n  return dollars;\n}\n"

func agenticArgs(f *fakeOpenAI, root, base, out string, extra ...string) []string {
	return append([]string{"--root", root, "--base", base, "--out", out, "--model", "test-model", "--price-input", "1", "--price-output", "4", "--endpoint", f.server.URL + "/v1/responses"}, extra...)
}

func TestAgenticReviewRunsEndToEndWithoutLeakingExcludedFilesOrTheKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", fakeKey)
	f := newFakeOpenAI(t)
	root, base, _ := newAgenticRepo(t, cleanHead)
	out := filepath.Join(t.TempDir(), "run")
	var stdout bytes.Buffer
	if err := runAgenticReview(agenticArgs(f, root, base, out, "--yes"), &stdout); err != nil {
		t.Fatalf("run failed: %v\n%s", err, stdout.String())
	}
	summary := stdout.String()
	for _, want := range []string{"P1 src/refund.ts:3  Refund rounds down", "verified 1", "Report:", "WARNING: the report shows the whole change", "do not publish or share it", ".env - environment-secret path", "package-lock.json - generated or dependency file"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	if len(f.auths) != 1 || !f.auths["Bearer "+fakeKey] {
		t.Fatalf("every request must carry only the configured key: %v", f.auths)
	}
	prompts := f.allPrompts()
	if strings.Contains(prompts, fakeSecret) || strings.Contains(prompts, fakeKey) {
		t.Fatal("the excluded file's secret or the API key reached the model provider")
	}
	if !strings.Contains(prompts, "path is excluded from model input") {
		t.Fatal("the model was not told its read of .env was refused")
	}
	if strings.Contains(prompts, "package-lock.json") && strings.Contains(prompts, `"changed": true`) {
		t.Fatal("a generated lock file's diff was sent")
	}
	for _, name := range []string{"findings.json", "trace.json", filepath.Join("report", "index.html"), filepath.Join("report", "review.json"), filepath.Join("private", "feedback.md"), filepath.Join("private", "notes.json")} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatalf("missing output %s: %v", name, err)
		}
		if bytes.Contains(data, []byte(fakeKey)) {
			t.Fatalf("%s contains the API key", name)
		}
		// The report shows the whole commit, .env included, so only the files
		// that could be shared on their own must be free of the excluded secret.
		if (name == "findings.json" || name == "trace.json") && bytes.Contains(data, []byte(fakeSecret)) {
			t.Fatalf("%s contains an excluded secret", name)
		}
	}
	review, _ := os.ReadFile(filepath.Join(out, "report", "review.json"))
	if !bytes.Contains(review, []byte(`"trace"`)) || !bytes.Contains(review, []byte("Refund rounds down")) {
		t.Fatal("the report does not carry the finding and its trace")
	}
	info, err := os.Stat(filepath.Join(out, "private", "transcripts.jsonl"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("transcripts must be owner-only: %v %v", info, err)
	}
	feedback, _ := os.ReadFile(filepath.Join(out, "private", "feedback.md"))
	if !strings.Contains(string(feedback), "Refund rounds down") || !strings.Contains(string(feedback), "What it missed") {
		t.Fatalf("feedback sheet incomplete:\n%s", feedback)
	}
}

func TestDryRunAndMissingConsentSendNothing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", fakeKey)
	f := newFakeOpenAI(t)
	root, base, _ := newAgenticRepo(t, cleanHead)
	var dry bytes.Buffer
	if err := runAgenticReview(agenticArgs(f, root, base, t.TempDir(), "--dry-run"), &dry); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"src/refund.ts", "Never shown to the model", ".env", "Dry run: nothing was sent", "cost cap $0.96"} {
		if !strings.Contains(dry.String(), want) {
			t.Fatalf("dry run missing %q:\n%s", want, dry.String())
		}
	}
	var refused bytes.Buffer
	err := runAgenticReview(agenticArgs(f, root, base, t.TempDir()), &refused)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("without --yes: %v", err)
	}
	if f.requests != 0 {
		t.Fatalf("%d requests were sent without consent", f.requests)
	}
	t.Setenv("OPENAI_API_KEY", "")
	if err := runAgenticReview(agenticArgs(f, root, base, t.TempDir(), "--yes"), io.Discard); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("missing key: %v", err)
	}
	if err := runAgenticReview(agenticArgs(f, root, base, t.TempDir(), "--dry-run"), io.Discard); err != nil {
		t.Fatalf("a dry run needs no key: %v", err)
	}
}

func TestSecretsInTheDiffBlockTheRunBeforeAnythingIsSent(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", fakeKey)
	f := newFakeOpenAI(t)
	leaky := cleanHead + "const token = \"" + fakeOpenAIKey + "\";\n"
	root, base, _ := newAgenticRepo(t, leaky)
	err := runAgenticReview(agenticArgs(f, root, base, t.TempDir(), "--yes"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "likely secrets") || !strings.Contains(err.Error(), "src/refund.ts") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), strings.Repeat("a", 32)) || f.requests != 0 {
		t.Fatalf("the error leaked the secret or requests were sent: %d", f.requests)
	}
	if err := runAgenticReview(agenticArgs(f, root, base, t.TempDir(), "--yes", "--exclude-glob", "src/refund.ts"), io.Discard); err == nil || !strings.Contains(err.Error(), "no reviewable text changes") {
		t.Fatalf("excluding the leaking file leaves nothing to review: %v", err)
	}
}

func TestCostCapStopsTheReviewBeforeTheFirstCall(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", fakeKey)
	f := newFakeOpenAI(t)
	root, base, _ := newAgenticRepo(t, cleanHead)
	out := filepath.Join(t.TempDir(), "run")
	err := runAgenticReview(agenticArgs(f, root, base, out, "--yes", "--max-cost", "0.000001"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "review stopped early") {
		t.Fatalf("err = %v", err)
	}
	if f.requests != 0 {
		t.Fatalf("a call that could cross the cap was sent (%d)", f.requests)
	}
	if _, statErr := os.Stat(filepath.Join(out, "trace.json")); statErr != nil {
		t.Fatalf("partial trace not saved: %v", statErr)
	}
}

func TestAgenticFlagsAreValidated(t *testing.T) {
	good := []string{"--base", "main", "--model", "m", "--price-input", "1", "--price-output", "2"}
	for name, args := range map[string][]string{
		"no base":        {"--model", "m", "--price-input", "1", "--price-output", "2"},
		"no prices":      {"--base", "main", "--model", "m"},
		"bad tier":       append(append([]string{}, good...), "--tier", "base"),
		"http endpoint":  append(append([]string{}, good...), "--endpoint", "http://api.example.com/v1/responses"),
		"cred endpoint":  append(append([]string{}, good...), "--endpoint", "https://u:p@api.example.com/"),
		"zero cap":       append(append([]string{}, good...), "--max-cost", "0"),
		"tiny output":    append(append([]string{}, good...), "--max-output-tokens", "10"),
		"too many procs": append(append([]string{}, good...), "--concurrency", "99"),
	} {
		if _, err := parseAgenticFlags(args); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	opts, err := parseAgenticFlags(append(append([]string{}, good...), "--endpoint", "http://127.0.0.1:9/v1/responses"))
	if err != nil || opts.priceCached != 1 {
		t.Fatalf("loopback http and the cached-price default: %#v %v", opts, err)
	}
}

func TestNumberedDiffMatchesWhatTheModelsAreToldToRead(t *testing.T) {
	file := staticReviewFile{Path: "a.ts", Hunks: []staticReviewHunk{{Header: "@@ -1,4 +1,5 @@", Lines: []staticReviewDiffLine{
		{OldLine: 1, NewLine: 1, Kind: "context", Text: "keep"},
		{OldLine: 2, Kind: "removed", Text: "old"},
		{NewLine: 2, Kind: "added", Text: "new one"},
		{NewLine: 3, Kind: "added", Text: "new two"},
		{OldLine: 3, NewLine: 4, Kind: "context", Text: "keep too"},
		{NewLine: 5, Kind: "added", Text: "later"},
	}}}}
	text, ranges := numberedDiff(file)
	want := "@@ -1,4 +1,5 @@\n1\t keep\n-\told\n2\t+new one\n3\t+new two\n4\t keep too\n5\t+later"
	if text != want {
		t.Fatalf("diff = %q\nwant   %q", text, want)
	}
	if len(ranges) != 2 || ranges[0].StartLine != 2 || ranges[0].EndLine != 3 || ranges[1].StartLine != 5 || ranges[1].EndLine != 5 {
		t.Fatalf("ranges = %#v (adjacent added lines merge; a context line splits them)", ranges)
	}
}

func TestPrivacyPolicyAndRedaction(t *testing.T) {
	policy, err := newPrivacyPolicy([]string{"internal/**"})
	if err != nil {
		t.Fatal(err)
	}
	for path, wantReason := range map[string]string{
		".env": "environment-secret path", "deploy/.env.production": "environment-secret path", "keys/server.pem": "private-key or certificate path",
		"web/package-lock.json": "generated or dependency file", "internal/pay/x.go": "--exclude-glob internal/**",
	} {
		if ok, reason := policy.Allow(path); ok || !strings.Contains(reason, wantReason) {
			t.Fatalf("%s: allowed=%v reason=%q, want %q", path, ok, reason, wantReason)
		}
	}
	for _, path := range []string{"src/app.ts", ".env.example", "docs/internal.md"} {
		if ok, reason := policy.Allow(path); !ok {
			t.Fatalf("%s should be allowed, got %q", path, reason)
		}
	}
	redactor := &secretRedactor{}
	text := redactor.Redact("a " + fakeOpenAIKey + " b\n" + fakePrivateKey() + "\nnormal text")
	if strings.Contains(text, "sk-proj") || strings.Contains(text, "MIIEow") || !strings.Contains(text, "normal text") || redactor.count.Load() != 2 {
		t.Fatalf("redacted = %q (count %d)", text, redactor.count.Load())
	}
}
