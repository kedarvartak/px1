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
)

const StaticReviewCommentMarker = "<!-- px1-static-review -->"

type SummaryAPIClient interface {
	Do(context.Context, int64, *http.Request) (*http.Response, error)
}

type StickySummaryPublisher struct {
	Client   SummaryAPIClient
	BaseURL  string
	MaxPages int
}

type issueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
}

// Upsert edits the marker-bearing comment owned by the expected bot, or
// creates one when absent. Marker text in a user's comment is never trusted.
func (p StickySummaryPublisher) Upsert(ctx context.Context, installationID int64, owner, repo string, pullNumber int, botLogin, body string) (string, error) {
	if p.Client == nil || installationID <= 0 || pullNumber <= 0 || !validSummarySlug(owner) || !validSummarySlug(repo) || strings.TrimSpace(botLogin) == "" {
		return "", errors.New("GitHub client, installation, repository, pull number, and bot login are required")
	}
	if !strings.HasPrefix(body, StaticReviewCommentMarker) {
		return "", errors.New("summary body is missing the px1 marker")
	}
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	commentID, err := p.find(ctx, installationID, baseURL, owner, repo, pullNumber, botLogin)
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]string{"body": body})
	method := http.MethodPost
	endpoint := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", baseURL, url.PathEscape(owner), url.PathEscape(repo), pullNumber)
	action := "created"
	if commentID != 0 {
		method = http.MethodPatch
		endpoint = fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d", baseURL, url.PathEscape(owner), url.PathEscape(repo), commentID)
		action = "updated"
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := p.Client.Do(ctx, installationID, request)
	if err != nil {
		return "", fmt.Errorf("%s sticky summary: %w", action, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return "", fmt.Errorf("%s sticky summary: status %d", action, response.StatusCode)
	}
	return action, nil
}

func (p StickySummaryPublisher) find(ctx context.Context, installationID int64, baseURL, owner, repo string, pullNumber int, botLogin string) (int64, error) {
	maxPages := p.MaxPages
	if maxPages <= 0 {
		maxPages = 10
	}
	for page := 1; page <= maxPages; page++ {
		endpoint := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments?per_page=100&page=%d", baseURL, url.PathEscape(owner), url.PathEscape(repo), pullNumber, page)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return 0, err
		}
		response, err := p.Client.Do(ctx, installationID, request)
		if err != nil {
			return 0, fmt.Errorf("list PR comments: %w", err)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return 0, fmt.Errorf("list PR comments: status %d", response.StatusCode)
		}
		var comments []issueComment
		err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&comments)
		response.Body.Close()
		if err != nil {
			return 0, fmt.Errorf("decode PR comments: %w", err)
		}
		for _, comment := range comments {
			if comment.User.Type == "Bot" && strings.EqualFold(comment.User.Login, botLogin) && strings.Contains(comment.Body, StaticReviewCommentMarker) {
				return comment.ID, nil
			}
		}
		if len(comments) < 100 {
			break
		}
		if page == maxPages {
			return 0, errors.New("PR comment search reached its page limit")
		}
	}
	return 0, nil
}

func validSummarySlug(value string) bool {
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
