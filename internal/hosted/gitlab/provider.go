// Package gitlab implements the provider-neutral SCM contract for GitLab.
package gitlab

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"px1/internal/hosted/scm"
)

const maxResponseBytes = 20 << 20

type TokenSource interface {
	Token(context.Context, int64) (string, error)
}

type Provider struct {
	HTTP         *http.Client
	Tokens       TokenSource
	BaseURL      string
	WebhookToken string
}

type change struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
	Collapsed   bool   `json:"collapsed"`
	TooLarge    bool   `json:"too_large"`
}

func (p Provider) Diff(ctx context.Context, repository scm.Repository, pull scm.PullRequest) (scm.Diff, error) {
	changes, err := p.changes(ctx, repository, pull)
	if err != nil {
		return scm.Diff{}, err
	}
	var patch strings.Builder
	for _, item := range changes {
		fmt.Fprintf(&patch, "diff --git a/%s b/%s\n%s\n", item.OldPath, item.NewPath, item.Diff)
	}
	return scm.Diff{BaseSHA: pull.BaseSHA, HeadSHA: pull.HeadSHA, Patch: []byte(patch.String())}, nil
}

func (p Provider) Files(ctx context.Context, repository scm.Repository, pull scm.PullRequest) ([]scm.File, error) {
	changes, err := p.changes(ctx, repository, pull)
	if err != nil {
		return nil, err
	}
	files := make([]scm.File, 0, len(changes))
	for _, item := range changes {
		status := "modified"
		if item.NewFile {
			status = "added"
		}
		if item.DeletedFile {
			status = "deleted"
		}
		if item.RenamedFile {
			status = "renamed"
		}
		files = append(files, scm.File{Path: item.NewPath, Status: status})
	}
	return files, nil
}

func (p Provider) UpsertSummary(ctx context.Context, repository scm.Repository, pull scm.PullRequest, marker, body string) (scm.Comment, error) {
	if err := validate(repository, pull); err != nil || marker == "" || !strings.Contains(body, marker) {
		return scm.Comment{}, errors.New("valid merge request and marker-bearing summary are required")
	}
	var notes []struct {
		ID        int64     `json:"id"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
	}
	path := p.projectPath(repository) + "/merge_requests/" + strconv.Itoa(pull.Number) + "/notes"
	if err := p.request(ctx, repository.InstallationID, http.MethodGet, path, nil, &notes); err != nil {
		return scm.Comment{}, err
	}
	method, endpoint := http.MethodPost, path
	for _, note := range notes {
		if strings.Contains(note.Body, marker) {
			method, endpoint = http.MethodPut, path+"/"+strconv.FormatInt(note.ID, 10)
			break
		}
	}
	var saved struct {
		ID        int64     `json:"id"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := p.request(ctx, repository.InstallationID, method, endpoint, map[string]string{"body": body}, &saved); err != nil {
		return scm.Comment{}, err
	}
	return scm.Comment{ID: strconv.FormatInt(saved.ID, 10), Body: saved.Body, CreatedAt: saved.CreatedAt}, nil
}

func (p Provider) Threads(ctx context.Context, repository scm.Repository, pull scm.PullRequest) ([]scm.Thread, error) {
	if err := validate(repository, pull); err != nil {
		return nil, err
	}
	var discussions []struct {
		ID    string `json:"id"`
		Notes []struct {
			ID        int64     `json:"id"`
			Body      string    `json:"body"`
			Resolved  bool      `json:"resolved"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"notes"`
	}
	path := p.projectPath(repository) + "/merge_requests/" + strconv.Itoa(pull.Number) + "/discussions"
	if err := p.request(ctx, repository.InstallationID, http.MethodGet, path, nil, &discussions); err != nil {
		return nil, err
	}
	threads := make([]scm.Thread, 0, len(discussions))
	for _, discussion := range discussions {
		thread := scm.Thread{ID: discussion.ID, Resolved: true}
		for _, note := range discussion.Notes {
			thread.Resolved = thread.Resolved && note.Resolved
			thread.Comments = append(thread.Comments, scm.Comment{ID: strconv.FormatInt(note.ID, 10), Body: note.Body, CreatedAt: note.CreatedAt})
		}
		threads = append(threads, thread)
	}
	return threads, nil
}

func (p Provider) SetStatus(ctx context.Context, repository scm.Repository, pull scm.PullRequest, status scm.CheckStatus) error {
	if err := validate(repository, pull); err != nil || status.Name == "" || status.State == "" {
		return errors.New("valid merge request, status name, and state are required")
	}
	state, err := mapStatus(status.State)
	if err != nil {
		return err
	}
	payload := map[string]string{"state": state, "name": status.Name, "description": status.Summary, "target_url": status.DetailsURL}
	return p.request(ctx, repository.InstallationID, http.MethodPost, p.projectPath(repository)+"/statuses/"+pull.HeadSHA, payload, nil)
}

func (p Provider) VerifyWebhook(body io.Reader, delivery, token string) (scm.Webhook, error) {
	if body == nil || delivery == "" || p.WebhookToken == "" || len(token) != len(p.WebhookToken) || subtle.ConstantTimeCompare([]byte(token), []byte(p.WebhookToken)) != 1 {
		return scm.Webhook{}, errors.New("GitLab webhook token or delivery is invalid")
	}
	payload, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil || len(payload) > maxResponseBytes || !json.Valid(payload) {
		return scm.Webhook{}, errors.New("GitLab webhook body is invalid or too large")
	}
	var envelope struct {
		Kind string `json:"object_kind"`
	}
	if json.Unmarshal(payload, &envelope) != nil || envelope.Kind == "" {
		return scm.Webhook{}, errors.New("GitLab webhook kind is required")
	}
	return scm.Webhook{DeliveryID: delivery, Event: envelope.Kind, Body: payload}, nil
}

func (p Provider) changes(ctx context.Context, repository scm.Repository, pull scm.PullRequest) ([]change, error) {
	if err := validate(repository, pull); err != nil {
		return nil, err
	}
	path := p.projectPath(repository) + "/merge_requests/" + strconv.Itoa(pull.Number) + "/diffs"
	var changes []change
	for page := 1; page <= 10; page++ {
		var batch []change
		endpoint := path + "?per_page=100&page=" + strconv.Itoa(page)
		if err := p.request(ctx, repository.InstallationID, http.MethodGet, endpoint, nil, &batch); err != nil {
			return nil, err
		}
		changes = append(changes, batch...)
		for _, item := range batch {
			if item.Collapsed || item.TooLarge {
				return nil, errors.New("GitLab omitted an oversized or collapsed diff; review cannot continue with incomplete evidence")
			}
		}
		if len(batch) < 100 {
			return changes, nil
		}
	}
	return nil, errors.New("GitLab merge request diff exceeded the 1000-file retrieval limit")
}

func (p Provider) projectPath(repository scm.Repository) string {
	return "/projects/" + url.PathEscape(repository.Owner+"/"+repository.Name)
}

func (p Provider) request(ctx context.Context, installationID int64, method, path string, payload, output any) error {
	if p.Tokens == nil {
		return errors.New("GitLab token source is required")
	}
	token, err := p.Tokens.Token(ctx, installationID)
	if err != nil || token == "" {
		return errors.New("GitLab access token is unavailable")
	}
	var body io.Reader
	if payload != nil {
		encoded, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		body = bytes.NewReader(encoded)
	}
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = "https://gitlab.com/api/v4"
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("PRIVATE-TOKEN", token)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return fmt.Errorf("GitLab API returned status %d", response.StatusCode)
	}
	if output == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(output)
}

func validate(repository scm.Repository, pull scm.PullRequest) error {
	if err := scm.ValidateRepository(repository); err != nil {
		return err
	}
	return scm.ValidatePullRequest(pull)
}

func mapStatus(state string) (string, error) {
	switch state {
	case "pending", "running", "success", "failed", "canceled", "skipped":
		return state, nil
	case "in_progress":
		return "running", nil
	case "completed", "neutral":
		return "success", nil
	case "failure":
		return "failed", nil
	default:
		return "", errors.New("unsupported GitLab status state")
	}
}

type DiffPosition struct {
	OldPath, NewPath string
	OldLine, NewLine int
	LineCode         string
}

func MapDiffPosition(oldPath, newPath string, oldLine, newLine int) (DiffPosition, error) {
	if oldPath == "" || newPath == "" || oldLine < 0 || newLine < 0 || (oldLine == 0 && newLine == 0) {
		return DiffPosition{}, errors.New("paths and at least one positive line are required")
	}
	digest := sha1.Sum([]byte(newPath))
	return DiffPosition{OldPath: oldPath, NewPath: newPath, OldLine: oldLine, NewLine: newLine, LineCode: hex.EncodeToString(digest[:]) + "_" + strconv.Itoa(oldLine) + "_" + strconv.Itoa(newLine)}, nil
}

var _ scm.Provider = Provider{}
