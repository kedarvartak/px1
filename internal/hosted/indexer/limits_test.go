package indexer

import (
	"strings"
	"testing"
)

func TestRepositoryLimitsProduceDeterministicPartialState(t *testing.T) {
	result, err := ApplyRepositoryLimits([]LimitFile{{Path: "z.go", Size: 4}, {Path: "a.go", Size: 4}, {Path: "b.go", Size: 4}}, RepositoryLimits{MaxFiles: 2, MaxBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "partial" || result.SkippedFiles != 1 || len(result.IndexedFiles) != 2 || result.IndexedFiles[0].Path != "a.go" {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.SummaryLine(), "may miss skipped code") {
		t.Fatalf("summary = %q", result.SummaryLine())
	}
}

func TestRepositoryLimitsUseV1Defaults(t *testing.T) {
	result, err := ApplyRepositoryLimits([]LimitFile{{Path: "a.go", Size: 1}}, RepositoryLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "ready" || result.IndexedBytes != 1 {
		t.Fatalf("result = %+v", result)
	}
}
