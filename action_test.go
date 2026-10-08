package main

import (
	"os"
	"strings"
	"testing"
)

func TestArtifactActionKeepsRevisionInputsExplicit(t *testing.T) {
	body, err := os.ReadFile("action.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"PX1_BASE: ${{ inputs.base }}",
		"PX1_HEAD: ${{ inputs.head }}",
		"--base \"$PX1_BASE\"",
		"--head \"$PX1_HEAD\"",
		"go run . \"${args[@]}\"",
		"test -s \"$PX1_OUT/index.html\"",
		"PX1_EXPLANATION_MODEL: ${{ inputs.explanation-model }}",
		"generate-explanations",
		"OPENAI_API_KEY",
		"PX1_EXPLANATION_EXCLUDE_GLOBS: ${{ inputs.explanation-exclude-globs }}",
		"--exclude-glob \"$pattern\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("action.yml missing %q", want)
		}
	}
	if strings.Contains(text, "github.event.pull_request") {
		t.Fatal("the reusable action must receive explicit revisions from its caller")
	}
	if strings.Contains(text, "openai-api-key") {
		t.Fatal("API keys must come from the caller environment, not action inputs")
	}
}

func TestArtifactHistoryActionUsesValidatedHistoryScript(t *testing.T) {
	body, err := os.ReadFile("history/action.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"PX1_HISTORY_MODE: ${{ inputs.mode }}",
		"PX1_REVIEW_HISTORY_BRANCH: ${{ inputs.branch }}",
		`"$PX1_HISTORY_MODE" "$PX1_HISTORY_SITE"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("history/action.yml missing %q", want)
		}
	}
}

func TestActionReleaseKeepsImmutableAndMajorTagsSeparate(t *testing.T) {
	body, err := os.ReadFile(".github/workflows/release-actions.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		`^v[1-9][0-9]*\.[0-9]+\.[0-9]+$`,
		`immutable tag ${RELEASE_VERSION} already exists`,
		`git push origin "refs/tags/${RELEASE_TAG}"`,
		`git push --force origin "refs/tags/${MAJOR_TAG}"`,
		`gh release create "$RELEASE_TAG" --verify-tag`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("release workflow missing %q", want)
		}
	}
}
