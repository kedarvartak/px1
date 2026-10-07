package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Verification data is imported from GitHub Actions and tied to the exact PR
// head. The report generator never runs repository commands.
type verificationCheck struct {
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Summary    string     `json:"summary,omitempty"`
	URL        string     `json:"url,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type verificationReport struct {
	Source   string              `json:"source"`
	Revision string              `json:"revision"`
	URL      string              `json:"url,omitempty"`
	Checks   []verificationCheck `json:"checks"`
}

type verificationResponse struct {
	Available bool                `json:"available"`
	Source    string              `json:"source,omitempty"`
	Revision  string              `json:"revision,omitempty"`
	URL       string              `json:"url,omitempty"`
	Checks    []verificationCheck `json:"checks"`
	Error     string              `json:"error,omitempty"`
}

func nonNilChecks(checks []verificationCheck) []verificationCheck {
	if checks == nil {
		return []verificationCheck{}
	}
	return checks
}

func validateVerificationReport(report verificationReport, head string) error {
	if report.Source != "github-actions" {
		return errors.New("verification report must come from github-actions")
	}
	if !validRevision(report.Revision) {
		return errors.New("verification report has an invalid revision")
	}
	if head == "" {
		return errors.New("verification report cannot be matched without a revision")
	}
	if report.Revision != head {
		return fmt.Errorf("verification report is for %s, but this review is for %s", shortRevision(report.Revision), shortRevision(head))
	}
	if err := validateHTTPSURL(report.URL); err != nil {
		return fmt.Errorf("verification report URL: %w", err)
	}
	seen := map[string]bool{}
	for _, check := range report.Checks {
		if strings.TrimSpace(check.Name) == "" {
			return errors.New("verification check name cannot be empty")
		}
		if seen[check.Name] {
			return fmt.Errorf("verification check %q is duplicated", check.Name)
		}
		seen[check.Name] = true
		switch check.Status {
		case "queued", "running", "passed", "failed", "cancelled":
		default:
			return fmt.Errorf("verification check %q has unknown status %q", check.Name, check.Status)
		}
		if err := validateHTTPSURL(check.URL); err != nil {
			return fmt.Errorf("verification check %q URL: %w", check.Name, err)
		}
	}
	return nil
}

func validateHTTPSURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("must be an HTTPS URL")
	}
	return nil
}

func validRevision(revision string) bool {
	if len(revision) != 40 {
		return false
	}
	_, err := hex.DecodeString(revision)
	return err == nil
}

func shortRevision(revision string) string {
	if len(revision) > 8 {
		return revision[:8]
	}
	return revision
}
