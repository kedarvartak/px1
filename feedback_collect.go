package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// feedbackStats is what people did with px1's inline comments: the only
// precision signal that comes from real use instead of a seeded benchmark.
type feedbackStats struct {
	Repo       string            `json:"repo"`
	Since      string            `json:"since"`
	Comments   int               `json:"comments"`
	ThumbsUp   int               `json:"thumbs_up"`
	ThumbsDown int               `json:"thumbs_down"`
	Replied    int               `json:"replied"`
	Engaged    int               `json:"engaged"` // reacted to or replied to
	Findings   []feedbackFinding `json:"findings"`
}

type feedbackFinding struct {
	PullRequest string `json:"pull_request"`
	Path        string `json:"path"`
	Line        int    `json:"line"`
	Title       string `json:"title"`
	ThumbsUp    int    `json:"thumbs_up"`
	ThumbsDown  int    `json:"thumbs_down"`
	Replies     int    `json:"replies"`
}

// Acceptance is thumbs-up out of all thumbs. It is undefined (ok=false) until
// someone has reacted: silence is not agreement.
func (s feedbackStats) Acceptance() (float64, bool) {
	if s.ThumbsUp+s.ThumbsDown == 0 {
		return 0, false
	}
	return float64(s.ThumbsUp) / float64(s.ThumbsUp+s.ThumbsDown), true
}

// runFeedbackCollect counts the thumbs-up, thumbs-down and replies people left
// on px1's inline review comments in one repository. It reads from the GitHub
// API with GITHUB_TOKEN and writes nothing there.
func runFeedbackCollect(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("feedback-collect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := fs.String("repo", os.Getenv("GITHUB_REPOSITORY"), "owner/name (default: $GITHUB_REPOSITORY)")
	days := fs.Int("days", 30, "only comments from the last N days")
	apiURL := fs.String("api-url", envOr("GITHUB_API_URL", "https://api.github.com"), "GitHub API base URL")
	out := fs.String("out", "", "optional file for the JSON result")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !validRepoSlug(*repo) || *days < 1 || *days > 365 {
		return errors.New("feedback-collect requires --repo owner/name and --days between 1 and 365")
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return errors.New("GITHUB_TOKEN is required (for a private repository try GITHUB_TOKEN=$(gh auth token))")
	}
	gh := githubClient{base: strings.TrimRight(*apiURL, "/"), token: token, http: &http.Client{Timeout: 30 * time.Second}}
	since := time.Now().UTC().AddDate(0, 0, -*days)
	stats, err := collectFeedback(context.Background(), gh, *repo, since)
	if err != nil {
		return err
	}
	if *out != "" {
		data, _ := json.MarshalIndent(stats, "", "  ")
		if err := os.WriteFile(*out, append(data, '\n'), 0o600); err != nil {
			return err
		}
	}
	_, err = io.WriteString(stdout, stats.Markdown())
	return err
}

type reviewCommentAPI struct {
	ID          int64     `json:"id"`
	InReplyTo   int64     `json:"in_reply_to_id"`
	Body        string    `json:"body"`
	Path        string    `json:"path"`
	Line        int       `json:"line"`
	PullRequest string    `json:"pull_request_url"`
	CreatedAt   time.Time `json:"created_at"`
	User        struct {
		Type string `json:"type"`
	} `json:"user"`
	Reactions struct {
		Up   int `json:"+1"`
		Down int `json:"-1"`
	} `json:"reactions"`
}

func collectFeedback(ctx context.Context, gh githubClient, repo string, since time.Time) (feedbackStats, error) {
	stats := feedbackStats{Repo: repo, Since: since.Format("2006-01-02")}
	var all []reviewCommentAPI
	for page := 1; page <= maxFilePages; page++ {
		var comments []reviewCommentAPI
		path := fmt.Sprintf("%s/pulls/comments?per_page=100&sort=created&direction=desc&since=%s&page=%d", repoPath(repo), since.Format(time.RFC3339), page)
		if err := gh.do(ctx, http.MethodGet, path, nil, &comments); err != nil {
			return stats, err
		}
		all = append(all, comments...)
		if len(comments) < 100 {
			break
		}
	}
	replies := map[int64]int{}
	for _, c := range all {
		if c.InReplyTo != 0 && c.User.Type != "Bot" {
			replies[c.InReplyTo]++
		}
	}
	for _, c := range all {
		if c.InReplyTo != 0 || c.CreatedAt.Before(since) || !strings.Contains(c.Body, "<!-- px1-finding:") {
			continue
		}
		f := feedbackFinding{PullRequest: c.PullRequest, Path: c.Path, Line: c.Line, Title: findingTitle(c.Body),
			ThumbsUp: c.Reactions.Up, ThumbsDown: c.Reactions.Down, Replies: replies[c.ID]}
		stats.Comments++
		stats.ThumbsUp += f.ThumbsUp
		stats.ThumbsDown += f.ThumbsDown
		if f.Replies > 0 {
			stats.Replied++
		}
		if f.ThumbsUp+f.ThumbsDown+f.Replies > 0 {
			stats.Engaged++
		}
		stats.Findings = append(stats.Findings, f)
	}
	sort.SliceStable(stats.Findings, func(i, j int) bool {
		return stats.Findings[i].ThumbsDown-stats.Findings[i].ThumbsUp > stats.Findings[j].ThumbsDown-stats.Findings[j].ThumbsUp
	})
	return stats, nil
}

// findingTitle pulls the bold "**P2: title**" line out of an inline comment.
func findingTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
			return strings.Trim(line, "*")
		}
	}
	return ""
}

func (s feedbackStats) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# px1 feedback on %s since %s\n\n", s.Repo, s.Since)
	if s.Comments == 0 {
		b.WriteString("No px1 inline comments in this period.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "- %d inline comments; %d reacted to or replied to (%.0f%% engaged)\n", s.Comments, s.Engaged, 100*float64(s.Engaged)/float64(s.Comments))
	fmt.Fprintf(&b, "- 👍 %d, 👎 %d, replies on %d comments\n", s.ThumbsUp, s.ThumbsDown, s.Replied)
	if a, ok := s.Acceptance(); ok {
		fmt.Fprintf(&b, "- **Acceptance %.0f%%** of reactions were 👍 (from %d reactions; read it as a rough signal, people react to few comments)\n", a*100, s.ThumbsUp+s.ThumbsDown)
	} else {
		b.WriteString("- **Acceptance unknown:** nobody has reacted yet. Silence is not agreement.\n")
	}
	b.WriteString("\n| Finding | 👍 | 👎 | Replies |\n| --- | --- | --- | --- |\n")
	for _, f := range s.Findings {
		title := f.Title
		if title == "" {
			title = f.Path
		}
		fmt.Fprintf(&b, "| %s (`%s:%d`) | %d | %d | %d |\n", title, f.Path, f.Line, f.ThumbsUp, f.ThumbsDown, f.Replies)
	}
	return b.String()
}
