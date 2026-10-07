package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestDetectReviewAttentionRules(t *testing.T) {
	largeBefore := make([]string, attentionLargeLineDelta+1)
	largeAfter := make([]string, attentionLargeLineDelta+1)
	for i := range largeBefore {
		largeBefore[i] = fmt.Sprintf("old line %d", i)
		largeAfter[i] = fmt.Sprintf("new line %d", i)
	}
	candidates := []attentionCandidate{
		{Path: "package-lock.json"},
		{Path: "internal/auth/session.go"},
		{Path: "db/migrations/001_users.sql"},
		{Path: ".env.production"},
		{Path: "api/users_test.go", Deleted: true},
		{Path: "generated/client.pb.go"},
		{Path: "scripts/deploy.sh", BaselineMode: 0o644, CurrentMode: 0o755},
		{Path: "bulk.txt", Baseline: []byte(strings.Join(largeBefore, "\n")), Current: []byte(strings.Join(largeAfter, "\n")), ContentAvailable: true},
		{Path: "routes.go", Current: []byte("router.get(\"/users\", handler)\n"), ContentAvailable: true},
		{Path: "README.md", Current: []byte("small documentation edit\n"), ContentAvailable: true},
	}
	attention := detectReviewAttention(candidates, nil)
	want := map[string]bool{
		"dependency":      true,
		"authentication":  true,
		"schema":          true,
		"configuration":   true,
		"deleted-test":    true,
		"generated":       true,
		"executable-mode": true,
		"large-diff":      true,
		"public-api":      true,
	}
	for _, flag := range attention.Flags {
		delete(want, flag.Rule)
		if flag.Path == "README.md" {
			t.Fatalf("ordinary file received attention flag: %#v", flag)
		}
		if flag.ID == "" || flag.Reason == "" || flag.Evidence == "" {
			t.Fatalf("flag is not explainable: %#v", flag)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing attention rules: %v; flags: %#v", want, attention.Flags)
	}
}

func TestDetectReviewAttentionAvoidsNearMatches(t *testing.T) {
	candidates := []attentionCandidate{
		{Path: "docs/package-guide.md"},
		{Path: "internal/author.go"},
		{Path: "db/models.go"},
		{Path: "notes/environmental-impact.md"},
		{Path: "api/users_test.go"},
		{Path: "internal/client.go"},
		{Path: "scripts/deploy.sh", BaselineMode: 0o644, CurrentMode: 0o644},
		{Path: "small.go", Baseline: []byte("package small\n"), Current: []byte("package small\nvar private = 1\n"), ContentAvailable: true},
	}
	if got := detectReviewAttention(candidates, nil); len(got.Flags) != 0 {
		t.Fatalf("near matches received attention flags: %#v", got.Flags)
	}
}

func TestDetectReviewAttentionDismissesExactFlag(t *testing.T) {
	candidate := attentionCandidate{Path: "package.json"}
	initial := detectReviewAttention([]attentionCandidate{candidate}, nil)
	if len(initial.Flags) != 1 {
		t.Fatalf("initial flags = %#v", initial.Flags)
	}
	dismissed := map[string]bool{initial.Flags[0].ID: true}
	if got := detectReviewAttention([]attentionCandidate{candidate}, dismissed); len(got.Flags) != 0 {
		t.Fatalf("dismissed flag returned: %#v", got.Flags)
	}
}

func TestDetectReviewAttentionCapsOutput(t *testing.T) {
	candidates := make([]attentionCandidate, attentionMaxFlags+10)
	for i := range candidates {
		candidates[i] = attentionCandidate{Path: fmt.Sprintf("deps/%03d/package.json", i)}
	}
	got := detectReviewAttention(candidates, nil)
	if len(got.Flags) != attentionMaxFlags || !got.Truncated {
		t.Fatalf("bounded result = %d flags, truncated=%v", len(got.Flags), got.Truncated)
	}
}
