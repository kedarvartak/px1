package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
)

type privacyRisk struct {
	Kind   string
	Path   string
	Reason string
}

var highConfidenceSecretPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"private key", regexp.MustCompile(`-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE KEY-----`)},
	{"OpenAI API key", regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}\b`)},
	{"GitHub token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)},
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{20,}\b`)},
	{"npm access token", regexp.MustCompile(`\bnpm_[A-Za-z0-9]{20,}\b`)},
}

func runPrivacyCheck(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("privacy-check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	reviewFile := fs.String("review-file", "", "path to generated review.json (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*reviewFile) == "" {
		return errors.New("privacy-check requires --review-file")
	}
	b, err := os.ReadFile(*reviewFile)
	if err != nil {
		return fmt.Errorf("read review file: %w", err)
	}
	var report staticReviewReport
	if err := json.Unmarshal(b, &report); err != nil {
		return fmt.Errorf("decode review file: %w", err)
	}
	risks := findPrivacyRisks(report, b)
	if len(risks) > 0 {
		parts := make([]string, 0, len(risks))
		for _, risk := range risks {
			if risk.Path == "" {
				parts = append(parts, risk.Reason)
			} else {
				parts = append(parts, fmt.Sprintf("%s (%s)", risk.Reason, risk.Path))
			}
		}
		return fmt.Errorf("privacy check blocked report: likely sensitive content: %s; inspect the source before delivery. This check is detection, not redaction or access control", strings.Join(parts, ", "))
	}
	fmt.Fprintln(stdout, "privacy check passed: no high-confidence secret signatures or sensitive paths detected")
	fmt.Fprintln(stdout, "privacy check is not redaction or access control; choose delivery visibility deliberately")
	return nil
}

func findPrivacyRisks(report staticReviewReport, encoded []byte) []privacyRisk {
	seen := map[string]bool{}
	risks := []privacyRisk{}
	add := func(risk privacyRisk) {
		key := risk.Kind + "\x00" + risk.Path
		if seen[key] {
			return
		}
		seen[key] = true
		risks = append(risks, risk)
	}
	for _, file := range report.Files {
		if reason := sensitiveReportPath(file.Path); reason != "" {
			add(privacyRisk{Kind: "path", Path: file.Path, Reason: reason})
		}
	}
	for _, candidate := range highConfidenceSecretPatterns {
		if candidate.pattern.Match(encoded) {
			add(privacyRisk{Kind: candidate.name, Reason: candidate.name + " signature"})
		}
	}
	sort.Slice(risks, func(i, j int) bool {
		if risks[i].Path != risks[j].Path {
			return risks[i].Path < risks[j].Path
		}
		return risks[i].Kind < risks[j].Kind
	})
	return risks
}

func sensitiveReportPath(value string) string {
	clean := strings.ToLower(strings.TrimSpace(value))
	base := pathpkg.Base(clean)
	if base == ".env" || (strings.HasPrefix(base, ".env.") && !strings.HasSuffix(base, ".example") && !strings.HasSuffix(base, ".sample") && !strings.HasSuffix(base, ".template")) {
		return "environment-secret path"
	}
	switch base {
	case ".npmrc", ".pypirc", ".netrc", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "credentials.json", "service-account.json":
		return "credential-bearing path"
	}
	switch pathpkg.Ext(base) {
	case ".pem", ".key", ".p12", ".pfx", ".jks":
		return "private-key or certificate path"
	}
	return ""
}
