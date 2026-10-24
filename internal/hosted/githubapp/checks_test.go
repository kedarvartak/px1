package githubapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type checkAPIFake struct {
	methods []string
	paths   []string
	bodies  [][]byte
}

func (f *checkAPIFake) Do(_ context.Context, _ int64, request *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(request.Body)
	f.methods = append(f.methods, request.Method)
	f.paths = append(f.paths, request.URL.Path)
	f.bodies = append(f.bodies, body)
	response := `{}`
	status := http.StatusOK
	if request.Method == http.MethodPost {
		response = `{"id":42}`
		status = http.StatusCreated
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(response))}, nil
}

func TestCheckRunLifecycleIncludesRetryAction(t *testing.T) {
	api := &checkAPIFake{}
	publisher := CheckRunPublisher{Client: api, BaseURL: "https://github.test", Now: func() time.Time { return time.Unix(10, 0) }}
	id, err := publisher.Start(context.Background(), 7, "acme", "widget", CheckRunStart{
		HeadSHA: strings.Repeat("a", 40), RunID: "run-9", DetailsURL: "https://px1.test/reviews/run-9", Summary: "Queued",
	})
	if err != nil || id != 42 {
		t.Fatalf("id = %d, err = %v", id, err)
	}
	if err := publisher.Complete(context.Background(), 7, "acme", "widget", id, CheckRunResult{Conclusion: "neutral", Title: "Review ready", Summary: "Inspect the findings."}); err != nil {
		t.Fatal(err)
	}
	if len(api.methods) != 2 || api.methods[0] != http.MethodPost || api.methods[1] != http.MethodPatch || api.paths[1] != "/repos/acme/widget/check-runs/42" {
		t.Fatalf("methods = %#v, paths = %#v", api.methods, api.paths)
	}
	var completed map[string]any
	if err := json.Unmarshal(api.bodies[1], &completed); err != nil {
		t.Fatal(err)
	}
	actions := completed["actions"].([]any)
	if actions[0].(map[string]any)["identifier"] != RetryReviewAction || completed["conclusion"] != "neutral" {
		t.Fatalf("completed payload = %#v", completed)
	}
}

func TestReviewConclusionIsConfigurable(t *testing.T) {
	if ReviewConclusion(true, true) != "failure" || ReviewConclusion(true, false) != "neutral" || ReviewConclusion(false, true) != "neutral" {
		t.Fatal("unexpected conclusion policy")
	}
}

func TestCheckRunRejectsInvalidHeadBeforePublishing(t *testing.T) {
	api := &checkAPIFake{}
	_, err := (CheckRunPublisher{Client: api}).Start(context.Background(), 1, "acme", "widget", CheckRunStart{HeadSHA: "main", RunID: "run", DetailsURL: "https://px1.test"})
	if err == nil || len(api.methods) != 0 {
		t.Fatalf("err = %v, calls = %d", err, len(api.methods))
	}
}
