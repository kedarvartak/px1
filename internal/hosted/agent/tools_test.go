package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"px1/internal/hosted/graph"
	"px1/internal/hosted/integrations/issues"
)

var scope = Scope{InstallationID: 7, RepositoryID: 9, HeadSHA: strings.Repeat("a", 40), LinkedIssues: []string{"ENG-12"}}

type fakeFiles struct{ gotPath string }

func (f *fakeFiles) ReadLines(_ context.Context, s Scope, path string, start, end int) (string, error) {
	f.gotPath = path
	return strings.Repeat("é", 20_000), nil
}

type fakeSearch struct{}

func (fakeSearch) Search(_ context.Context, _ Scope, query string, limit int) ([]SearchHit, error) {
	hits := make([]SearchHit, 50)
	for i := range hits {
		hits[i] = SearchHit{Path: "a.go", StartLine: i + 1, EndLine: i + 1, Snippet: query, Source: "ripgrep"}
	}
	return hits, nil
}

type fakeSymbols struct{}

func (fakeSymbols) ResolveSymbol(_ context.Context, _ Scope, path, name string) (int64, bool, error) {
	return 42, name == "Charge", nil
}

type fakeGraph struct{ gotTenant, gotRepo int64 }

func (g *fakeGraph) Neighbors(_ context.Context, tenant, repo int64, ids []int64, direction graph.Direction, _ []string) ([]graph.ToolEdge, error) {
	g.gotTenant, g.gotRepo = tenant, repo
	caller := graph.ToolSymbol{ID: 5, Path: "api/handler.go", Kind: "function", Name: "Handle"}
	return []graph.ToolEdge{{Source: caller, Target: graph.ToolSymbol{ID: ids[0]}, Kind: "calls"}}, nil
}

type fakeIssues struct{ fetched []string }

func (f *fakeIssues) FetchIssue(_ context.Context, _ int64, key string) (issues.Issue, error) {
	f.fetched = append(f.fetched, key)
	return issues.Issue{Key: key, Title: "Refunds", URL: "https://linear.app/x/issue/" + key}, nil
}

func call(name, arguments string) Call {
	return Call{Name: name, Arguments: json.RawMessage(arguments)}
}

func TestSpecsOfferOnlyConfiguredTools(t *testing.T) {
	if specs := (Toolbox{}).Specs(); len(specs) != 0 {
		t.Fatalf("empty toolbox offered %d tools", len(specs))
	}
	specs := Toolbox{Files: &fakeFiles{}, Search: fakeSearch{}}.Specs()
	if len(specs) != 2 || specs[0].Name != ReadFile || specs[1].Name != SearchCode {
		t.Fatalf("unexpected specs: %#v", specs)
	}
	for _, spec := range specs {
		var schema map[string]any
		if err := json.Unmarshal(spec.InputSchema, &schema); err != nil || schema["additionalProperties"] != false {
			t.Fatalf("%s schema must be strict JSON: %v", spec.Name, err)
		}
	}
}

func TestDispatchRequiresAPinnedTenantScope(t *testing.T) {
	box := Toolbox{Files: &fakeFiles{}}
	for _, bad := range []Scope{{RepositoryID: 9, HeadSHA: scope.HeadSHA}, {InstallationID: 7, RepositoryID: 9, HeadSHA: "main"}} {
		if _, err := box.Dispatch(context.Background(), bad, call(ReadFile, `{"path":"a.go","start_line":1,"end_line":2}`)); err == nil {
			t.Fatalf("scope %#v accepted", bad)
		}
	}
}

func TestReadFileRejectsEscapesUnknownFieldsAndHugeRanges(t *testing.T) {
	box := Toolbox{Files: &fakeFiles{}}
	for _, arguments := range []string{
		`{"path":"../secrets","start_line":1,"end_line":2}`,
		`{"path":"/etc/passwd","start_line":1,"end_line":2}`,
		`{"path":"a.go","start_line":1,"end_line":2,"shell":"rm"}`,
		`{"path":"a.go","start_line":5,"end_line":4}`,
		`{"path":"a.go","start_line":1,"end_line":401}`,
		`{"path":"a.go","start_line":1,"end_line":2} {}`,
	} {
		if _, err := box.Dispatch(context.Background(), scope, call(ReadFile, arguments)); err == nil {
			t.Fatalf("%s accepted", arguments)
		}
	}
	if _, err := box.Dispatch(context.Background(), scope, call("run_shell", `{}`)); err == nil {
		t.Fatal("unknown tool accepted")
	}
}

func TestOutputIsTruncatedOnARuneBoundary(t *testing.T) {
	files := &fakeFiles{}
	result, err := Toolbox{Files: files}.Dispatch(context.Background(), scope, call(ReadFile, `{"path":"./pkg//a.go","start_line":1,"end_line":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if files.gotPath != "pkg/a.go" || !result.Truncated || len(result.Content) > MaxToolOutput || !utf8.ValidString(result.Content) || !strings.HasSuffix(result.Content, truncationNotice) {
		t.Fatalf("path=%q truncated=%v len=%d", files.gotPath, result.Truncated, len(result.Content))
	}
}

func TestSearchCapsResults(t *testing.T) {
	result, err := Toolbox{Search: fakeSearch{}}.Dispatch(context.Background(), scope, call(SearchCode, `{"query":"refund","limit":500}`))
	if err != nil {
		t.Fatal(err)
	}
	var hits []SearchHit
	if err := json.Unmarshal([]byte(result.Content), &hits); err != nil || len(hits) != MaxSearchResults {
		t.Fatalf("hits=%d err=%v", len(hits), err)
	}
}

func TestGraphToolsResolveSymbolsWithinTheTenant(t *testing.T) {
	reader := &fakeGraph{}
	box := Toolbox{Symbols: fakeSymbols{}, Graph: graph.Tools{Reader: reader}}
	result, err := box.Dispatch(context.Background(), scope, call(GraphCallers, `{"path":"billing/charge.go","symbol":"Charge","depth":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if reader.gotTenant != 7 || reader.gotRepo != 9 || !strings.Contains(result.Content, "Handle") {
		t.Fatalf("tenant=%d repo=%d content=%s", reader.gotTenant, reader.gotRepo, result.Content)
	}
	missing, err := box.Dispatch(context.Background(), scope, call(GraphCallees, `{"path":"a.go","symbol":"Nope"}`))
	if err != nil || !strings.Contains(missing.Content, "No indexed symbol") {
		t.Fatalf("missing symbol: %#v %v", missing, err)
	}
	if _, err := box.Dispatch(context.Background(), scope, call(GraphCallers, `{"path":"a.go","symbol":"Charge","depth":3}`)); err == nil {
		t.Fatal("three-hop traversal accepted")
	}
}

func TestReadIssueOnlyFetchesLinkedIssues(t *testing.T) {
	tracker := &fakeIssues{}
	box := Toolbox{Issues: tracker}
	if _, err := box.Dispatch(context.Background(), scope, call(ReadIssue, `{"key":"SEC-1"}`)); err == nil {
		t.Fatal("unlinked issue fetched")
	}
	result, err := box.Dispatch(context.Background(), scope, call(ReadIssue, `{"key":"ENG-12"}`))
	if err != nil || !strings.Contains(result.Content, "Refunds") || len(tracker.fetched) != 1 {
		t.Fatalf("result=%#v err=%v fetched=%v", result, err, tracker.fetched)
	}
}

func TestUnconfiguredToolsFailClosed(t *testing.T) {
	for _, name := range []string{ReadFile, SearchCode, GraphCallers, GitBlame, GitLog, ReadIssue} {
		if _, err := (Toolbox{}).Dispatch(context.Background(), scope, call(name, `{}`)); err == nil {
			t.Fatalf("%s ran without a backing store", name)
		}
	}
}

type fakeHistory struct{ gotLimit int }

func (h *fakeHistory) Blame(_ context.Context, _ Scope, path string, start, end int) ([]BlameLine, error) {
	return []BlameLine{{Line: start, Commit: strings.Repeat("b", 40), Author: "dev", Summary: "add " + path}}, nil
}

func (h *fakeHistory) Log(_ context.Context, _ Scope, _ string, limit int) ([]Commit, error) {
	h.gotLimit = limit
	return make([]Commit, 100), nil
}

func TestHistoryToolsAreBounded(t *testing.T) {
	history := &fakeHistory{}
	box := Toolbox{History: history}
	blame, err := box.Dispatch(context.Background(), scope, call(GitBlame, `{"path":"a.go","start_line":3,"end_line":3}`))
	if err != nil || !strings.Contains(blame.Content, "add a.go") {
		t.Fatalf("blame=%#v err=%v", blame, err)
	}
	log, err := box.Dispatch(context.Background(), scope, call(GitLog, `{"path":"a.go","limit":1000}`))
	if err != nil {
		t.Fatal(err)
	}
	var commits []Commit
	if err := json.Unmarshal([]byte(log.Content), &commits); err != nil || history.gotLimit != MaxHistoryCommits || len(commits) != MaxHistoryCommits {
		t.Fatalf("limit=%d commits=%d err=%v", history.gotLimit, len(commits), err)
	}
}
