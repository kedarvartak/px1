package githubapp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type commandAPIFake struct {
	permission string
	calls      int
}

func (f *commandAPIFake) Do(_ context.Context, _ int64, _ *http.Request) (*http.Response, error) {
	f.calls++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"permission":"` + f.permission + `"}`))}, nil
}

func TestParseCommand(t *testing.T) {
	for input, want := range map[string]Command{
		"@px1 review": {Name: "review"}, "hello\n@px1 why": {},
		"\n @PX1 explain internal/api.go\nmore": {Name: "explain", Path: "internal/api.go"},
	} {
		got, ok := ParseCommand(input)
		if want.Name == "" {
			if ok {
				t.Fatalf("ParseCommand(%q) unexpectedly accepted", input)
			}
		} else if !ok || got != want {
			t.Fatalf("ParseCommand(%q) = %#v, %v", input, got, ok)
		}
	}
	if _, ok := ParseCommand("@px1 explain ../secret"); ok {
		t.Fatal("accepted escaping path")
	}
}

func TestCommandAuthorizerRequiresWriteAccess(t *testing.T) {
	for permission, allowed := range map[string]bool{"read": false, "triage": false, "write": true, "maintain": true, "admin": true} {
		api := &commandAPIFake{permission: permission}
		got, err := (CommandAuthorizer{Client: api, BaseURL: "https://github.test"}).Authorize(context.Background(), 1, "acme", "widget", "octocat", "@px1 review")
		if allowed && (err != nil || got.Name != "review") {
			t.Fatalf("permission %s: got %#v, %v", permission, got, err)
		}
		if !allowed && err == nil {
			t.Fatalf("permission %s was allowed", permission)
		}
	}
}
