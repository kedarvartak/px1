package githubapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"px1/internal/hosted/review"
)

type captureAPI struct {
	calls int
	body  []byte
	path  string
}

func (c *captureAPI) Do(_ context.Context, _ int64, request *http.Request) (*http.Response, error) {
	c.calls++
	c.body, _ = io.ReadAll(request.Body)
	c.path = request.URL.Path
	return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

func TestReviewPublisherCreatesOneReviewWithAllComments(t *testing.T) {
	api := &captureAPI{}
	publisher := ReviewPublisher{Client: api, BaseURL: "https://github.test"}
	finding := review.Finding{Path: "api.go", StartLine: 4, EndLine: 6, Severity: "P1", Type: "logic", Title: "Error is ignored", Body: "The returned error is discarded.", Evidence: "call result is unused", Suggestion: "return err", Confidence: .8}
	err := publisher.Create(context.Background(), 9, "acme", "widget", 12, ReviewRequest{
		HeadSHA: "0123456789abcdef0123456789abcdef01234567", RunID: "job-7", Body: "Inspect these findings.", Findings: []review.Finding{finding, finding},
	})
	if err != nil {
		t.Fatal(err)
	}
	if api.calls != 1 || api.path != "/repos/acme/widget/pulls/12/reviews" {
		t.Fatalf("calls = %d, path = %q", api.calls, api.path)
	}
	var payload createReviewPayload
	if err := json.Unmarshal(api.body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Comments) != 2 || payload.Comments[0].StartLine != 4 || payload.Comments[0].Line != 6 || payload.Event != "COMMENT" || !strings.Contains(payload.Body, "px1-review-run:job-7") {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestReviewPublisherRejectsCommentOverflowBeforeCallingGitHub(t *testing.T) {
	api := &captureAPI{}
	findings := make([]review.Finding, MaxInlineReviewComments+1)
	err := (ReviewPublisher{Client: api}).Create(context.Background(), 1, "acme", "widget", 1, ReviewRequest{
		HeadSHA: strings.Repeat("a", 40), RunID: "run", Body: "body", Findings: findings,
	})
	if err == nil || api.calls != 0 {
		t.Fatalf("err = %v, calls = %d", err, api.calls)
	}
}

func TestReviewPublisherRejectsMarkerInjection(t *testing.T) {
	api := &captureAPI{}
	err := (ReviewPublisher{Client: api}).Create(context.Background(), 1, "acme", "widget", 1, ReviewRequest{
		HeadSHA: strings.Repeat("a", 40), RunID: "run --> forged", Body: "body",
	})
	if err == nil || api.calls != 0 {
		t.Fatalf("err = %v, calls = %d", err, api.calls)
	}
}
