package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	reviewImportVersion  = 1
	reviewImportMaxBytes = 16 << 20
	reviewImportTokenEnv = "PX1_IMPORT_TOKEN"
	reviewImportMaxFiles = 5000
)

var errReviewSnapshotConflict = errors.New("a different snapshot already exists for this repository, pull request, and head SHA")

// reviewSnapshotImport is the wire and persistence format shared with a
// self-hosted CI publisher. Identity is duplicated inside Snapshot on purpose:
// imports are rejected unless the routing envelope and rendered report agree.
type reviewSnapshotImport struct {
	Version     int                `json:"version"`
	Provider    string             `json:"provider"`
	Owner       string             `json:"owner"`
	Repository  string             `json:"repository"`
	PullRequest int                `json:"pullRequest"`
	Base        string             `json:"base"`
	Head        string             `json:"head"`
	Snapshot    staticReviewReport `json:"snapshot"`
}

type reviewSnapshotStore struct {
	root string
	mu   sync.RWMutex
}

func reviewSnapshotRoot() string {
	sessions := reviewStateRoot()
	if sessions == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessions), "review-snapshots")
}

func newReviewSnapshotStore(root string) *reviewSnapshotStore {
	return &reviewSnapshotStore{root: root}
}

func (s *reviewSnapshotStore) snapshotPath(owner, repository string, pullRequest int, head string) string {
	return filepath.Join(s.root, strings.ToLower(owner), strings.ToLower(repository), strconv.Itoa(pullRequest), head+".json")
}

// Save preserves the immutable-link contract. Repeating the same import is
// idempotent, while different content at the same repository/PR/head is a
// conflict rather than a silent replacement.
func (s *reviewSnapshotStore) Save(in reviewSnapshotImport) (bool, error) {
	if s == nil || s.root == "" {
		return false, errors.New("cannot determine px1 snapshot directory")
	}
	b, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return false, err
	}
	path := s.snapshotPath(in.Owner, in.Repository, in.PullRequest, in.Head)
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := os.ReadFile(path)
	if err == nil {
		if string(existing) == string(b) {
			return false, nil
		}
		return false, errReviewSnapshotConflict
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	created, err := writeImmutable(path, b, 0o600)
	if err == nil && created {
		return true, nil
	}
	if err != nil && !errors.Is(err, os.ErrExist) {
		return false, err
	}
	// Another px1 process may have won the exclusive publish after our first
	// read. Compare its complete file rather than replacing it.
	existing, err = os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if string(existing) == string(b) {
		return false, nil
	}
	return false, errReviewSnapshotConflict
}

// writeImmutable publishes a complete file only when its destination does not
// exist. Linking a fully written temporary file keeps the operation atomic
// across multiple px1 processes sharing the same state directory.
func writeImmutable(path string, b []byte, mode os.FileMode) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".px1-import-")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Chmod(mode)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err := os.Link(tmpName, path); err != nil {
		return false, err
	}
	return true, nil
}

func (s *reviewSnapshotStore) Load(target githubReviewTarget) (staticReviewReport, bool, error) {
	if s == nil || s.root == "" {
		return staticReviewReport{}, false, nil
	}
	path := s.snapshotPath(target.Owner, target.Repository, target.PullRequest, target.Head)
	s.mu.RLock()
	b, err := os.ReadFile(path)
	s.mu.RUnlock()
	if errors.Is(err, os.ErrNotExist) {
		return staticReviewReport{}, false, nil
	}
	if err != nil {
		return staticReviewReport{}, false, err
	}
	var imported reviewSnapshotImport
	if err := json.Unmarshal(b, &imported); err != nil {
		return staticReviewReport{}, false, fmt.Errorf("invalid stored snapshot: %w", err)
	}
	if err := validateReviewSnapshotImport(&imported); err != nil {
		return staticReviewReport{}, false, fmt.Errorf("invalid stored snapshot: %w", err)
	}
	if !strings.EqualFold(imported.Owner, target.Owner) || !strings.EqualFold(imported.Repository, target.Repository) || imported.PullRequest != target.PullRequest || imported.Head != target.Head {
		return staticReviewReport{}, false, errors.New("stored snapshot identity does not match review link")
	}
	return imported.Snapshot, true, nil
}

func (s *Server) handleReviewImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		fail(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if !authorizeReviewImport(w, r) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, reviewImportMaxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var imported reviewSnapshotImport
	if err := decoder.Decode(&imported); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("snapshot exceeds %d bytes", reviewImportMaxBytes))
			return
		}
		fail(w, http.StatusBadRequest, "invalid snapshot JSON: "+err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "request body must contain exactly one JSON object")
		return
	}
	if err := validateReviewSnapshotImport(&imported); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.snapshots.Save(imported)
	if err != nil {
		if errors.Is(err, errReviewSnapshotConflict) {
			fail(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, http.StatusInternalServerError, "store review snapshot: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if created {
		w.WriteHeader(http.StatusCreated)
	}
	writeJSON(w, map[string]any{
		"imported": created,
		"path": reviewLinkPath(githubReviewTarget{
			Provider: "github", Owner: imported.Owner, Repository: imported.Repository,
			PullRequest: imported.PullRequest, Head: imported.Head,
		}),
		"target": githubReviewTarget{
			Provider: "github", Owner: imported.Owner, Repository: imported.Repository,
			PullRequest: imported.PullRequest, Head: imported.Head,
		},
	})
}

func authorizeReviewImport(w http.ResponseWriter, r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv(reviewImportTokenEnv))
	if expected == "" {
		fail(w, http.StatusServiceUnavailable, reviewImportTokenEnv+" is not configured")
		return false
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) || !constantTimeStringEqual(strings.TrimSpace(strings.TrimPrefix(header, prefix)), expected) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		fail(w, http.StatusUnauthorized, "invalid import token")
		return false
	}
	return true
}

func constantTimeStringEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}

func validateReviewSnapshotImport(in *reviewSnapshotImport) error {
	if in.Version != reviewImportVersion {
		return fmt.Errorf("unsupported import version %d", in.Version)
	}
	if in.Provider != "github" {
		return errors.New("provider must be github")
	}
	if !validGitHubOwner(in.Owner) {
		return errors.New("import has an invalid GitHub owner")
	}
	if !validGitHubRepository(in.Repository) {
		return errors.New("import has an invalid GitHub repository")
	}
	if in.PullRequest <= 0 {
		return errors.New("import has an invalid pull request number")
	}
	in.Owner = strings.ToLower(in.Owner)
	in.Repository = strings.ToLower(in.Repository)
	in.Base = strings.ToLower(in.Base)
	in.Head = strings.ToLower(in.Head)
	if !validRevision(in.Base) || !validRevision(in.Head) {
		return errors.New("base and head must be full 40-character commit SHAs")
	}
	report := &in.Snapshot
	report.Repository = strings.ToLower(report.Repository)
	report.Base = strings.ToLower(report.Base)
	report.Head = strings.ToLower(report.Head)
	if report.Version != 1 {
		return fmt.Errorf("unsupported snapshot version %d", report.Version)
	}
	if !strings.EqualFold(report.Repository, in.Repository) {
		return errors.New("snapshot repository does not match import repository")
	}
	if report.Base != in.Base || report.Head != in.Head {
		return errors.New("snapshot base and head must match the import envelope")
	}
	if report.GeneratedAt.IsZero() {
		return errors.New("snapshot generatedAt is required")
	}
	if len(report.Files) > reviewImportMaxFiles {
		return fmt.Errorf("snapshot contains more than %d files", reviewImportMaxFiles)
	}
	changed := make(map[string]bool, len(report.Files))
	for i, file := range report.Files {
		if !safeExplanationPath(file.Path) {
			return fmt.Errorf("snapshot file %d has an invalid path", i+1)
		}
		if changed[file.Path] {
			return fmt.Errorf("snapshot file %q is duplicated", file.Path)
		}
		changed[file.Path] = true
		switch file.Status {
		case "A", "M", "D", "R", "C", "T":
		default:
			return fmt.Errorf("snapshot file %q has invalid status %q", file.Path, file.Status)
		}
	}
	if len(report.RuleHits) > ruleMaxHits {
		return fmt.Errorf("snapshot contains more than %d rule findings", ruleMaxHits)
	}
	seenHits := map[string]bool{}
	for i, hit := range report.RuleHits {
		if hit.Key == "" || seenHits[hit.Key] {
			return fmt.Errorf("snapshot rule finding %d must have a unique key", i+1)
		}
		seenHits[hit.Key] = true
		if !changed[hit.Path] || hit.Line < 1 || strings.TrimSpace(hit.RuleID) == "" || strings.TrimSpace(hit.Message) == "" {
			return fmt.Errorf("snapshot rule finding %q is invalid", hit.Key)
		}
	}
	explanations, err := validateStaticExplanations(report.Explanations, changed, "snapshot")
	if err != nil {
		return err
	}
	report.Explanations = explanations
	if report.Verification.Available {
		verification := verificationReport{Source: report.Verification.Source, Revision: report.Verification.Revision, URL: report.Verification.URL, Checks: report.Verification.Checks}
		if err := validateVerificationReport(verification, in.Head); err != nil {
			return fmt.Errorf("snapshot verification: %w", err)
		}
	} else if len(report.Verification.Checks) != 0 {
		return errors.New("unavailable snapshot verification cannot contain checks")
	}
	return nil
}
