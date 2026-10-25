package graph

import (
	"strings"
	"testing"

	"px1/internal/hosted/indexer"
)

func TestEveryIndexedLanguageHasCompilingGraphQueries(t *testing.T) {
	if len(SupportedLanguages()) != 12 {
		t.Fatalf("supported %d graph languages, want 12", len(SupportedLanguages()))
	}
	for _, language := range SupportedLanguages() {
		t.Run(string(language), func(t *testing.T) {
			source, err := Source(language)
			if err != nil {
				t.Fatal(err)
			}
			for _, capture := range []string{"@definition", "@reference", "@import", "@call"} {
				if !strings.Contains(source, capture) {
					t.Errorf("query missing %s", capture)
				}
			}
			query, err := Compile(language)
			if err != nil {
				t.Fatal(err)
			}
			query.Close()
		})
	}
}

func TestUnknownLanguageHasNoGraphQuery(t *testing.T) {
	if _, err := Compile(indexer.LanguageUnknown); err == nil {
		t.Fatal("unknown language unexpectedly compiled")
	}
}
