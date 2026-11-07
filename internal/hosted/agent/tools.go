// Package agent contains the agentic review engine: the tools sub-agents may
// call, and the bounds that keep those calls tenant-scoped, revision-pinned,
// and read-only. Tool output is repository data, never instructions.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"px1/internal/hosted/graph"
	"px1/internal/hosted/integrations/issues"
)

const (
	MaxArgumentBytes  = 4 << 10
	MaxToolOutput     = 16 << 10
	MaxReadFileLines  = 400
	MaxSearchResults  = 20
	MaxHistoryCommits = 20
	truncationNotice  = "\n[output truncated by px1]"
)

const (
	ReadFile     = "read_file"
	SearchCode   = "search_code"
	GraphCallers = "graph_callers"
	GraphCallees = "graph_callees"
	GitBlame     = "git_blame"
	GitLog       = "git_log"
	ReadIssue    = "read_issue"
)

// Scope pins every call to one tenant, repository, and full head SHA. Linked
// issues are the only issue keys read_issue may fetch, so injected text cannot
// steer a sub-agent into reading unrelated tenant tickets.
type Scope struct {
	InstallationID int64
	RepositoryID   int64
	HeadSHA        string
	LinkedIssues   []string
}

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type Call struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type Result struct {
	Name      string `json:"name"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

type SearchHit struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Snippet   string `json:"snippet"`
	Source    string `json:"source"`
}

type BlameLine struct {
	Line    int    `json:"line"`
	Commit  string `json:"commit"`
	Author  string `json:"author"`
	Summary string `json:"summary"`
}

type Commit struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Summary string `json:"summary"`
}

// Backing stores. Implementations read the index, graph, and object store for
// Scope.HeadSHA; none of them may check out or execute repository code.
type (
	FileReader interface {
		ReadLines(ctx context.Context, scope Scope, path string, start, end int) (string, error)
	}
	CodeSearcher interface {
		Search(ctx context.Context, scope Scope, query string, limit int) ([]SearchHit, error)
	}
	SymbolResolver interface {
		ResolveSymbol(ctx context.Context, scope Scope, path, name string) (int64, bool, error)
	}
	History interface {
		Blame(ctx context.Context, scope Scope, path string, start, end int) ([]BlameLine, error)
		Log(ctx context.Context, scope Scope, path string, limit int) ([]Commit, error)
	}
)

type Toolbox struct {
	Files    FileReader
	Search   CodeSearcher
	Symbols  SymbolResolver
	Graph    graph.Tools
	History  History
	Issues   issues.Tracker
	MaxBytes int
}

// Specs lists only the tools whose backing store is configured, so a model is
// never offered a tool that will fail on every call.
func (t Toolbox) Specs() []ToolSpec {
	var specs []ToolSpec
	add := func(enabled bool, name, description, schema string) {
		if enabled {
			specs = append(specs, ToolSpec{Name: name, Description: description, InputSchema: json.RawMessage(schema)})
		}
	}
	add(t.Files != nil, ReadFile, "Read a line range of one file at the reviewed head commit.",
		`{"type":"object","additionalProperties":false,"required":["path","start_line","end_line"],"properties":{"path":{"type":"string"},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}}}`)
	add(t.Search != nil, SearchCode, "Search the indexed repository by meaning and by exact text.",
		`{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","maxLength":200},"limit":{"type":"integer","minimum":1,"maximum":20}}}`)
	graphSchema := `{"type":"object","additionalProperties":false,"required":["path","symbol"],"properties":{"path":{"type":"string"},"symbol":{"type":"string"},"depth":{"type":"integer","minimum":1,"maximum":2}}}`
	add(t.Symbols != nil && t.Graph.Reader != nil, GraphCallers, "List symbols that call the named symbol, up to two hops.", graphSchema)
	add(t.Symbols != nil && t.Graph.Reader != nil, GraphCallees, "List symbols the named symbol calls, up to two hops.", graphSchema)
	add(t.History != nil, GitBlame, "Show the last commit to touch each line in a range.",
		`{"type":"object","additionalProperties":false,"required":["path","start_line","end_line"],"properties":{"path":{"type":"string"},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}}}`)
	add(t.History != nil, GitLog, "List recent commits that changed one file.",
		`{"type":"object","additionalProperties":false,"required":["path"],"properties":{"path":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":20}}}`)
	add(t.Issues != nil, ReadIssue, "Read an issue linked from this pull request.",
		`{"type":"object","additionalProperties":false,"required":["key"],"properties":{"key":{"type":"string"}}}`)
	return specs
}

type lineRange struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type symbolArgs struct {
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
	Depth  int    `json:"depth"`
}

// Dispatch validates one model-proposed call and runs it. Arguments are
// decoded strictly; a call outside the scope or bounds returns an error the
// caller can report back to the model instead of executing it.
func (t Toolbox) Dispatch(ctx context.Context, scope Scope, call Call) (Result, error) {
	if scope.InstallationID <= 0 || scope.RepositoryID <= 0 || !fullSHA(scope.HeadSHA) {
		return Result{}, errors.New("tool calls require a tenant, repository, and full head SHA")
	}
	if len(call.Arguments) > MaxArgumentBytes {
		return Result{}, fmt.Errorf("%s arguments exceed %d bytes", call.Name, MaxArgumentBytes)
	}
	var output any
	var err error
	switch call.Name {
	case ReadFile:
		output, err = t.readFile(ctx, scope, call.Arguments)
	case SearchCode:
		output, err = t.search(ctx, scope, call.Arguments)
	case GraphCallers, GraphCallees:
		output, err = t.graphNeighbors(ctx, scope, call)
	case GitBlame:
		output, err = t.blame(ctx, scope, call.Arguments)
	case GitLog:
		output, err = t.log(ctx, scope, call.Arguments)
	case ReadIssue:
		output, err = t.readIssue(ctx, scope, call.Arguments)
	default:
		return Result{}, fmt.Errorf("unknown tool %q", call.Name)
	}
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", call.Name, err)
	}
	content, ok := output.(string)
	if !ok {
		encoded, err := json.Marshal(output)
		if err != nil {
			return Result{}, err
		}
		content = string(encoded)
	}
	content, truncated := truncate(content, t.maxBytes())
	return Result{Name: call.Name, Content: content, Truncated: truncated}, nil
}

func (t Toolbox) readFile(ctx context.Context, scope Scope, raw json.RawMessage) (any, error) {
	if t.Files == nil {
		return nil, errUnavailable
	}
	var args lineRange
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if err := validRange(&args); err != nil {
		return nil, err
	}
	return t.Files.ReadLines(ctx, scope, args.Path, args.StartLine, args.EndLine)
}

func (t Toolbox) search(ctx context.Context, scope Scope, raw json.RawMessage) (any, error) {
	if t.Search == nil {
		return nil, errUnavailable
	}
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" || len(args.Query) > 200 {
		return nil, errors.New("query must be 1 to 200 bytes")
	}
	args.Limit = clamp(args.Limit, 10, MaxSearchResults)
	hits, err := t.Search.Search(ctx, scope, args.Query, args.Limit)
	if err != nil {
		return nil, err
	}
	if len(hits) > args.Limit {
		hits = hits[:args.Limit]
	}
	return hits, nil
}

func (t Toolbox) graphNeighbors(ctx context.Context, scope Scope, call Call) (any, error) {
	if t.Symbols == nil || t.Graph.Reader == nil {
		return nil, errUnavailable
	}
	var args symbolArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return nil, err
	}
	clean, err := repositoryPath(args.Path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Symbol) == "" || len(args.Symbol) > 200 {
		return nil, errors.New("symbol must be 1 to 200 bytes")
	}
	id, found, err := t.Symbols.ResolveSymbol(ctx, scope, clean, args.Symbol)
	if err != nil {
		return nil, err
	}
	if !found {
		return fmt.Sprintf("No indexed symbol %q in %s at this revision.", args.Symbol, clean), nil
	}
	query := graph.ToolQuery{InstallationID: scope.InstallationID, RepositoryID: scope.RepositoryID, SymbolID: id, Depth: args.Depth, Limit: 50}
	if call.Name == GraphCallers {
		return t.Graph.Callers(ctx, query)
	}
	return t.Graph.Callees(ctx, query)
}

func (t Toolbox) blame(ctx context.Context, scope Scope, raw json.RawMessage) (any, error) {
	if t.History == nil {
		return nil, errUnavailable
	}
	var args lineRange
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if err := validRange(&args); err != nil {
		return nil, err
	}
	return t.History.Blame(ctx, scope, args.Path, args.StartLine, args.EndLine)
}

func (t Toolbox) log(ctx context.Context, scope Scope, raw json.RawMessage) (any, error) {
	if t.History == nil {
		return nil, errUnavailable
	}
	var args struct {
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	clean, err := repositoryPath(args.Path)
	if err != nil {
		return nil, err
	}
	limit := clamp(args.Limit, 10, MaxHistoryCommits)
	commits, err := t.History.Log(ctx, scope, clean, limit)
	if err != nil {
		return nil, err
	}
	if len(commits) > limit {
		commits = commits[:limit]
	}
	return commits, nil
}

func (t Toolbox) readIssue(ctx context.Context, scope Scope, raw json.RawMessage) (any, error) {
	if t.Issues == nil {
		return nil, errUnavailable
	}
	var args struct {
		Key string `json:"key"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	linked := false
	for _, key := range scope.LinkedIssues {
		linked = linked || key == args.Key
	}
	if !linked {
		return nil, fmt.Errorf("issue %q is not linked from this pull request", args.Key)
	}
	issue, err := t.Issues.FetchIssue(ctx, scope.InstallationID, args.Key)
	if err != nil {
		return nil, err
	}
	return struct {
		Key                string   `json:"key"`
		Title              string   `json:"title"`
		Description        string   `json:"description"`
		AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
		URL                string   `json:"url"`
	}{issue.Key, issue.Title, issue.Description, issue.AcceptanceCriteria, issue.URL}, nil
}

var errUnavailable = errors.New("tool is not configured for this review")

func decodeArgs(raw json.RawMessage, target any) error {
	if err := strictDecode(raw, target); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

func validRange(args *lineRange) error {
	clean, err := repositoryPath(args.Path)
	if err != nil {
		return err
	}
	args.Path = clean
	if args.StartLine < 1 || args.EndLine < args.StartLine {
		return errors.New("line range is invalid")
	}
	if args.EndLine-args.StartLine+1 > MaxReadFileLines {
		return fmt.Errorf("line range exceeds %d lines", MaxReadFileLines)
	}
	return nil
}

func repositoryPath(value string) (string, error) {
	clean := path.Clean(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || strings.HasPrefix(clean, "/") || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsRune(value, 0) {
		return "", errors.New("path must be repository-relative")
	}
	return clean, nil
}

func truncate(content string, limit int) (string, bool) {
	if len(content) <= limit {
		return content, false
	}
	cut := limit - len(truncationNotice)
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return content[:cut] + truncationNotice, true
}

func (t Toolbox) maxBytes() int {
	if t.MaxBytes <= 0 || t.MaxBytes > MaxToolOutput {
		return MaxToolOutput
	}
	return t.MaxBytes
}

func clamp(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	return min(value, maximum)
}

func fullSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
