package scm

import (
	"context"
	"io"
	"strings"
	"testing"
)

type fakeGitHub struct{ calls []string }

func (f *fakeGitHub) Diff(context.Context, Repository, PullRequest) (Diff, error) {
	f.calls = append(f.calls, "diff")
	return Diff{}, nil
}
func (f *fakeGitHub) Files(context.Context, Repository, PullRequest) ([]File, error) {
	f.calls = append(f.calls, "files")
	return nil, nil
}
func (f *fakeGitHub) UpsertSummary(context.Context, Repository, PullRequest, string, string) (Comment, error) {
	f.calls = append(f.calls, "summary")
	return Comment{}, nil
}
func (f *fakeGitHub) Threads(context.Context, Repository, PullRequest) ([]Thread, error) {
	f.calls = append(f.calls, "threads")
	return nil, nil
}
func (f *fakeGitHub) SetStatus(context.Context, Repository, PullRequest, CheckStatus) error {
	f.calls = append(f.calls, "status")
	return nil
}
func (f *fakeGitHub) VerifyWebhook(io.Reader, string, string) (Webhook, error) {
	f.calls = append(f.calls, "webhook")
	return Webhook{}, nil
}

func TestGitHubProviderDelegatesEveryCapability(t *testing.T) {
	operations := &fakeGitHub{}
	provider := GitHubProvider{Operations: operations}
	repository := Repository{InstallationID: 1, Owner: "acme", Name: "project"}
	pull := PullRequest{Number: 4, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40)}
	_, _ = provider.Diff(context.Background(), repository, pull)
	_, _ = provider.Files(context.Background(), repository, pull)
	_, _ = provider.UpsertSummary(context.Background(), repository, pull, "px1", "summary")
	_, _ = provider.Threads(context.Background(), repository, pull)
	_ = provider.SetStatus(context.Background(), repository, pull, CheckStatus{Name: "px1", State: "completed"})
	_, _ = provider.VerifyWebhook(strings.NewReader("{}"), "delivery", "sha256=x")
	if got := strings.Join(operations.calls, ","); got != "diff,files,summary,threads,status,webhook" {
		t.Fatalf("calls = %s", got)
	}
}

func TestGitHubProviderRejectsInvalidTenantBeforeDelegating(t *testing.T) {
	operations := &fakeGitHub{}
	provider := GitHubProvider{Operations: operations}
	_, err := provider.Files(context.Background(), Repository{Owner: "acme", Name: "project"}, PullRequest{Number: 1, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40)})
	if err == nil || len(operations.calls) != 0 {
		t.Fatal("invalid tenant reached provider operations")
	}
}
