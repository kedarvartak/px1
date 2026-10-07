package main

import (
	"os"
	"strings"
	"testing"
)

func TestArtifactActionKeepsRevisionInputsExplicit(t *testing.T) {
	body, err := os.ReadFile("action.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"PX1_BASE: ${{ inputs.base }}",
		"PX1_HEAD: ${{ inputs.head }}",
		"--base \"$PX1_BASE\"",
		"--head \"$PX1_HEAD\"",
		"go run . \"${args[@]}\"",
		"test -s \"$PX1_OUT/index.html\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("action.yml missing %q", want)
		}
	}
	if strings.Contains(text, "github.event.pull_request") {
		t.Fatal("the reusable action must receive explicit revisions from its caller")
	}
}
