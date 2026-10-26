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

func TestValidateV2CoverageRequiresOneHundredSeededAndFortyCrossFile(t *testing.T) {
	dataset := Dataset{Version: 1}
	for i := 0; i < 100; i++ {
		labels := []Label{{Path: "api.go", BugClass: "seeded", StartLine: 4, EndLine: 4}}
		crossFile := i < 40
		if crossFile {
			labels = append(labels, Label{Path: "client.go", BugClass: "seeded", StartLine: 8, EndLine: 8})
		}
		repo := fmt.Sprintf("https://github.com/acme/repo-%d", i%10)
		dataset.Cases = append(dataset.Cases, Case{ID: fmt.Sprintf("seed-%d", i), Kind: "seeded", Repository: repo, PullRequestURL: repo + "/pull/1", BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MutationID: fmt.Sprintf("mutation-%d", i), CrossFile: crossFile, Labels: labels})
	}
	if err := dataset.ValidateV2Coverage(); err != nil {
		t.Fatal(err)
	}
	dataset.Cases[39].CrossFile = false
	if err := dataset.ValidateV2Coverage(); err == nil {
		t.Fatal("accepted fewer than forty cross-file cases")
	}
}

func TestValidateV2CoverageRejectsUnauditableCrossFileCase(t *testing.T) {
	dataset := Dataset{Version: 1}
	for i := 0; i < 100; i++ {
		repo := "https://github.com/acme/repo"
		labels := []Label{{Path: "same.go", BugClass: "seeded", StartLine: 1, EndLine: 1}, {Path: "same.go", BugClass: "seeded", StartLine: 2, EndLine: 2}}
		dataset.Cases = append(dataset.Cases, Case{ID: fmt.Sprintf("seed-%d", i), Kind: "seeded", Repository: repo, PullRequestURL: repo + "/pull/1", BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MutationID: fmt.Sprintf("mutation-%d", i), CrossFile: i < 40, Labels: labels})
	}
	if err := dataset.ValidateV2Coverage(); err == nil {
		t.Fatal("accepted a cross-file case with one labelled path")
	}
}
