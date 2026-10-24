package review

import (
	"strings"
	"testing"
)

func TestBuildSummaryPromptExcludesDeterministicSignals(t *testing.T) {
	prompt := BuildSummaryPrompt(3, []Finding{{Severity: "P1", Path: "api.go", StartLine: 4, EndLine: 5, Title: "Unchecked error"}}, true)
	for _, want := range []string{"3-6 sentence", "P1 api.go:4-5", "Repository index partial: true"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestRenderSummaryKeepsSignalsVerbatim(t *testing.T) {
	detail := "CI status: failed\nexact command: go test ./...\n``` embedded fence"
	body, err := RenderSummary(SummaryInput{
		HeadSHA: "0123456789abcdef0123456789abcdef01234567", ChangedFiles: 2,
		ReportURL: "https://example.test/report", ModelSummary: "This change adjusts request handling.",
		DeterministicSignals: []DeterministicSignal{{Label: "CI", Detail: detail}}, Verification: "1 failed",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{HostedSummaryMarker, detail, "1 failed", "commit-pinned review report"} {
		if !strings.Contains(body, want) {
			t.Fatalf("summary missing %q:\n%s", want, body)
		}
	}
}

func TestRenderSummaryRejectsMutableIdentity(t *testing.T) {
	_, err := RenderSummary(SummaryInput{HeadSHA: "main", ReportURL: "https://example.test"})
	if err == nil {
		t.Fatal("expected invalid SHA to fail")
	}
}
