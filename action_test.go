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
		"test -s \"$PX1_OUT/review.json\"",
		"test -s \"$PX1_OUT/checksums.sha256\"",
		"PX1_EXPLANATION_MODEL: ${{ inputs.explanation-model }}",
		"generate-explanations",
		"OPENAI_API_KEY",
		"PX1_EXPLANATION_EXCLUDE_GLOBS: ${{ inputs.explanation-exclude-globs }}",
		"--exclude-glob \"$pattern\"",
		"PX1_PRIVACY_CHECK: ${{ inputs.privacy-check }}",
		"privacy-check --review-file \"$PX1_OUT/review.json\"",
		"sha256sum --check checksums.sha256",
		"AI explanations send bounded, non-excluded pull-request diff content",
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

func TestFreeActionRemainsAccountlessAndSelfRun(t *testing.T) {
	body, err := os.ReadFile("action.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(body))
	for _, banned := range []string{
		"px1 account",
		"hosted-token",
		"account-token",
		"api.px1",
		"app.px1",
		"px1_backend",
	} {
		if strings.Contains(text, banned) {
			t.Fatalf("free Action acquired hosted dependency %q", banned)
		}
	}
	for _, want := range []string{
		"using: composite",
		"go run . \"${args[@]}\"",
		"root:\n    description: repository checkout to inspect.\n    required: false",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("free Action contract missing %q", want)
		}
	}
}

func TestReviewWorkflowAcceptsCanonicalAndLegacyAPISecretNames(t *testing.T) {
	body, err := os.ReadFile(".github/workflows/px1-review-report.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "secrets.OPENAI_API_KEY || secrets.OPENAI_API__KEY") {
		t.Fatal("review workflow must prefer the canonical API secret and accept the existing legacy name")
	}
}

func TestPrivateArtifactWorkflowAvoidsPagesAndRequiresRepositoryAccess(t *testing.T) {
	body, err := os.ReadFile("examples/px1-review-private.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"uses: actions/upload-artifact@v4",
		"artifact-url",
		"artifact-digest",
		"retention-days: 30",
		"Repository read access is required",
		"privacy-check: true",
		"checksums detect changes; they do not prove authorship",
	} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Errorf("private workflow missing %q", want)
		}
	}
	for _, banned := range []string{"pages: write", "actions/deploy-pages", "actions/upload-pages-artifact"} {
		if strings.Contains(text, banned) {
			t.Errorf("private workflow must not contain %q", banned)
		}
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

func TestReviewWorkflowGeneratesStructuredStickyComment(t *testing.T) {
	body, err := os.ReadFile(".github/workflows/px1-review-report.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"go run . summary-comment",
		`--review-file "site/reviews/${HEAD_SHA}/review.json"`,
		`--artifact-url "$report_url"`,
		"fs.readFileSync(process.env.COMMENT_FILE, 'utf8')",
		"body.startsWith(marker)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("review workflow missing %q", want)
		}
	}
}
