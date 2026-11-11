package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const docsUpdatePage = "---\ntitle: A page\nsection: guides\nkind: narrative\nstatus: shipped\ncovers:\n  - tool.go\nverified_at:\n---\n\n# A page\n\n## Limits\n\nAt most 10 files.\n\n## Other\n\nUnrelated.\n"

// docsUpdateRepo has a page covering tool.go and a commit that changes it.
func docsUpdateRepo(t *testing.T) (root, base string) {
	t.Helper()
	root = t.TempDir()
	runGitTest(t, root, "init", "-q")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "Test")
	writeExportTestFile(t, root, "tool.go", "package main\n\nconst maxFiles = 10\n")
	writeExportTestFile(t, root, "docs/PAGE.md", docsUpdatePage)
	runGitTest(t, root, "add", "-A")
	runGitTest(t, root, "commit", "-qm", "base")
	base = strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	writeExportTestFile(t, root, "tool.go", "package main\n\nconst maxFiles = 25\n")
	runGitTest(t, root, "commit", "-qam", "raise the limit")
	return root, base
}

// docsModel is a fake Responses endpoint. It answers a drafting request with
// draft and a checking request with verdict, and records what it was sent.
type docsModel struct {
	draft, verdict string
	prompts        []string
}

func (m *docsModel) serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var request struct {
			Input []struct{ Role, Content string } `json:"input"`
		}
		_ = json.Unmarshal(body, &request)
		reply := m.draft
		if strings.HasPrefix(request.Input[0].Content, "You check one proposed edit") {
			reply = m.verdict
		}
		m.prompts = append(m.prompts, request.Input[1].Content)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "resp_test",
			"output": []map[string]any{{"content": []map[string]string{{"text": reply}}}},
			"usage":  map[string]int{"input_tokens": 100, "output_tokens": 50},
		})
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func docsUpdateArgs(root, base, endpoint string, extra ...string) []string {
	return append([]string{"update", "--root", root, "--base", base, "--model", "test-model", "--price-input", "1", "--price-output", "4", "--endpoint", endpoint}, extra...)
}

const goodDraft = "Here you go:\n```json\n" + `{"edits":[{"page":"docs/OTHER.md","heading":"Limits","body":"At most 25 files.","reason":"The limit rose from 10 to 25.","citations":[{"file":"tool.go","quote":"const maxFiles = 25"}]}]}` + "\n```"

func TestDocsUpdateAppliesASupportedEdit(t *testing.T) {
	root, base := docsUpdateRepo(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	model := &docsModel{draft: goodDraft, verdict: `{"supported":true}`}
	report := filepath.Join(t.TempDir(), "update.json")
	var out bytes.Buffer
	if err := runDocs(docsUpdateArgs(root, base, model.serve(t), "--yes", "--out", report), &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	page, _ := os.ReadFile(filepath.Join(root, "docs/PAGE.md"))
	if string(page) != strings.Replace(docsUpdatePage, "10 files", "25 files", 1) {
		t.Fatalf("page after update:\n%s", page)
	}
	if len(model.prompts) != 2 || !strings.Contains(model.prompts[0], "+const maxFiles = 25") || !strings.Contains(model.prompts[1], `"old_text":"At most 10 files."`) {
		t.Fatalf("prompts: %q", model.prompts)
	}
	for _, want := range []string{"**docs/PAGE.md**, section \"Limits\"", "`tool.go:3`", "2 model call(s)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	var decoded docsUpdateReport
	data, _ := os.ReadFile(report)
	if err := json.Unmarshal(data, &decoded); err != nil || len(decoded.Results) != 1 || !decoded.Results[0].Applied || decoded.ModelCalls != 2 || decoded.CostMicrousd <= 0 {
		t.Fatalf("report: %v %s", err, data)
	}
}

func TestDocsUpdateRefusesWhatItCannotStandBehind(t *testing.T) {
	cases := []struct{ name, draft, verdict, want string }{
		{"invented quote", strings.Replace(goodDraft, "maxFiles = 25", "maxFiles = 99", 1), `{"supported":true}`, "the quote is not in tool.go"},
		{"verifier says no", goodDraft, `{"supported":false,"problem":"The code does not say files."}`, "not supported by the code it cites: The code does not say files."},
		{"verifier reply is not a verdict", goodDraft, `I think it is fine.`, "not supported by the code it cites"},
		{"draft is not JSON", "The page looks fine to me.", `{"supported":true}`, "the draft was unusable"},
		{"no edit needed", `{"edits":[]}`, `{"supported":true}`, "No documentation edit was applied."},
	}
	for _, tc := range cases {
		root, base := docsUpdateRepo(t)
		t.Setenv("OPENAI_API_KEY", "test-key")
		model := &docsModel{draft: tc.draft, verdict: tc.verdict}
		var out bytes.Buffer
		if err := runDocs(docsUpdateArgs(root, base, model.serve(t), "--yes"), &out); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("%s: output lacks %q:\n%s", tc.name, tc.want, out.String())
		}
		if page, _ := os.ReadFile(filepath.Join(root, "docs/PAGE.md")); string(page) != docsUpdatePage {
			t.Errorf("%s: the page was changed", tc.name)
		}
	}
}

func TestDocsUpdateSendsNothingWithoutConsent(t *testing.T) {
	root, base := docsUpdateRepo(t)
	model := &docsModel{draft: goodDraft, verdict: `{"supported":true}`}
	endpoint := model.serve(t)
	var out bytes.Buffer

	t.Setenv("OPENAI_API_KEY", "")
	if err := runDocs([]string{"update", "--root", root, "--base", base, "--dry-run"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "docs/PAGE.md: the page and the diff of tool.go") || !strings.Contains(out.String(), "Dry run: nothing was sent") {
		t.Fatalf("dry run:\n%s", out.String())
	}
	if err := runDocs(docsUpdateArgs(root, base, endpoint, "--yes"), &out); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("no key: %v", err)
	}
	t.Setenv("OPENAI_API_KEY", "test-key")
	if err := runDocs(docsUpdateArgs(root, base, endpoint), &out); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("no --yes: %v", err)
	}
	if err := runDocs(docsUpdateArgs(root, base, "http://example.com/v1", "--yes"), &out); err == nil || !strings.Contains(err.Error(), "--endpoint") {
		t.Fatalf("plain http endpoint: %v", err)
	}
	if len(model.prompts) != 0 {
		t.Fatalf("the model was called %d time(s)", len(model.prompts))
	}
}

func TestDocsUpdateKeepsExcludedAndSecretContentFromTheModel(t *testing.T) {
	root, base := docsUpdateRepo(t)
	writeExportTestFile(t, root, "tool.go", "package main\n\nconst maxFiles = 25\n\nconst key = \"-----BEGIN RSA PRIVATE KEY-----\\nabc\\n-----END RSA PRIVATE KEY-----\"\n")
	runGitTest(t, root, "commit", "-qam", "add a key")
	t.Setenv("OPENAI_API_KEY", "test-key")
	model := &docsModel{draft: `{"edits":[]}`, verdict: `{"supported":true}`}
	endpoint := model.serve(t)
	var out bytes.Buffer
	if err := runDocs(docsUpdateArgs(root, base, endpoint, "--yes"), &out); err != nil {
		t.Fatal(err)
	}
	if len(model.prompts) != 1 || strings.Contains(model.prompts[0], "BEGIN RSA PRIVATE KEY") || !strings.Contains(model.prompts[0], "redacted") {
		t.Fatalf("prompt: %q", model.prompts)
	}
	out.Reset()
	if err := runDocs(docsUpdateArgs(root, base, endpoint, "--yes", "--exclude-glob", "tool.go"), &out); err != nil {
		t.Fatal(err)
	}
	if len(model.prompts) != 1 || !strings.Contains(out.String(), "tool.go not shown to the model") || !strings.Contains(out.String(), "Nothing to draft.") {
		t.Fatalf("excluded file: %d prompt(s)\n%s", len(model.prompts), out.String())
	}
}

func TestDocsUpdateStopsAtTheCostCap(t *testing.T) {
	root, base := docsUpdateRepo(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	model := &docsModel{draft: goodDraft, verdict: `{"supported":true}`}
	var out bytes.Buffer
	if err := runDocs(docsUpdateArgs(root, base, model.serve(t), "--yes", "--max-cost", "0.000001"), &out); err != nil {
		t.Fatal(err)
	}
	if len(model.prompts) != 0 || !strings.Contains(out.String(), "not drafted (a budget limit was reached") {
		t.Fatalf("%d prompt(s)\n%s", len(model.prompts), out.String())
	}
}
