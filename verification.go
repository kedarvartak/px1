package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// verificationCheck is a result published by CI. px1 only displays these
// results; it never executes the command that produced them.
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

type verificationManager struct {
	mu   sync.RWMutex
	root string
}

type verificationResponse struct {
	Available bool                `json:"available"`
	Source    string              `json:"source,omitempty"`
	Revision  string              `json:"revision,omitempty"`
	URL       string              `json:"url,omitempty"`
	Checks    []verificationCheck `json:"checks"`
	Error     string              `json:"error,omitempty"`
}

// SetRoot points the reader at another checkout.
func (m *verificationManager) SetRoot(root string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.root = root
}

func newVerificationManager(root string) *verificationManager {
	return &verificationManager{root: root}
}

// Load reads the small file written by the CI integration. Keeping this as a
// file contract makes the first integration self-hostable: a GitHub Action can
// write the report into the checkout before px1 opens it.
func (m *verificationManager) Load(head string) (*verificationResponse, error) {
	m.mu.RLock()
	root := m.root
	m.mu.RUnlock()

	path := filepath.Join(root, ".px1", "verification.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &verificationResponse{Available: false, Checks: []verificationCheck{}}, nil
	}
	if err != nil {
		return nil, err
	}

	var report verificationReport
	if err := json.Unmarshal(b, &report); err != nil {
		return &verificationResponse{
			Available: false,
			Checks:    []verificationCheck{},
			Error:     fmt.Sprintf("invalid %s: %v", filepath.ToSlash(filepath.Join(".px1", "verification.json")), err),
		}, nil
	}
	if err := validateVerificationReport(report, head); err != nil {
		return &verificationResponse{Available: false, Revision: report.Revision, URL: report.URL, Checks: []verificationCheck{}, Error: err.Error()}, nil
	}
	return &verificationResponse{
		Available: true,
		Source:    report.Source,
		Revision:  report.Revision,
		URL:       report.URL,
		Checks:    nonNilChecks(report.Checks),
	}, nil
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
		return errors.New("verification report cannot be matched without an active git revision")
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

func (s *Server) handleReviewChecks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	active, _ := s.review.Active()
	if active == nil {
		writeJSON(w, &verificationResponse{Available: false, Checks: []verificationCheck{}})
		return
	}
	report, err := s.verify.Load(active.Head)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, report)
}
