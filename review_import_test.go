package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testImportToken = "px1-test-import-token-with-enough-entropy"
const testBaseSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func testReviewImport() reviewSnapshotImport {
	return reviewSnapshotImport{
		Version:     1,
		Provider:    "github",
		Owner:       "acme",
		Repository:  "widgets",
		PullRequest: 42,
		Base:        testBaseSHA,
		Head:        testReviewSHA,
		Snapshot: staticReviewReport{
			Version:      1,
			Repository:   "widgets",
			Base:         testBaseSHA,
			Head:         testReviewSHA,
			GeneratedAt:  time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
			Files:        []staticReviewFile{{Path: "main.go", Status: "M", Diff: "diff --git a/main.go b/main.go\n"}},
			RuleHits:     []staticReviewHit{},
			Explanations: []staticExplanation{},
			Verification: verificationResponse{Available: false, Checks: []verificationCheck{}},
		},
	}
}

func newImportTestServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(reviewImportTokenEnv, testImportToken)
	s, _ := newTestServer(t)
	return s
}

func sendReviewImport(t *testing.T, s *Server, body []byte, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/review/import", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestReviewSnapshotImportPersistsAndServesPinnedReport(t *testing.T) {
	s := newImportTestServer(t)
	payload, err := json.Marshal(testReviewImport())
	if err != nil {
		t.Fatal(err)
	}
	w := sendReviewImport(t, s, payload, testImportToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("first import status = %d, body = %s", w.Code, w.Body.String())
	}
	var response struct {
		Imported bool   `json:"imported"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Imported || response.Path != "/github/acme/widgets/pull/42?sha="+testReviewSHA {
		t.Fatalf("response = %#v", response)
	}

	// Retrying the exact CI delivery is safe and does not rewrite the snapshot.
	w = sendReviewImport(t, s, payload, testImportToken)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"imported":false`) {
		t.Fatalf("repeat import status = %d, body = %s", w.Code, w.Body.String())
	}

	// A fresh server process discovers the persisted snapshot from px1 state.
	restarted, _ := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, response.Path, nil)
	w = httptest.NewRecorder()
	restarted.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("review route status = %d, body = %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "Review report") || !strings.Contains(body, `"repository":"widgets"`) || strings.Contains(body, reviewTargetElementID) {
		t.Fatalf("review route did not serve the imported report: %s", body)
	}
}

func TestReviewSnapshotImportRequiresConfiguredBearerToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	payload, err := json.Marshal(testReviewImport())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(reviewImportTokenEnv, "")
	s, _ := newTestServer(t)
	if w := sendReviewImport(t, s, payload, testImportToken); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured status = %d, want 503", w.Code)
	}
	t.Setenv(reviewImportTokenEnv, testImportToken)
	if w := sendReviewImport(t, s, payload, "wrong-token"); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d, want 401", w.Code)
	}
	if w := sendReviewImport(t, s, payload, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", w.Code)
	}
}

func TestReviewSnapshotImportRejectsInvalidAndConflictingPayloads(t *testing.T) {
	s := newImportTestServer(t)
	tests := []struct {
		name   string
		mutate func(*reviewSnapshotImport)
	}{
		{"unsupported version", func(in *reviewSnapshotImport) { in.Version = 2 }},
		{"invalid owner", func(in *reviewSnapshotImport) { in.Owner = "../acme" }},
		{"short base", func(in *reviewSnapshotImport) { in.Base = "abc" }},
		{"head mismatch", func(in *reviewSnapshotImport) { in.Snapshot.Head = testBaseSHA }},
		{"repository mismatch", func(in *reviewSnapshotImport) { in.Snapshot.Repository = "other" }},
		{"unsafe file path", func(in *reviewSnapshotImport) { in.Snapshot.Files[0].Path = "../main.go" }},
		{"duplicate file", func(in *reviewSnapshotImport) { in.Snapshot.Files = append(in.Snapshot.Files, in.Snapshot.Files[0]) }},
		{"invalid status", func(in *reviewSnapshotImport) { in.Snapshot.Files[0].Status = "X" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := testReviewImport()
			tt.mutate(&in)
			payload, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			if w := sendReviewImport(t, s, payload, testImportToken); w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
		})
	}

	unknown := []byte(`{"version":1,"unexpected":true}`)
	if w := sendReviewImport(t, s, unknown, testImportToken); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, body = %s", w.Code, w.Body.String())
	}

	valid := testReviewImport()
	payload, _ := json.Marshal(valid)
	if w := sendReviewImport(t, s, payload, testImportToken); w.Code != http.StatusCreated {
		t.Fatalf("initial status = %d, body = %s", w.Code, w.Body.String())
	}
	valid.Snapshot.GeneratedAt = valid.Snapshot.GeneratedAt.Add(time.Minute)
	changed, _ := json.Marshal(valid)
	if w := sendReviewImport(t, s, changed, testImportToken); w.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestReviewSnapshotImportRequiresJSON(t *testing.T) {
	s := newImportTestServer(t)
	r := httptest.NewRequest(http.MethodPost, "/api/review/import", strings.NewReader("{}"))
	r.Header.Set("Authorization", "Bearer "+testImportToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestReviewSnapshotStoreIsImmutableAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	firstStore := newReviewSnapshotStore(root)
	secondStore := newReviewSnapshotStore(root)
	first := testReviewImport()
	second := testReviewImport()
	second.Snapshot.GeneratedAt = second.Snapshot.GeneratedAt.Add(time.Minute)

	start := make(chan struct{})
	type result struct {
		created bool
		err     error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, candidate := range []reviewSnapshotImport{first, second} {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			created, err := newReviewSnapshotStore(root).Save(candidate)
			results <- result{created: created, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	created, conflicts := 0, 0
	for result := range results {
		if result.created {
			created++
		}
		if errors.Is(result.err, errReviewSnapshotConflict) {
			conflicts++
		} else if result.err != nil {
			t.Fatal(result.err)
		}
	}
	if created != 1 || conflicts != 1 {
		t.Fatalf("created = %d, conflicts = %d; want one of each", created, conflicts)
	}

	target := githubReviewTarget{Owner: "ACME", Repository: "Widgets", PullRequest: 42, Head: testReviewSHA}
	if _, found, err := firstStore.Load(target); err != nil || !found {
		t.Fatalf("first store load: found=%v err=%v", found, err)
	}
	if _, found, err := secondStore.Load(target); err != nil || !found {
		t.Fatalf("second store load: found=%v err=%v", found, err)
	}
}
