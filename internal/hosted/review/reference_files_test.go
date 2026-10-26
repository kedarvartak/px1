package review

import "testing"

func TestReferenceFilesSelectsBoundedRepositoryContext(t *testing.T) {
	config, err := DecodeReferenceFiles([]byte(`{"version":1,"files":[{"path":"docs/api.md","for":["internal/api/**"],"description":"API contract"},{"path":"go.mod","for":["*.go"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := config.ForPath("internal/api/handler.go")
	if len(got) != 1 || got[0].Path != "docs/api.md" {
		t.Fatalf("references = %#v", got)
	}
	if got := config.ForPath("../secret"); got != nil {
		t.Fatalf("unsafe path returned references: %#v", got)
	}
}

func TestReferenceFilesRejectsEscapesAndUnknownFields(t *testing.T) {
	for _, input := range []string{
		`{"version":1,"files":[{"path":"../secret","for":["src/**"]}]}`,
		`{"version":1,"files":[{"path":"README.md","for":["../**"]}]}`,
		`{"version":1,"files":[],"extra":true}`,
	} {
		if _, err := DecodeReferenceFiles([]byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}
