package agent

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Built at run time so no literal in the source matches the secret signatures
// that px1's own privacy check blocks reports for.
var guardSecret = "sk-live-" + strings.Repeat("a", 26)

func guardedFixture(t *testing.T) (Guarded, fixture) {
	t.Helper()
	f := newFixture(t)
	write(t, f.repo.Root, ".env", "OPENAI_API_KEY="+guardSecret+"\n")
	write(t, f.repo.Root, "pkg/config.go", "package pkg\n\nconst key = \""+guardSecret+"\"\nconst needle = \"needle\"\n")
	runGit(t, f.repo.Root, "add", "-A")
	runGit(t, f.repo.Root, "commit", "-qm", "add secrets")
	f.scope.HeadSHA = runGit(t, f.repo.Root, "rev-parse", "HEAD")
	// The fixture's hostile config also fires on the setup commits above, so
	// clear the marker: only what the tools do from here on should count.
	_ = os.Remove(f.marker)
	guard := Guarded{
		Repo: f.repo,
		Allow: func(p string) (bool, string) {
			if p == ".env" || strings.HasPrefix(p, "d:ir/") {
				return false, "excluded"
			}
			return true, ""
		},
		Redact: func(text string) string {
			return strings.ReplaceAll(text, guardSecret, "[redacted secret]")
		},
	}
	return guard, f
}

func TestGuardedToolsHonourThePrivacyPolicyEverywhere(t *testing.T) {
	g, f := guardedFixture(t)
	ctx := context.Background()
	if _, err := g.ReadLines(ctx, f.scope, ".env", 1, 5); err == nil || !strings.Contains(err.Error(), "excluded") {
		t.Fatalf("excluded file read: %v", err)
	}
	if _, err := g.Blame(ctx, f.scope, ".env", 1, 1); err == nil {
		t.Fatal("excluded file blamed")
	}
	if _, err := g.Log(ctx, f.scope, ".env", 3); err == nil {
		t.Fatal("excluded file history read")
	}
	if _, err := g.ReadLines(ctx, f.scope, "../escape", 1, 1); err == nil {
		t.Fatal("path escape accepted")
	}
	hits, err := g.Search(ctx, f.scope, "needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range hits {
		if strings.HasPrefix(hit.Path, "d:ir/") {
			t.Fatalf("search leaked an excluded path: %#v", hit)
		}
	}
	if len(hits) == 0 {
		t.Fatal("search lost the allowed hits")
	}
	if secrets, _ := g.Search(ctx, f.scope, "OPENAI_API_KEY", 10); len(secrets) != 0 {
		t.Fatalf("search found text in an excluded file: %#v", secrets)
	}
	listing, err := g.ListFiles(ctx, f.scope, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range listing {
		if entry == ".env" {
			t.Fatalf("an excluded file is visible in the listing: %v", listing)
		}
	}
	if !strings.Contains(strings.Join(listing, ","), "pkg/") {
		t.Fatalf("listing lost allowed entries: %v", listing)
	}
}

func TestGuardedToolsRedactSecretsInAllowedFiles(t *testing.T) {
	g, f := guardedFixture(t)
	read, err := g.ReadLines(context.Background(), f.scope, "pkg/config.go", 1, 10)
	if err != nil || strings.Contains(read, guardSecret) || !strings.Contains(read, "[redacted secret]") {
		t.Fatalf("read = %q %v", read, err)
	}
	hits, err := g.Search(context.Background(), f.scope, "sk-live-", 10)
	if err != nil || len(hits) != 1 || strings.Contains(hits[0].Snippet, guardSecret[:16]) {
		t.Fatalf("search snippet not redacted: %#v %v", hits, err)
	}
}

func TestGuardedPlugsIntoTheToolbox(t *testing.T) {
	g, f := guardedFixture(t)
	box := Toolbox{Files: g, Lister: g, Search: g, History: g}
	if _, err := box.Dispatch(context.Background(), f.scope, Call{Name: ReadFile, Arguments: []byte(`{"path":".env","start_line":1,"end_line":2}`)}); err == nil {
		t.Fatal("the toolbox read an excluded file")
	}
	result, err := box.Dispatch(context.Background(), f.scope, Call{Name: ReadFile, Arguments: []byte(`{"path":"pkg/config.go","start_line":1,"end_line":4}`)})
	if err != nil || strings.Contains(result.Content, guardSecret) {
		t.Fatalf("result=%q err=%v", result.Content, err)
	}
	if f.ran() {
		t.Fatal("a guarded call ran repository-configured code")
	}
}
