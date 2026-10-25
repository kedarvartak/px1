package githubapp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type reactionClientFunc func(context.Context, int64, *http.Request) (*http.Response, error)

func (f reactionClientFunc) Do(ctx context.Context, installationID int64, request *http.Request) (*http.Response, error) {
	return f(ctx, installationID, request)
}

type reactionStoreStub struct {
	due      []ReactionComment
	feedback []ReactionFeedback
	next     time.Time
	done     bool
}

func (s *reactionStoreStub) DueReactionComments(context.Context, time.Time) ([]ReactionComment, error) {
	return s.due, nil
}
func (s *reactionStoreStub) SaveReactionPoll(_ context.Context, _ ReactionComment, feedback []ReactionFeedback, next time.Time, done bool) error {
	s.feedback, s.next, s.done = feedback, next, done
	return nil
}

func TestReactionPollerStoresVotesAndSchedulesFifteenMinutes(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	store := &reactionStoreStub{due: []ReactionComment{{InstallationID: 1, RepositoryID: 2, Owner: "acme", Repository: "app", CommentID: 3, ReviewCreatedAt: now.Add(-time.Hour)}}}
	poller := ReactionPoller{Store: store, Now: func() time.Time { return now }, BaseURL: "https://api.example", Client: reactionClientFunc(func(_ context.Context, installationID int64, request *http.Request) (*http.Response, error) {
		if installationID != 1 || !strings.Contains(request.URL.String(), "/pulls/comments/3/reactions") {
			t.Fatalf("unexpected request: %d %s", installationID, request.URL)
		}
		body := `[{"id":9,"content":"+1","created_at":"2026-10-25T11:30:00Z","user":{"id":10}},{"id":11,"content":"heart","created_at":"2026-10-25T11:31:00Z","user":{"id":12}}]`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	if err := poller.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.feedback) != 1 || store.feedback[0].Kind != "thumbs_up" || store.next != now.Add(ReactionPollInterval) || store.done {
		t.Fatalf("stored poll = %+v %s %v", store.feedback, store.next, store.done)
	}
}

func TestReactionPollerStopsAfterFourteenDays(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	store := &reactionStoreStub{due: []ReactionComment{{InstallationID: 1, RepositoryID: 2, Owner: "acme", Repository: "app", CommentID: 3, ReviewCreatedAt: now.Add(-ReactionPollWindow)}}}
	poller := ReactionPoller{Store: store, Now: func() time.Time { return now }, Client: reactionClientFunc(func(context.Context, int64, *http.Request) (*http.Response, error) {
		t.Fatal("expired review must not call GitHub")
		return nil, nil
	})}
	if err := poller.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.done || !store.next.IsZero() {
		t.Fatalf("expired poll was rescheduled: %s %v", store.next, store.done)
	}
}
