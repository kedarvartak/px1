package githubapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"px1/internal/hosted/review"
)

const MaxInlineReviewComments = 15

type APIClient interface {
	Do(context.Context, int64, *http.Request) (*http.Response, error)
}

type ReviewPublisher struct {
	Client  APIClient
	BaseURL string
}

type ReviewRequest struct {
	HeadSHA  string
	RunID    string
	Body     string
	Findings []review.Finding
}

type createReviewPayload struct {
	CommitID string                `json:"commit_id"`
	Body     string                `json:"body"`
	Event    string                `json:"event"`
	Comments []createReviewComment `json:"comments"`
}

type createReviewComment struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      string `json:"side"`
	StartLine int    `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
}

// Create posts every inline finding in one GitHub review request. It does not
// retry POSTs: the caller's durable run state is the idempotency boundary.
func (p ReviewPublisher) Create(ctx context.Context, installationID int64, owner, repo string, pullNumber int, input ReviewRequest) error {
	if p.Client == nil || installationID <= 0 || pullNumber <= 0 || !validSlug(owner) || !validSlug(repo) {
		return errors.New("GitHub client, installation, repository, and pull number are required")
	}
	if !validReviewSHA(input.HeadSHA) || !validRunID(input.RunID) || strings.TrimSpace(input.Body) == "" {
		return errors.New("full head SHA, run ID, and review body are required")
	}
	if len(input.Findings) > MaxInlineReviewComments {
		return fmt.Errorf("inline finding count %d exceeds limit %d", len(input.Findings), MaxInlineReviewComments)
	}
	comments := make([]createReviewComment, 0, len(input.Findings))
	for _, finding := range input.Findings {
		if err := finding.Validate(); err != nil {
			return fmt.Errorf("invalid inline finding: %w", err)
		}
		comment := createReviewComment{Path: finding.Path, Line: finding.EndLine, Side: "RIGHT", Body: renderFindingComment(finding)}
		if finding.StartLine != finding.EndLine {
			comment.StartLine = finding.StartLine
			comment.StartSide = "RIGHT"
		}
		comments = append(comments, comment)
	}
	payload := createReviewPayload{
		CommitID: input.HeadSHA,
		Body:     fmt.Sprintf("<!-- px1-review-run:%s -->\n%s", input.RunID, input.Body),
		Event:    "COMMENT",
		Comments: comments,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", baseURL, url.PathEscape(owner), url.PathEscape(repo), pullNumber)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := p.Client.Do(ctx, installationID, request)
	if err != nil {
		return fmt.Errorf("create GitHub review: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return fmt.Errorf("create GitHub review: status %d", response.StatusCode)
	}
	return nil
}

func renderFindingComment(finding review.Finding) string {
	var body strings.Builder
	fmt.Fprintf(&body, "**%s: %s**\n\n%s\n\nEvidence: %s\n\nConfidence: %.2f", finding.Severity, finding.Title, finding.Body, finding.Evidence, finding.Confidence)
	if finding.Suggestion != "" {
		fence := "```"
		for strings.Contains(finding.Suggestion, fence) {
			fence += "`"
		}
		fmt.Fprintf(&body, "\n\n%ssuggestion\n%s\n%s", fence, finding.Suggestion, fence)
	}
	return body.String()
}

func validReviewSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func validRunID(value string) bool {
	if value == "" || len(value) > 100 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func validSlug(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
