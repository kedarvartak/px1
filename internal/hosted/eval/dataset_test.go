package eval

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateV1CoverageRequiresRoadmapMinimums(t *testing.T) {
	dataset := Dataset{Version: 1}
	for i := 0; i < 70; i++ {
		kind := "historical"
		mutation, fix := "", strings.Repeat("c", 40)
		if i >= 50 {
			kind, mutation, fix = "seeded", fmt.Sprintf("mutation-%d", i), ""
		}
		repo := fmt.Sprintf("https://github.com/acme/repo-%d", i%10)
		dataset.Cases = append(dataset.Cases, Case{ID: fmt.Sprintf("case-%d", i), Kind: kind, Repository: repo, PullRequestURL: repo + "/pull/1", BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), BugFixSHA: fix, MutationID: mutation, Labels: []Label{{Path: "api.go", BugClass: "wrong-error-handling", StartLine: 4, EndLine: 4}}})
	}
	if err := dataset.ValidateV1Coverage(); err != nil {
		t.Fatal(err)
	}
	dataset.Cases = dataset.Cases[:69]
	if err := dataset.ValidateV1Coverage(); err == nil {
		t.Fatal("accepted undersized dataset")
	}
}

func TestDecodeRejectsUnauditableCase(t *testing.T) {
	_, err := Decode([]byte(`{"version":1,"cases":[{"ID":"x"}]}`))
	if err == nil {
		t.Fatal("accepted unauditable case")
	}
}
