package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// resolveCommit canonicalizes an explicit Git ref before report generation so
// the artifact identity cannot drift with the checkout.
func resolveCommit(root, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("commit ref is empty")
	}
	b, err := exec.Command("git", "-C", root, "rev-parse", "--verify", ref+"^{commit}").Output()
	if err != nil {
		return "", fmt.Errorf("could not resolve commit %q: %w", ref, err)
	}
	commit := strings.TrimSpace(string(b))
	if !validRevision(commit) {
		return "", fmt.Errorf("could not resolve commit %q to a full SHA", ref)
	}
	return commit, nil
}
