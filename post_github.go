package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	hostedreview "px1/internal/hosted/review"
)

const (
	agenticCommentMarker = "<!-- px1-agentic-review -->"
	maxInlineFindings    = 20
	maxFilePages         = 30
)

type postGitHubOptions struct {
	findings, repo, head, footer, apiURL string
	pr                                   int
	dryRun                               bool
}

// runPostGitHub publishes findings.json to a pull request: one sticky summary
// comment that is updated on every push, plus inline comments for findings that
// sit on lines the pull request changed. The token comes only from GITHUB_TOKEN.
func runPostGitHub(args []string, stdout io.Writer) error {
	var o postGitHubOptions
	fs := flag.NewFlagSet("post-github", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&o.findings, "findings", "", "findings.json written by agentic-review (required)")
	fs.StringVar(&o.repo, "repo", os.Getenv("GITHUB_REPOSITORY"), "owner/name (default: $GITHUB_REPOSITORY)")
	fs.IntVar(&o.pr, "pr", 0, "pull request number (required)")
	fs.StringVar(&o.head, "head", "", "full head commit SHA the review was run on (default: the findings revision)")
	fs.StringVar(&o.footer, "footer", "", "one line appended to the summary, such as the run cost")
	fs.StringVar(&o.apiURL, "api-url", envOr("GITHUB_API_URL", "https://api.github.com"), "GitHub API base URL")
	fs.BoolVar(&o.dryRun, "dry-run", false, "print what would be posted; call nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if o.findings == "" || o.pr <= 0 || !validRepoSlug(o.repo) {
		return errors.New("post-github requires --findings, --pr and --repo owner/name")
	}
	data, err := os.ReadFile(o.findings)
	if err != nil {
		return fmt.Errorf("read findings: %w", err)
	}
	var envelope hostedFindingsEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode findings: %w", err)
	}
	if o.head == "" {
		o.head = envelope.Revision
	}
	if !validRevision(o.head) {
		return errors.New("a full 40-character head SHA is required")
	}
	for _, finding := range envelope.Findings {
		if err := finding.Validate(); err != nil {
			return fmt.Errorf("invalid finding: %w", err)
		}
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" && !o.dryRun {
		return errors.New("GITHUB_TOKEN is required (in a workflow: env GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }} with pull-requests: write)")
	}
	gh := githubClient{base: strings.TrimRight(o.apiURL, "/"), token: token, http: &http.Client{Timeout: 30 * time.Second}}
	ctx := context.Background()

	var commentable map[string]map[int]bool
	var posted map[string]bool
	if !o.dryRun {
		if commentable, err = gh.commentableLines(ctx, o.repo, o.pr); err != nil {
			return err
		}
		if posted, err = gh.postedFindingKeys(ctx, o.repo, o.pr); err != nil {
			return err
		}
	} else {
		commentable = map[string]map[int]bool{}
		for _, f := range envelope.Findings {
			commentable[f.Path] = map[int]bool{f.EndLine: true, f.StartLine: true}
		}
		posted = map[string]bool{}
	}
	plan := planPost(envelope.Findings, commentable, posted)
	summary := renderAgenticSummary(plan, o.head, o.footer)
	if o.dryRun {
		fmt.Fprintf(stdout, "Would post %d inline comment(s) and a summary:\n\n%s\n", len(plan.inline), summary)
		return nil
	}
	if len(plan.inline) > 0 {
		if err := gh.createReview(ctx, o.repo, o.pr, o.head, plan.inline); err != nil {
			return err
		}
	}
	if err := gh.upsertSummary(ctx, o.repo, o.pr, summary); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Posted %d inline comment(s), %d finding(s) in the summary.\n", len(plan.inline), len(plan.all))
	return nil
}

type inlineComment struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      string `json:"side"`
	StartLine int    `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
}

type postPlan struct {
	all      []hostedreview.Finding
	inline   []inlineComment
	inlined  map[string]bool // finding key -> shown as inline comment (now or earlier)
	outside  []hostedreview.Finding
	overflow int
}

func findingKey(f hostedreview.Finding) string {
	sum := sha256.Sum256([]byte(f.Path + "\x00" + f.Title))
	return hex.EncodeToString(sum[:6])
}

func planPost(findings []hostedreview.Finding, commentable map[string]map[int]bool, posted map[string]bool) postPlan {
	plan := postPlan{inlined: map[string]bool{}}
	plan.all = append(plan.all, findings...)
	sort.SliceStable(plan.all, func(i, j int) bool { return plan.all[i].Severity < plan.all[j].Severity })
	for _, f := range plan.all {
		key := findingKey(f)
		lines := commentable[f.Path]
		if !lines[f.EndLine] || (f.StartLine != f.EndLine && !lines[f.StartLine]) {
			plan.outside = append(plan.outside, f)
			continue
		}
		if posted[key] {
			plan.inlined[key] = true // already commented on an earlier push
			continue
		}
		if len(plan.inline) >= maxInlineFindings {
			plan.overflow++
			plan.outside = append(plan.outside, f)
			continue
		}
		c := inlineComment{Path: f.Path, Line: f.EndLine, Side: "RIGHT", Body: renderInlineFinding(f, key)}
		if f.StartLine != f.EndLine {
			c.StartLine, c.StartSide = f.StartLine, "RIGHT"
		}
		plan.inline = append(plan.inline, c)
		plan.inlined[key] = true
	}
	return plan
}

func renderInlineFinding(f hostedreview.Finding, key string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- px1-finding:%s -->\n**%s: %s**\n\n%s\n\nEvidence: %s\n\nConfidence: %.2f", key, f.Severity, f.Title, f.Body, f.Evidence, f.Confidence)
	if f.Suggestion != "" {
		fmt.Fprintf(&b, "\n\nSuggestion: %s", f.Suggestion)
	}
	return b.String()
}

func renderAgenticSummary(plan postPlan, head, footer string) string {
	var b strings.Builder
	b.WriteString(agenticCommentMarker + "\n")
	fmt.Fprintf(&b, "## px1 review\n\nReviewed `%s`. ", shortRevision(head))
	if len(plan.all) == 0 {
		b.WriteString("No verified findings. That is not evidence the change is correct: it means nothing the model proposed survived verification.\n")
	} else {
		fmt.Fprintf(&b, "%d verified finding(s).\n\n", len(plan.all))
		for _, f := range plan.all {
			where := "inline"
			if !plan.inlined[findingKey(f)] {
				where = "not on a changed line"
			}
			fmt.Fprintf(&b, "- **%s** `%s:%d` %s (%s)\n", f.Severity, f.Path, f.EndLine, f.Title, where)
		}
		for _, f := range plan.outside {
			fmt.Fprintf(&b, "\n<details><summary>%s: %s</summary>\n\n`%s:%d-%d`\n\n%s\n\nEvidence: %s\n\nConfidence: %.2f\n</details>\n", f.Severity, f.Title, f.Path, f.StartLine, f.EndLine, f.Body, f.Evidence, f.Confidence)
		}
	}
	if footer != "" {
		fmt.Fprintf(&b, "\n---\n%s\n", footer)
	}
	b.WriteString("\n_Findings come from an AI model; check each one before acting on it._\n")
	return b.String()
}

var patchHunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// patchLines returns the new-file line numbers a patch shows, which are the
// only lines GitHub accepts inline comments on.
func patchLines(patch string) map[int]bool {
	lines := map[int]bool{}
	n := 0
	for _, line := range strings.Split(patch, "\n") {
		if m := patchHunkHeader.FindStringSubmatch(line); m != nil {
			n, _ = strconv.Atoi(m[1])
			continue
		}
		if n == 0 || strings.HasPrefix(line, `\`) {
			continue
		}
		switch {
		case strings.HasPrefix(line, "-"):
		case strings.HasPrefix(line, "+"), strings.HasPrefix(line, " "):
			lines[n] = true
			n++
		}
	}
	return lines
}

type githubClient struct {
	base, token string
	http        *http.Client
}

func (g githubClient) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		hint := ""
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			hint = " (the workflow needs permissions: pull-requests: write; fork pull requests get a read-only token)"
		}
		return fmt.Errorf("%s %s: GitHub returned status %d%s", method, path, resp.StatusCode, hint)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
}

func repoPath(repo string) string {
	owner, name, _ := strings.Cut(repo, "/")
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

func (g githubClient) commentableLines(ctx context.Context, repo string, pr int) (map[string]map[int]bool, error) {
	result := map[string]map[int]bool{}
	for page := 1; page <= maxFilePages; page++ {
		var files []struct {
			Filename string `json:"filename"`
			Patch    string `json:"patch"`
		}
		if err := g.do(ctx, http.MethodGet, fmt.Sprintf("%s/pulls/%d/files?per_page=100&page=%d", repoPath(repo), pr, page), nil, &files); err != nil {
			return nil, err
		}
		for _, f := range files {
			result[f.Filename] = patchLines(f.Patch)
		}
		if len(files) < 100 {
			break
		}
	}
	return result, nil
}

var findingMarker = regexp.MustCompile(`<!-- px1-finding:([0-9a-f]{12}) -->`)

func (g githubClient) postedFindingKeys(ctx context.Context, repo string, pr int) (map[string]bool, error) {
	keys := map[string]bool{}
	for page := 1; page <= maxFilePages; page++ {
		var comments []struct {
			Body string `json:"body"`
		}
		if err := g.do(ctx, http.MethodGet, fmt.Sprintf("%s/pulls/%d/comments?per_page=100&page=%d", repoPath(repo), pr, page), nil, &comments); err != nil {
			return nil, err
		}
		for _, c := range comments {
			if m := findingMarker.FindStringSubmatch(c.Body); m != nil {
				keys[m[1]] = true
			}
		}
		if len(comments) < 100 {
			break
		}
	}
	return keys, nil
}

func (g githubClient) createReview(ctx context.Context, repo string, pr int, head string, comments []inlineComment) error {
	payload := map[string]any{"commit_id": head, "event": "COMMENT", "body": "px1 found issues on changed lines; details in the px1 review comment.", "comments": comments}
	return g.do(ctx, http.MethodPost, fmt.Sprintf("%s/pulls/%d/reviews", repoPath(repo), pr), payload, nil)
}

func (g githubClient) upsertSummary(ctx context.Context, repo string, pr int, body string) error {
	for page := 1; page <= maxFilePages; page++ {
		var comments []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
		}
		if err := g.do(ctx, http.MethodGet, fmt.Sprintf("%s/issues/%d/comments?per_page=100&page=%d", repoPath(repo), pr, page), nil, &comments); err != nil {
			return err
		}
		for _, c := range comments {
			if strings.HasPrefix(c.Body, agenticCommentMarker) {
				return g.do(ctx, http.MethodPatch, fmt.Sprintf("%s/issues/comments/%d", repoPath(repo), c.ID), map[string]string{"body": body}, nil)
			}
		}
		if len(comments) < 100 {
			break
		}
	}
	return g.do(ctx, http.MethodPost, fmt.Sprintf("%s/issues/%d/comments", repoPath(repo), pr), map[string]string{"body": body}, nil)
}

var repoSlug = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func validRepoSlug(s string) bool { return repoSlug.MatchString(s) }

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
