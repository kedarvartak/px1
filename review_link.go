package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const reviewTargetElementID = "px1-review-target"

const reviewLinkSecretEnv = "PX1_REVIEW_LINK_SECRET"

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
	if len(query) < 1 || len(query) > 2 {
		return nil, errors.New("review link only accepts sha and optional sig parameters")
	}
	for key := range query {
		if key != "sha" && key != "sig" {
			return nil, errors.New("review link only accepts sha and optional sig parameters")
		}
	}
	shas, ok := query["sha"]
	if !ok || len(shas) != 1 {
		return nil, errors.New("review link requires exactly one sha parameter")
	}
	head := strings.ToLower(shas[0])
	if !validRevision(head) {
		return nil, errors.New("review link sha must be a full 40-character commit SHA")
	}
	if sig, ok := query["sig"]; ok {
		if len(sig) != 1 || !validReviewLinkSignature(sig[0]) {
			return nil, errors.New("review link signature is invalid")
		}
	}
	return &githubReviewTarget{
		Provider:    "github",
		Owner:       parts[1],
		Repository:  parts[2],
		PullRequest: pullRequest,
		Head:        head,
	}, nil
}

func validReviewLinkSignature(sig string) bool {
	if len(sig) != sha256.Size*2 {
		return false
	}
	for _, r := range sig {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func reviewLinkSignature(target githubReviewTarget, secret string) string {
	message := strings.Join([]string{
		strings.ToLower(target.Provider),
		strings.ToLower(target.Owner),
		strings.ToLower(target.Repository),
		strconv.Itoa(target.PullRequest),
		strings.ToLower(target.Head),
	}, "\n")
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write([]byte(message))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func reviewLinkSecret() string {
	return strings.TrimSpace(os.Getenv(reviewLinkSecretEnv))
}

func authorizeReviewLink(w http.ResponseWriter, r *http.Request, target githubReviewTarget) bool {
	secret := reviewLinkSecret()
	if secret == "" {
		return true
	}
	sigs := r.URL.Query()["sig"]
	if len(sigs) != 1 || !validReviewLinkSignature(sigs[0]) {
		w.Header().Set("WWW-Authenticate", `Signature realm="px1 review"`)
		fail(w, http.StatusUnauthorized, "review link signature is required")
		return false
	}
	expected := reviewLinkSignature(target, secret)
	if !hmac.Equal([]byte(strings.ToLower(sigs[0])), []byte(expected)) {
		fail(w, http.StatusForbidden, "review link signature is invalid")
		return false
	}
	return true
}

func reviewLinkPath(target githubReviewTarget) string {
	path := fmt.Sprintf("/github/%s/%s/pull/%d?sha=%s", target.Owner, target.Repository, target.PullRequest, target.Head)
	if secret := reviewLinkSecret(); secret != "" {
		path += "&sig=" + reviewLinkSignature(target, secret)
	}
	return path
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
	if !authorizeReviewLink(w, r, *target) {
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
