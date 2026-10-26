package learning

import (
	"context"
	"errors"
	"testing"
)

type proposalStoreStub struct {
	proposal QueuedRuleProposal
	claimed  bool
	state    ProposalState
}

func (s *proposalStoreStub) LoadProposal(context.Context, int64, int64, string) (QueuedRuleProposal, error) {
	return s.proposal, nil
}
func (s *proposalStoreStub) UpdatePendingProposal(_ context.Context, _, _ int64, _ string, text string) error {
	s.proposal.RuleText = text
	return nil
}
func (s *proposalStoreStub) RejectPendingProposal(context.Context, int64, int64, string) error {
	s.state = ProposalRejected
	return nil
}
func (s *proposalStoreStub) ClaimPendingProposal(context.Context, int64, int64, string) (bool, error) {
	s.claimed, s.state, s.proposal.State = true, ProposalPublishing, ProposalPublishing
	return true, nil
}
func (s *proposalStoreStub) CompleteProposal(_ context.Context, _, _ int64, _ string, rawURL string) error {
	s.proposal.PullRequestURL, s.state, s.proposal.State = rawURL, ProposalAccepted, ProposalAccepted
	return nil
}
func (s *proposalStoreStub) ReleaseProposal(context.Context, int64, int64, string) error {
	s.state, s.proposal.State = ProposalPending, ProposalPending
	return nil
}

type proposalPublisherStub struct{ err error }

func (p proposalPublisherStub) OpenProposal(context.Context, int64, int64, string, string, []string) (string, error) {
	return "https://github.com/acme/repo/pull/12", p.err
}

func pendingProposal() QueuedRuleProposal {
	return QueuedRuleProposal{ID: "proposal-1", InstallationID: 1, RepositoryID: 2, RuleText: "Wrap errors.", EvidenceLinks: []string{"https://github.com/acme/repo/pull/1#discussion_r1"}, State: ProposalPending}
}

func TestProposalQueueAcceptsThroughAtomicClaim(t *testing.T) {
	store := &proposalStoreStub{proposal: pendingProposal()}
	queue := ProposalQueue{Store: store, Publisher: proposalPublisherStub{}}
	got, err := queue.Accept(context.Background(), 1, 2, "proposal-1")
	if err != nil || got != "https://github.com/acme/repo/pull/12" || !store.claimed || store.state != ProposalAccepted {
		t.Fatalf("url=%q store=%#v err=%v", got, store, err)
	}
}

func TestProposalQueueReleasesClaimWhenPublishingFails(t *testing.T) {
	store := &proposalStoreStub{proposal: pendingProposal()}
	queue := ProposalQueue{Store: store, Publisher: proposalPublisherStub{err: errors.New("GitHub unavailable")}}
	if _, err := queue.Accept(context.Background(), 1, 2, "proposal-1"); err == nil || store.state != ProposalPending {
		t.Fatalf("state=%q err=%v", store.state, err)
	}
}

func TestProposalQueueRecoversPublishingProposalIdempotently(t *testing.T) {
	proposal := pendingProposal()
	proposal.State = ProposalPublishing
	store := &proposalStoreStub{proposal: proposal, state: ProposalPublishing}
	queue := ProposalQueue{Store: store, Publisher: proposalPublisherStub{}}
	if _, err := queue.Accept(context.Background(), 1, 2, "proposal-1"); err != nil || store.claimed || store.state != ProposalAccepted {
		t.Fatalf("store=%#v err=%v", store, err)
	}
}

func TestProposalQueueRejectsCrossTenantProposal(t *testing.T) {
	store := &proposalStoreStub{proposal: pendingProposal()}
	queue := ProposalQueue{Store: store}
	if err := queue.Reject(context.Background(), 9, 2, "proposal-1"); err == nil {
		t.Fatal("accepted proposal from another installation")
	}
}
