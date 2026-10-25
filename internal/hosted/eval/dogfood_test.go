package eval

import (
	"fmt"
	"testing"
	"time"
)

func TestSummarizeDogfoodEnforcesLaunchWindow(t *testing.T) {
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var reviews []DogfoodReview
	for i := 0; i < 4; i++ {
		reviews = append(reviews, DogfoodReview{Repository: fmt.Sprintf("repo-%d", i), PullRequestURL: fmt.Sprintf("https://github.com/acme/repo-%d/pull/1", i), ReviewedAt: start.Add(time.Duration(i*13) * 24 * time.Hour / 3), ReviewLatency: time.Duration(i+1) * time.Minute, Comments: 2, CommentsAddressed: 1, HelpfulVotes: 1, HumanReviewDone: true, CostUSD: .05})
	}
	summary, err := SummarizeDogfood(reviews)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Ready || summary.Repositories != 4 || summary.ObservationDays != 14 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.CommentAddressRate != .5 || summary.HelpfulVoteRate != 1 || summary.TotalCostUSD != .2 {
		t.Fatalf("incorrect metrics: %+v", summary)
	}
}

func TestSummarizeDogfoodDoesNotPassIncompleteHumanReview(t *testing.T) {
	review := DogfoodReview{Repository: "repo", PullRequestURL: "https://github.com/acme/repo/pull/1", ReviewedAt: time.Now(), ReviewLatency: time.Minute}
	summary, err := SummarizeDogfood([]DogfoodReview{review})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Ready {
		t.Fatal("incomplete dogfood run passed")
	}
}
