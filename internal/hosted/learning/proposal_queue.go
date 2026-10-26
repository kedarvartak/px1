package learning

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type ProposalState string

const (
	ProposalPending    ProposalState = "pending"
	ProposalPublishing ProposalState = "publishing"
	ProposalAccepted   ProposalState = "accepted"
	ProposalRejected   ProposalState = "rejected"
)

type QueuedRuleProposal struct {
	ID             string
	InstallationID int64
	RepositoryID   int64
	RuleText       string
	EvidenceLinks  []string
	State          ProposalState
	PullRequestURL string
}

type ProposalQueueStore interface {
	LoadProposal(context.Context, int64, int64, string) (QueuedRuleProposal, error)
	UpdatePendingProposal(context.Context, int64, int64, string, string) error
	RejectPendingProposal(context.Context, int64, int64, string) error
	ClaimPendingProposal(context.Context, int64, int64, string) (bool, error)
	CompleteProposal(context.Context, int64, int64, string, string) error
	ReleaseProposal(context.Context, int64, int64, string) error
}

type ProposalPullRequestPublisher interface {
	// OpenProposal is idempotent for proposalID.
	OpenProposal(context.Context, int64, int64, string, string, []string) (string, error)
}

type ProposalQueue struct {
	Store     ProposalQueueStore
	Publisher ProposalPullRequestPublisher
}

func (q ProposalQueue) Edit(ctx context.Context, installationID, repositoryID int64, proposalID, ruleText string) error {
	if err := q.validate(ctx, installationID, repositoryID, proposalID); err != nil {
		return err
	}
	ruleText = strings.TrimSpace(ruleText)
	if ruleText == "" || len(ruleText) > 8000 {
		return errors.New("rule text must contain 1 to 8000 bytes")
	}
	return q.Store.UpdatePendingProposal(ctx, installationID, repositoryID, proposalID, ruleText)
}

func (q ProposalQueue) Reject(ctx context.Context, installationID, repositoryID int64, proposalID string) error {
	if err := q.validate(ctx, installationID, repositoryID, proposalID); err != nil {
		return err
	}
	return q.Store.RejectPendingProposal(ctx, installationID, repositoryID, proposalID)
}

func (q ProposalQueue) Accept(ctx context.Context, installationID, repositoryID int64, proposalID string) (string, error) {
	if q.Publisher == nil {
		return "", errors.New("proposal publisher is required")
	}
	proposal, err := q.loadProposal(ctx, installationID, repositoryID, proposalID)
	if err != nil {
		return "", err
	}
	if proposal.State == ProposalPending {
		claimed, claimErr := q.Store.ClaimPendingProposal(ctx, installationID, repositoryID, proposalID)
		if claimErr != nil || !claimed {
			if claimErr == nil {
				claimErr = errors.New("proposal is no longer pending")
			}
			return "", claimErr
		}
	} else if proposal.State != ProposalPublishing {
		return "", errors.New("proposal cannot be accepted from its current state")
	}
	pullRequestURL, err := q.Publisher.OpenProposal(ctx, installationID, repositoryID, proposal.ID, proposal.RuleText, proposal.EvidenceLinks)
	if err != nil {
		_ = q.Store.ReleaseProposal(ctx, installationID, repositoryID, proposalID)
		return "", err
	}
	if !githubPullRequestURL(pullRequestURL) {
		_ = q.Store.ReleaseProposal(ctx, installationID, repositoryID, proposalID)
		return "", errors.New("publisher returned an invalid pull request URL")
	}
	if err := q.Store.CompleteProposal(ctx, installationID, repositoryID, proposalID, pullRequestURL); err != nil {
		return "", err
	}
	return pullRequestURL, nil
}

func (q ProposalQueue) validate(ctx context.Context, installationID, repositoryID int64, proposalID string) error {
	_, err := q.loadValidated(ctx, installationID, repositoryID, proposalID)
	return err
}

func (q ProposalQueue) loadValidated(ctx context.Context, installationID, repositoryID int64, proposalID string) (QueuedRuleProposal, error) {
	proposal, err := q.loadProposal(ctx, installationID, repositoryID, proposalID)
	if err != nil {
		return QueuedRuleProposal{}, err
	}
	if proposal.State != ProposalPending {
		return QueuedRuleProposal{}, errors.New("proposal is not pending")
	}
	return proposal, nil
}

func (q ProposalQueue) loadProposal(ctx context.Context, installationID, repositoryID int64, proposalID string) (QueuedRuleProposal, error) {
	if q.Store == nil || installationID <= 0 || repositoryID <= 0 || strings.TrimSpace(proposalID) == "" {
		return QueuedRuleProposal{}, errors.New("proposal store, tenant, and id are required")
	}
	proposal, err := q.Store.LoadProposal(ctx, installationID, repositoryID, proposalID)
	if err != nil {
		return QueuedRuleProposal{}, err
	}
	if proposal.ID != proposalID || proposal.InstallationID != installationID || proposal.RepositoryID != repositoryID || strings.TrimSpace(proposal.RuleText) == "" || len(proposal.EvidenceLinks) == 0 {
		return QueuedRuleProposal{}, errors.New("proposal is invalid")
	}
	for _, evidence := range proposal.EvidenceLinks {
		if !githubEvidenceURL(evidence) {
			return QueuedRuleProposal{}, errors.New("proposal evidence must be an HTTPS GitHub URL")
		}
	}
	return proposal, nil
}

func githubEvidenceURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Host, "github.com") && strings.Trim(parsed.Path, "/") != ""
}

func githubPullRequestURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") {
		return false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" {
		return false
	}
	number, err := strconv.Atoi(parts[3])
	return err == nil && number > 0
}
