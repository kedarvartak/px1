package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const reviewTargetElementID = "px1-review-target"

// githubReviewTarget is the immutable identity carried by an external review
// link. The eventual snapshot importer can use the same fields without making
// routing depend on mutable server state.
type githubReviewTarget struct {
	Provider    string `json:"provider"`
	Owner       string `json:"owner"`
	Repository  string `json:"repository"`
	PullRequest int    `json:"pullRequest"`
	Head        string `json:"head"`
}

func parseGitHubReviewTarget(path string, query url.Values) (*githubReviewTarget, error) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "github" || parts[3] != "pull" {
		return nil, errors.New("review link must match /github/<owner>/<repo>/pull/<number>")
	}
	if !validGitHubOwner(parts[1]) {
		return nil, errors.New("review link has an invalid GitHub owner")
	}
	if !validGitHubRepository(parts[2]) {
		return nil, errors.New("review link has an invalid GitHub repository")
	}
	pullRequest, err := strconv.Atoi(parts[4])
	if err != nil || pullRequest <= 0 {
		return nil, errors.New("review link has an invalid pull request number")
	}
	if len(query) != 1 {
		return nil, errors.New("review link only accepts one sha parameter")
	}
	shas, ok := query["sha"]
	if !ok || len(shas) != 1 {
		return nil, errors.New("review link requires exactly one sha parameter")
	}
	head := strings.ToLower(shas[0])
	if !validRevision(head) {
		return nil, errors.New("review link sha must be a full 40-character commit SHA")
	}
	return &githubReviewTarget{
		Provider:    "github",
		Owner:       parts[1],
		Repository:  parts[2],
		PullRequest: pullRequest,
		Head:        head,
	}, nil
}

func validGitHubOwner(owner string) bool {
	if len(owner) == 0 || len(owner) > 39 || owner[0] == '-' || owner[len(owner)-1] == '-' || strings.Contains(owner, "--") {
		return false
	}
	for _, r := range owner {
		if !asciiAlphaNumeric(r) && r != '-' {
			return false
		}
	}
	return true
}

func validGitHubRepository(repository string) bool {
	if len(repository) == 0 || len(repository) > 100 || repository == "." || repository == ".." {
		return false
	}
	for _, r := range repository {
		if !asciiAlphaNumeric(r) && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func asciiAlphaNumeric(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

func (s *Server) handleGitHubReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		fail(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	target, err := parseGitHubReviewTarget(r.URL.Path, r.URL.Query())
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.snapshots != nil {
		report, found, err := s.snapshots.Load(*target)
		if err != nil {
			fail(w, http.StatusInternalServerError, "load review snapshot: "+err.Error())
			return
		}
		if found {
			page, err := renderStaticReviewHTML(report)
			if err != nil {
				fail(w, http.StatusInternalServerError, "render review snapshot: "+err.Error())
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(page)
			return
		}
	}
	s.serveIndex(w, target)
}

func (s *Server) serveIndex(w http.ResponseWriter, target *githubReviewTarget) {
	b, err := fs.ReadFile(assets, "web/index.html")
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if target != nil {
		encoded, err := json.Marshal(target)
		if err != nil {
			fail(w, http.StatusInternalServerError, fmt.Sprintf("encode review target: %v", err))
			return
		}
		marker := []byte("</head>")
		if !bytes.Contains(b, marker) {
			fail(w, http.StatusInternalServerError, "index is missing its closing head element")
			return
		}
		script := append([]byte(`<script type="application/json" id="`+reviewTargetElementID+`">`), encoded...)
		script = append(script, []byte("</script>\n</head>")...)
		b = bytes.Replace(b, marker, script, 1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}
