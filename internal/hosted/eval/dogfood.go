package eval

import (
	"errors"
	"sort"
	"time"
)

// DogfoodReview records the human outcome of one px1-assisted pull-request
// review. Votes describe feedback on px1's review, not approval of the PR.
type DogfoodReview struct {
	Repository        string
	PullRequestURL    string
	ReviewedAt        time.Time
	ReviewLatency     time.Duration
	Comments          int
	CommentsAddressed int
	HelpfulVotes      int
	UnhelpfulVotes    int
	HumanReviewDone   bool
	CostUSD           float64
}

type DogfoodSummary struct {
	Repositories       int
	Reviews            int
	ObservationDays    int
	MedianLatency      time.Duration
	CommentAddressRate float64
	HelpfulVoteRate    float64
	TotalCostUSD       float64
	Ready              bool
}

func SummarizeDogfood(reviews []DogfoodReview) (DogfoodSummary, error) {
	if len(reviews) == 0 {
		return DogfoodSummary{}, errors.New("dogfood evidence is empty")
	}
	repositories := map[string]bool{}
	latencies := make([]time.Duration, 0, len(reviews))
	first, last := reviews[0].ReviewedAt, reviews[0].ReviewedAt
	var comments, addressed, helpful, unhelpful int
	var cost float64
	allHumanReviewed := true
	for _, review := range reviews {
		if review.Repository == "" || review.PullRequestURL == "" || review.ReviewedAt.IsZero() || review.ReviewLatency < 0 || review.Comments < 0 || review.CommentsAddressed < 0 || review.CommentsAddressed > review.Comments || review.HelpfulVotes < 0 || review.UnhelpfulVotes < 0 || review.CostUSD < 0 {
			return DogfoodSummary{}, errors.New("dogfood evidence contains an invalid review")
		}
		repositories[review.Repository] = true
		latencies = append(latencies, review.ReviewLatency)
		if review.ReviewedAt.Before(first) {
			first = review.ReviewedAt
		}
		if review.ReviewedAt.After(last) {
			last = review.ReviewedAt
		}
		comments += review.Comments
		addressed += review.CommentsAddressed
		helpful += review.HelpfulVotes
		unhelpful += review.UnhelpfulVotes
		cost += review.CostUSD
		allHumanReviewed = allHumanReviewed && review.HumanReviewDone
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	median := latencies[len(latencies)/2]
	if len(latencies)%2 == 0 {
		median = (latencies[len(latencies)/2-1] + median) / 2
	}
	summary := DogfoodSummary{Repositories: len(repositories), Reviews: len(reviews), ObservationDays: int(last.Sub(first).Hours()/24) + 1, MedianLatency: median, TotalCostUSD: cost}
	if comments > 0 {
		summary.CommentAddressRate = float64(addressed) / float64(comments)
	}
	if helpful+unhelpful > 0 {
		summary.HelpfulVoteRate = float64(helpful) / float64(helpful+unhelpful)
	}
	summary.Ready = len(repositories) >= 4 && summary.ObservationDays >= 14 && allHumanReviewed
	return summary, nil
}
