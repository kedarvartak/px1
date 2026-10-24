package githubapp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type summaryAPIFake struct {
	responses []*http.Response
	methods   []string
	paths     []string
}

func (f *summaryAPIFake) Do(_ context.Context, _ int64, request *http.Request) (*http.Response, error) {
	f.methods = append(f.methods, request.Method)
	f.paths = append(f.paths, request.URL.Path)
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func summaryResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestStickySummaryUpdatesBotOwnedMarker(t *testing.T) {
	api := &summaryAPIFake{responses: []*http.Response{
		summaryResponse(200, `[{"id":4,"body":"<!-- px1-static-review -->","user":{"login":"px1[bot]","type":"Bot"}}]`),
		summaryResponse(200, `{}`),
	}}
	action, err := (StickySummaryPublisher{Client: api, BaseURL: "https://github.test"}).Upsert(context.Background(), 1, "acme", "widget", 7, "px1[bot]", StaticReviewCommentMarker+"\nsummary")
	if err != nil || action != "updated" {
		t.Fatalf("action = %q, err = %v", action, err)
	}
	if len(api.methods) != 2 || api.methods[1] != http.MethodPatch || api.paths[1] != "/repos/acme/widget/issues/comments/4" {
		t.Fatalf("methods = %#v, paths = %#v", api.methods, api.paths)
	}
}

func TestStickySummaryIgnoresUserOwnedMarker(t *testing.T) {
	api := &summaryAPIFake{responses: []*http.Response{
		summaryResponse(200, `[{"id":4,"body":"<!-- px1-static-review -->","user":{"login":"attacker","type":"User"}}]`),
		summaryResponse(201, `{}`),
	}}
	action, err := (StickySummaryPublisher{Client: api}).Upsert(context.Background(), 1, "acme", "widget", 7, "px1[bot]", StaticReviewCommentMarker+"\nsummary")
	if err != nil || action != "created" || api.methods[1] != http.MethodPost {
		t.Fatalf("action = %q, err = %v, methods = %#v", action, err, api.methods)
	}
}

func TestStickySummaryRequiresMarker(t *testing.T) {
	api := &summaryAPIFake{}
	_, err := (StickySummaryPublisher{Client: api}).Upsert(context.Background(), 1, "acme", "widget", 7, "px1[bot]", "summary")
	if err == nil || len(api.methods) != 0 {
		t.Fatalf("err = %v, methods = %#v", err, api.methods)
	}
}
