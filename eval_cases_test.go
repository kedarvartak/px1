package main

import (
	"os"
	"testing"

	"px1/internal/hosted/eval"
)

// The committed example must stay loadable and show both kinds of case: seeded
// bugs, and clean changes, which are what measure false alarms.
func TestCommittedEvalCasesAreValid(t *testing.T) {
	data, err := os.ReadFile("eval/example-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := eval.DecodeRepeat(data)
	if err != nil {
		t.Fatal(err)
	}
	seeded, clean := 0, 0
	for _, c := range dataset.Cases {
		switch c.Kind {
		case "seeded":
			seeded++
		case "clean":
			clean++
		}
	}
	if seeded < 1 || clean < 1 {
		t.Fatalf("want at least one seeded and one clean case, got %d and %d", seeded, clean)
	}
}
