package main

import (
	"strings"
	"testing"
)

func TestAffectedFileHintsExplainImportsTestsConfigAndDependencies(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "test")
	writeExportTestFile(t, root, "src/service.ts", "export const value = 1;\n")
	writeExportTestFile(t, root, "src/controller.ts", "import { value } from './service';\n")
	writeExportTestFile(t, root, "src/service.test.ts", "import { value } from './service';\n")
	writeExportTestFile(t, root, "tsconfig.json", "{}\n")
	writeExportTestFile(t, root, "package.json", "{\"dependencies\":{}}\n")
	writeExportTestFile(t, root, "package-lock.json", "{\"lockfileVersion\":3}\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))

	writeExportTestFile(t, root, "src/service.ts", "export const value = 2;\n")
	writeExportTestFile(t, root, "package.json", "{\"dependencies\":{\"demo\":\"1\"}}\n")
	runGitTest(t, root, "commit", "-am", "head")
	head := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))

	report, err := generateStaticReview(root, base, head)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]bool{}
	for _, hint := range report.AffectedFiles {
		got[hint.Path] = map[string]bool{}
		for _, evidence := range hint.Evidence {
			got[hint.Path][evidence.Kind] = true
			if evidence.ChangedPath == "" || evidence.Detail == "" {
				t.Fatalf("unexplained evidence: %#v", evidence)
			}
		}
	}
	for file, kind := range map[string]string{
		"src/controller.ts":   "import",
		"src/service.test.ts": "test",
		"tsconfig.json":       "config",
		"package-lock.json":   "dependency",
	} {
		if !got[file][kind] {
			t.Errorf("missing %s evidence for %s in %#v", kind, file, got)
		}
	}
	if _, exists := got["src/service.ts"]; exists {
		t.Fatal("changed files must not appear as affected-file hints")
	}
}

func TestAffectedFileHintsAreEmptyNotNull(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "test")
	writeExportTestFile(t, root, "notes.txt", "one\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "base")
	writeExportTestFile(t, root, "notes.txt", "two\n")
	runGitTest(t, root, "commit", "-am", "head")
	head := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	hints, err := affectedFileHints(root, head, []staticReviewFile{{Path: "notes.txt"}})
	if err != nil || hints == nil || len(hints) != 0 {
		t.Fatalf("hints = %#v, err = %v", hints, err)
	}
}
