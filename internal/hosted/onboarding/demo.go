// Package onboarding coordinates bounded self-serve installation flows.
package onboarding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const MaxDemoRepositories = 20

type Repository struct {
	InstallationID, RepositoryID int64
	Owner, Name                  string
}
type MergedPull struct {
	Number       int
	HeadSHA, URL string
	MergedAt     time.Time
}
type DemoReview struct {
	InstallationID, RepositoryID                   int64
	PullNumber                                     int
	HeadSHA, SourceURL, IdempotencyKey, Provenance string
}

type MergedPullReader interface {
	LatestMergedPull(context.Context, Repository) (MergedPull, bool, error)
}
type DemoReviewQueue interface {
	EnqueueDemoReview(context.Context, DemoReview) (bool, error)
}

type DemoScheduler struct {
	Pulls MergedPullReader
	Queue DemoReviewQueue
}

func (s DemoScheduler) Schedule(ctx context.Context, installationID int64, repositories []Repository) (int, error) {
	if s.Pulls == nil || s.Queue == nil || installationID <= 0 || len(repositories) == 0 || len(repositories) > MaxDemoRepositories {
		return 0, errors.New("pull reader, queue, tenant, and one to twenty repositories are required")
	}
	seen := map[int64]bool{}
	scheduled := 0
	for _, repository := range repositories {
		if repository.InstallationID != installationID || repository.RepositoryID <= 0 || repository.Owner == "" || repository.Name == "" || seen[repository.RepositoryID] {
			return scheduled, errors.New("demo repositories must be unique and belong to the installation")
		}
		seen[repository.RepositoryID] = true
		pull, found, err := s.Pulls.LatestMergedPull(ctx, repository)
		if err != nil {
			return scheduled, err
		}
		if !found {
			continue
		}
		if pull.Number <= 0 || !fullSHA(pull.HeadSHA) || pull.MergedAt.IsZero() || !strings.HasPrefix(pull.URL, "https://") {
			return scheduled, errors.New("latest merged pull request is invalid")
		}
		job := DemoReview{InstallationID: installationID, RepositoryID: repository.RepositoryID, PullNumber: pull.Number, HeadSHA: strings.ToLower(pull.HeadSHA), SourceURL: pull.URL, IdempotencyKey: fmt.Sprintf("demo:%d:%d:%s", installationID, repository.RepositoryID, strings.ToLower(pull.HeadSHA)), Provenance: "Demo review of the latest merged pull request selected during installation. It does not represent the current default branch or a live pull request."}
		created, err := s.Queue.EnqueueDemoReview(ctx, job)
		if err != nil {
			return scheduled, err
		}
		if created {
			scheduled++
		}
	}
	return scheduled, nil
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
