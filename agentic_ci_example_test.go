package main

import (
	"os"
	"regexp"
	"testing"
)

// The example checks out px1 and runs it with the caller's API key, so it must
// name an immutable commit, never a branch or tag that can move.
func TestCIExamplePinsPx1ToACommitSHA(t *testing.T) {
	data, err := os.ReadFile("examples/px1-agentic-review.yml")
	if err != nil {
		t.Fatal(err)
	}
	refs := regexp.MustCompile(`(?m)^\s+ref:\s*(\S+)`).FindAllStringSubmatch(string(data), -1)
	if len(refs) != 1 {
		t.Fatalf("want exactly one px1 ref, found %d", len(refs))
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(refs[0][1]) {
		t.Fatalf("px1 ref %q must be a full 40-character commit SHA", refs[0][1])
	}
}
