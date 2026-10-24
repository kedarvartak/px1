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
	"time"
)

const RetryReviewAction = "retry_review"

type CheckAPIClient interface {
	Do(context.Context, int64, *http.Request) (*http.Response, error)
}

type CheckRunPublisher struct {
	Client  CheckAPIClient
	BaseURL string
	Now     func() time.Time
}

type CheckRunStart struct {
	HeadSHA    string
	RunID      string
	DetailsURL string
	Summary    string
}

type CheckRunResult struct {
	Conclusion string
	Title      string
	Summary    string
}

type checkOutput struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

type checkAction struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Identifier  string `json:"identifier"`
}

func (p CheckRunPublisher) Start(ctx context.Context, installationID int64, owner, repo string, input CheckRunStart) (int64, error) {
	if err := p.validate(installationID, owner, repo); err != nil {
		return 0, err
	}
	if !validCheckSHA(input.HeadSHA) || strings.TrimSpace(input.RunID) == "" || !validHTTPURL(input.DetailsURL) {
		return 0, errors.New("full head SHA, run ID, and details URL are required")
	}
	payload := map[string]any{
		"name": "px1 review", "head_sha": input.HeadSHA, "status": "in_progress",
		"external_id": input.RunID, "details_url": input.DetailsURL,
		"output": checkOutput{Title: "px1 review in progress", Summary: boundedCheckText(input.Summary)},
	}
	response, err := p.send(ctx, installationID, http.MethodPost, fmt.Sprintf("/repos/%s/%s/check-runs", url.PathEscape(owner), url.PathEscape(repo)), payload)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&created); err != nil || created.ID <= 0 {
		return 0, errors.New("GitHub returned an invalid check run ID")
	}
	return created.ID, nil
}

func (p CheckRunPublisher) Complete(ctx context.Context, installationID int64, owner, repo string, checkRunID int64, result CheckRunResult) error {
	if err := p.validate(installationID, owner, repo); err != nil {
		return err
	}
	if checkRunID <= 0 || (result.Conclusion != "neutral" && result.Conclusion != "failure") || strings.TrimSpace(result.Title) == "" {
		return errors.New("check run ID, neutral or failure conclusion, and title are required")
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	payload := map[string]any{
		"status": "completed", "conclusion": result.Conclusion, "completed_at": now().UTC().Format(time.RFC3339),
		"output":  checkOutput{Title: result.Title, Summary: boundedCheckText(result.Summary)},
		"actions": []checkAction{{Label: "Retry review", Description: "Run px1 again for this pull request head", Identifier: RetryReviewAction}},
	}
	response, err := p.send(ctx, installationID, http.MethodPatch, fmt.Sprintf("/repos/%s/%s/check-runs/%d", url.PathEscape(owner), url.PathEscape(repo), checkRunID), payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return nil
}

func ReviewConclusion(hasP0, failOnP0 bool) string {
	if hasP0 && failOnP0 {
		return "failure"
	}
	return "neutral"
}

func (p CheckRunPublisher) validate(installationID int64, owner, repo string) error {
	if p.Client == nil || installationID <= 0 || !validCheckSlug(owner) || !validCheckSlug(repo) {
		return errors.New("GitHub client, installation, and repository are required")
	}
	return nil
}

func (p CheckRunPublisher) send(ctx context.Context, installationID int64, method, path string, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	request, err := http.NewRequestWithContext(ctx, method, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := p.Client.Do(ctx, installationID, request)
	if err != nil {
		return nil, fmt.Errorf("publish GitHub check run: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, fmt.Errorf("publish GitHub check run: status %d", response.StatusCode)
	}
	return response, nil
}

func boundedCheckText(value string) string {
	const maxBytes = 60_000
	if len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "\n\n[truncated by px1]"
}

func validHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

func validCheckSHA(value string) bool {
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

func validCheckSlug(value string) bool {
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
