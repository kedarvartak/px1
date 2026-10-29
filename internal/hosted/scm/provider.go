// Package scm defines the provider-neutral source-control boundary used by hosted reviews.
package scm

import (
	"context"
	"errors"
	"io"
	"time"
)

type Repository struct {
	InstallationID int64
	Owner, Name    string
}
type PullRequest struct {
	Number           int
	BaseSHA, HeadSHA string
}
type File struct {
	Path, Status         string
	Additions, Deletions int
}
type Diff struct {
	BaseSHA, HeadSHA string
	Patch            []byte
}
type Comment struct {
	ID, Body, Path string
	Line           int
	CreatedAt      time.Time
}
type Thread struct {
	ID       string
	Resolved bool
	Comments []Comment
}
type CheckStatus struct{ Name, State, Summary, DetailsURL string }
type Webhook struct {
	DeliveryID, Event string
	Body              []byte
}

type Provider interface {
	Diff(context.Context, Repository, PullRequest) (Diff, error)
	Files(context.Context, Repository, PullRequest) ([]File, error)
	UpsertSummary(context.Context, Repository, PullRequest, string, string) (Comment, error)
	Threads(context.Context, Repository, PullRequest) ([]Thread, error)
	SetStatus(context.Context, Repository, PullRequest, CheckStatus) error
	VerifyWebhook(io.Reader, string, string) (Webhook, error)
}

func ValidateRepository(repository Repository) error {
	if repository.InstallationID <= 0 || repository.Owner == "" || repository.Name == "" {
		return errors.New("installation, owner, and repository name are required")
	}
	return nil
}

func ValidatePullRequest(pull PullRequest) error {
	if pull.Number <= 0 || !fullSHA(pull.BaseSHA) || !fullSHA(pull.HeadSHA) || pull.BaseSHA == pull.HeadSHA {
		return errors.New("pull number and distinct full base/head SHAs are required")
	}
	return nil
}

func fullSHA(value string) bool {
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

// GitHubOperations is implemented by the existing GitHub App delivery layer.
// Keeping it separate lets the adapter be contract-tested without GitHub calls.
type GitHubOperations interface {
	Diff(context.Context, Repository, PullRequest) (Diff, error)
	Files(context.Context, Repository, PullRequest) ([]File, error)
	UpsertSummary(context.Context, Repository, PullRequest, string, string) (Comment, error)
	Threads(context.Context, Repository, PullRequest) ([]Thread, error)
	SetStatus(context.Context, Repository, PullRequest, CheckStatus) error
	VerifyWebhook(io.Reader, string, string) (Webhook, error)
}

type GitHubProvider struct{ Operations GitHubOperations }

func (p GitHubProvider) ready(repository Repository, pull PullRequest) error {
	if p.Operations == nil {
		return errors.New("GitHub operations are required")
	}
	if err := ValidateRepository(repository); err != nil {
		return err
	}
	return ValidatePullRequest(pull)
}
func (p GitHubProvider) Diff(ctx context.Context, r Repository, pr PullRequest) (Diff, error) {
	if err := p.ready(r, pr); err != nil {
		return Diff{}, err
	}
	return p.Operations.Diff(ctx, r, pr)
}
func (p GitHubProvider) Files(ctx context.Context, r Repository, pr PullRequest) ([]File, error) {
	if err := p.ready(r, pr); err != nil {
		return nil, err
	}
	return p.Operations.Files(ctx, r, pr)
}
func (p GitHubProvider) UpsertSummary(ctx context.Context, r Repository, pr PullRequest, marker, body string) (Comment, error) {
	if err := p.ready(r, pr); err != nil || marker == "" || body == "" {
		return Comment{}, errors.New("valid pull request, marker, and summary are required")
	}
	return p.Operations.UpsertSummary(ctx, r, pr, marker, body)
}
func (p GitHubProvider) Threads(ctx context.Context, r Repository, pr PullRequest) ([]Thread, error) {
	if err := p.ready(r, pr); err != nil {
		return nil, err
	}
	return p.Operations.Threads(ctx, r, pr)
}
func (p GitHubProvider) SetStatus(ctx context.Context, r Repository, pr PullRequest, status CheckStatus) error {
	if err := p.ready(r, pr); err != nil {
		return err
	}
	if status.Name == "" || status.State == "" {
		return errors.New("status name and state are required")
	}
	return p.Operations.SetStatus(ctx, r, pr, status)
}
func (p GitHubProvider) VerifyWebhook(body io.Reader, delivery, signature string) (Webhook, error) {
	if p.Operations == nil || body == nil || delivery == "" || signature == "" {
		return Webhook{}, errors.New("GitHub operations, body, delivery, and signature are required")
	}
	return p.Operations.VerifyWebhook(body, delivery, signature)
}

var _ Provider = GitHubProvider{}
