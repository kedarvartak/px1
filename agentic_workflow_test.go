package main

import (
	"os"
	"strings"
	"testing"
)

// The dogfood workflow gives a pull request's code no path to the API key.
func TestAgenticDogfoodWorkflowKeepsTheKeyAwayFromPullRequestCode(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/px1-agentic-review.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"pull_request:",
		"github.event.pull_request.head.repo.full_name == github.repository", // never forks
		"ref: ${{ github.event.pull_request.base.sha }}",                     // reviewer built from the base commit
		"uses: ./.px1-tool/agentic-action",                                   // and that is the copy that runs
		"persist-credentials: false",
		"pull-requests: write",
		"continue-on-error: true",
		"OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("workflow lacks %q", want)
		}
	}
	for _, banned := range []string{"pull_request_target", "uses: ./agentic-action", "contents: write", "OPENAI_API_KEY: sk-"} {
		if strings.Contains(text, banned) {
			t.Errorf("workflow must not contain %q", banned)
		}
	}
	if n := strings.Count(text, "uses: ./.px1-tool/agentic-action"); n != 1 {
		t.Errorf("the reviewer must run exactly once from the base checkout, found %d", n)
	}
}
