package review

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const HostedSummaryMarker = "<!-- px1-hosted-review -->"

type SummaryInput struct {
	HeadSHA              string
	ChangedFiles         int
	PartialIndex         bool
	Verification         string
	ReportURL            string
	ModelSummary         string
	Findings             []Finding
	DeterministicSignals []DeterministicSignal
	BlastRadius          []BlastRadiusItem
	Confidence           *ConfidenceScore
}

type DeterministicSignal struct {
	Label  string
	Detail string
}

// BuildSummaryPrompt asks the model only for a compact orientation paragraph.
// Deterministic signals are intentionally excluded and rendered separately so
// model prose cannot silently alter their meaning.
func BuildSummaryPrompt(changedFiles int, findings []Finding, partialIndex bool) string {
	var prompt strings.Builder
	fmt.Fprintln(&prompt, "Write a neutral 3-6 sentence orientation to this pull request review.")
	fmt.Fprintln(&prompt, "Do not approve the change, claim safety, or invent evidence.")
	fmt.Fprintf(&prompt, "Changed files: %d\nRepository index partial: %t\n", changedFiles, partialIndex)
	fmt.Fprintln(&prompt, "Verified findings:")
	for _, finding := range findings {
		fmt.Fprintf(&prompt, "- %s %s:%d-%d: %s\n", finding.Severity, finding.Path, finding.StartLine, finding.EndLine, finding.Title)
	}
	return prompt.String()
}

func RenderSummary(input SummaryInput) (string, error) {
	if len(input.HeadSHA) != 40 || !isHex(input.HeadSHA) {
		return "", errors.New("head SHA must be a full commit hash")
	}
	parsed, err := url.Parse(strings.TrimSpace(input.ReportURL))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", errors.New("report URL must be absolute HTTP(S)")
	}

	var body strings.Builder
	fmt.Fprintln(&body, HostedSummaryMarker)
	fmt.Fprintln(&body, "## px1 hosted review")
	fmt.Fprintf(&body, "\nReview for commit `%s`. %d changed files; %d verified findings.\n", input.HeadSHA, input.ChangedFiles, len(input.Findings))
	if input.PartialIndex {
		fmt.Fprintln(&body, "\n> Repository context was partial because the index limit was reached.")
	}
	if summary := strings.TrimSpace(input.ModelSummary); summary != "" {
		fmt.Fprintf(&body, "\n%s\n", summary)
	}
	if input.Confidence != nil {
		if err := input.Confidence.Validate(); err != nil {
			return "", err
		}
		fmt.Fprintf(&body, "\n### Review confidence: %d/5\n", input.Confidence.Value)
		fmt.Fprintln(&body, "This deterministic score summarizes available evidence; it is not approval or proof of correctness.")
		for _, reason := range input.Confidence.Reasons {
			fmt.Fprintf(&body, "- `%s` (%+d): %s\n", reason.Input, reason.Impact, reason.Detail)
		}
	}
	if len(input.Findings) > 0 {
		fmt.Fprintln(&body, "\n### Findings")
		for _, finding := range input.Findings {
			fmt.Fprintf(&body, "- **%s** `%s:%d-%d` — %s\n", finding.Severity, finding.Path, finding.StartLine, finding.EndLine, finding.Title)
		}
	}
	if len(input.DeterministicSignals) > 0 {
		fmt.Fprintln(&body, "\n### Deterministic signals (verbatim)")
		for _, signal := range input.DeterministicSignals {
			fmt.Fprintf(&body, "\n**%s**\n\n", signal.Label)
			writeVerbatim(&body, signal.Detail)
		}
	}
	if len(input.BlastRadius) > 0 {
		fmt.Fprintln(&body, "\n### Blast radius (unchanged related code)")
		fmt.Fprintln(&body, "These relationships are inspection pointers, not claims that the files are broken.")
		for _, item := range input.BlastRadius {
			fmt.Fprintf(&body, "- **%s** `%s` — %s\n", item.Kind, item.Path, item.Name)
		}
	}
	verification := strings.TrimSpace(input.Verification)
	if verification == "" {
		verification = "not available"
	}
	fmt.Fprintf(&body, "\n**Verification:** %s\n\n[Open the commit-pinned review report](%s)\n", verification, parsed.String())
	return body.String(), nil
}

func writeVerbatim(body *strings.Builder, value string) {
	fence := "```"
	for strings.Contains(value, fence) {
		fence += "`"
	}
	fmt.Fprintf(body, "%s\n%s\n%s\n", fence, value, fence)
}

func isHex(value string) bool {
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
