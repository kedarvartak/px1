package learning

import (
	"context"
	"testing"
	"time"
)

type inferenceStoreStub struct {
	comments []ReviewComment
	marked   bool
}

func (s *inferenceStoreStub) HumanReviewComments(context.Context, int64, int64, time.Time) ([]ReviewComment, error) {
	return s.comments, nil
}
func (s *inferenceStoreStub) RuleProposalExists(context.Context, int64, int64, string) (bool, error) {
	return false, nil
}
func (s *inferenceStoreStub) MarkRuleProposal(context.Context, int64, int64, string, []int64) error {
	s.marked = true
	return nil
}

type clustererFunc func(context.Context, []ReviewComment) ([]CommentCluster, error)

func (f clustererFunc) Cluster(ctx context.Context, comments []ReviewComment) ([]CommentCluster, error) {
	return f(ctx, comments)
}

type proposalPublisherFunc func(context.Context, int64, int64, string, string, string) error

func (f proposalPublisherFunc) OpenRulesPullRequest(ctx context.Context, installationID, repositoryID int64, path, title, body string) error {
	return f(ctx, installationID, repositoryID, path, title, body)
}

func TestRuleInferenceRequiresThreeCommentsAcrossTwoPRs(t *testing.T) {
	store := &inferenceStoreStub{comments: []ReviewComment{{ID: 1, PullRequest: 10, URL: "https://github.com/a/r/pull/10#x"}, {ID: 2, PullRequest: 10, URL: "https://github.com/a/r/pull/10#y"}, {ID: 3, PullRequest: 11, URL: "https://github.com/a/r/pull/11#z"}}}
	opened := false
	job := RuleInferenceJob{Store: store, Clusterer: clustererFunc(func(context.Context, []ReviewComment) ([]CommentCluster, error) {
		return []CommentCluster{{Theme: "wrapped errors", RuleText: "Wrap returned errors with operation context.", CommentIDs: []int64{1, 2, 3}}}, nil
	}), Publisher: proposalPublisherFunc(func(_ context.Context, _, _ int64, path, _, body string) error {
		opened = path == ".px1/rules.md" && body != ""
		return nil
	})}
	count, err := job.Run(context.Background(), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || !opened || !store.marked {
		t.Fatalf("count=%d opened=%v marked=%v", count, opened, store.marked)
	}
}

func TestRuleInferenceSkipsOnePRTheme(t *testing.T) {
	store := &inferenceStoreStub{comments: []ReviewComment{{ID: 1, PullRequest: 10}, {ID: 2, PullRequest: 10}, {ID: 3, PullRequest: 10}}}
	job := RuleInferenceJob{Store: store, Clusterer: clustererFunc(func(context.Context, []ReviewComment) ([]CommentCluster, error) {
		return []CommentCluster{{Theme: "x", RuleText: "y", CommentIDs: []int64{1, 2, 3}}}, nil
	}), Publisher: proposalPublisherFunc(func(context.Context, int64, int64, string, string, string) error {
		t.Fatal("must not publish")
		return nil
	})}
	count, err := job.Run(context.Background(), 1, 2)
	if err != nil || count != 0 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}
