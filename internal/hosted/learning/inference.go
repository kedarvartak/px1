package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const RuleInferenceWindow = 7 * 24 * time.Hour

type ReviewComment struct {
	ID          int64
	PullRequest int
	URL         string
	Text        string
	CreatedAt   time.Time
}

type CommentCluster struct {
	Theme      string
	RuleText   string
	CommentIDs []int64
}

type CommentClusterer interface {
	Cluster(context.Context, []ReviewComment) ([]CommentCluster, error)
}

type RuleInferenceStore interface {
	HumanReviewComments(context.Context, int64, int64, time.Time) ([]ReviewComment, error)
	RuleProposalExists(context.Context, int64, int64, string) (bool, error)
	MarkRuleProposal(context.Context, int64, int64, string, []int64) error
}

type RuleProposalPublisher interface {
	OpenRulesPullRequest(context.Context, int64, int64, string, string, string) error
}

type RuleInferenceJob struct {
	Store     RuleInferenceStore
	Clusterer CommentClusterer
	Publisher RuleProposalPublisher
	Now       func() time.Time
}

func (j RuleInferenceJob) Run(ctx context.Context, installationID, repositoryID int64) (int, error) {
	if j.Store == nil || j.Clusterer == nil || j.Publisher == nil || installationID <= 0 || repositoryID <= 0 {
		return 0, errors.New("rule inference requires store, clusterer, publisher, and tenant")
	}
	now := time.Now().UTC()
	if j.Now != nil {
		now = j.Now().UTC()
	}
	comments, err := j.Store.HumanReviewComments(ctx, installationID, repositoryID, now.Add(-RuleInferenceWindow))
	if err != nil {
		return 0, fmt.Errorf("load review comments: %w", err)
	}
	clusters, err := j.Clusterer.Cluster(ctx, comments)
	if err != nil {
		return 0, fmt.Errorf("cluster review comments: %w", err)
	}
	byID := map[int64]ReviewComment{}
	for _, comment := range comments {
		byID[comment.ID] = comment
	}
	opened := 0
	for _, cluster := range clusters {
		selected, prs := uniqueClusterComments(cluster.CommentIDs, byID)
		theme := strings.TrimSpace(cluster.Theme)
		ruleText := strings.TrimSpace(cluster.RuleText)
		if len(selected) < 3 || len(prs) < 2 || theme == "" || len(theme) > 200 || ruleText == "" || len(ruleText) > 8000 {
			continue
		}
		fingerprint := proposalFingerprint(theme, ruleText)
		exists, err := j.Store.RuleProposalExists(ctx, installationID, repositoryID, fingerprint)
		if err != nil {
			return opened, err
		}
		if exists {
			continue
		}
		body := renderRuleProposal(cluster, selected, byID, fingerprint)
		if err := j.Publisher.OpenRulesPullRequest(ctx, installationID, repositoryID, ".px1/rules.md", "px1: propose "+theme, body); err != nil {
			return opened, fmt.Errorf("open rule proposal: %w", err)
		}
		if err := j.Store.MarkRuleProposal(ctx, installationID, repositoryID, fingerprint, selected); err != nil {
			return opened, fmt.Errorf("mark rule proposal: %w", err)
		}
		opened++
	}
	return opened, nil
}

func uniqueClusterComments(ids []int64, comments map[int64]ReviewComment) ([]int64, map[int]bool) {
	seen, prs := map[int64]bool{}, map[int]bool{}
	var selected []int64
	for _, id := range ids {
		comment, ok := comments[id]
		if !ok || seen[id] || comment.PullRequest <= 0 || !strings.HasPrefix(comment.URL, "https://github.com/") {
			continue
		}
		seen[id], prs[comment.PullRequest] = true, true
		selected = append(selected, id)
	}
	sort.Slice(selected, func(i, k int) bool { return selected[i] < selected[k] })
	return selected, prs
}

func proposalFingerprint(theme, ruleText string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(theme+"\n"+ruleText), " "))
	digest := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(digest[:])
}

func renderRuleProposal(cluster CommentCluster, ids []int64, comments map[int64]ReviewComment, fingerprint string) string {
	var body strings.Builder
	fmt.Fprintf(&body, "<!-- px1-rule-proposal:%s -->\n", fingerprint)
	fmt.Fprintf(&body, "## Proposed review rule\n\n%s\n\n## Evidence\n", strings.TrimSpace(cluster.RuleText))
	for _, id := range ids {
		fmt.Fprintf(&body, "\n- %s", comments[id].URL)
	}
	body.WriteString("\n\nThis is a proposal inferred from recurring human comments. Review and edit it like any other repository policy.\n")
	return body.String()
}
