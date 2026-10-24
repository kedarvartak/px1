package main

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

type githubAppManifest struct {
	Name               string            `json:"name"`
	URL                string            `json:"url"`
	RedirectURL        string            `json:"redirect_url"`
	CallbackURLs       []string          `json:"callback_urls"`
	SetupURL           string            `json:"setup_url"`
	RequestOAuth       bool              `json:"request_oauth_on_install"`
	SetupOnUpdate      bool              `json:"setup_on_update"`
	Public             bool              `json:"public"`
	DefaultPermissions map[string]string `json:"default_permissions"`
	DefaultEvents      []string          `json:"default_events"`
	HookAttributes     struct {
		URL    string `json:"url"`
		Active bool   `json:"active"`
	} `json:"hook_attributes"`
}

func TestGitHubAppManifestsKeepLeastPrivilegeContract(t *testing.T) {
	wantPermissions := map[string]string{
		"checks": "write", "contents": "read", "issues": "write",
		"metadata": "read", "pull_requests": "write",
	}
	wantEvents := []string{
		"check_run", "installation", "installation_repositories", "issue_comment",
		"pull_request", "pull_request_review_comment", "push",
	}
	for _, path := range []string{"deploy/github-app/dev.json", "deploy/github-app/prod.json"} {
		t.Run(path, func(t *testing.T) {
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var manifest githubAppManifest
			if err := json.Unmarshal(body, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Name == "" || manifest.URL == "" || manifest.RedirectURL == "" || len(manifest.CallbackURLs) != 1 || manifest.SetupURL == "" || !manifest.RequestOAuth || !manifest.SetupOnUpdate || manifest.HookAttributes.URL == "" || !manifest.HookAttributes.Active {
				t.Fatalf("manifest is missing required registration fields: %+v", manifest)
			}
			if !reflect.DeepEqual(manifest.DefaultPermissions, wantPermissions) {
				t.Fatalf("permissions = %#v, want %#v", manifest.DefaultPermissions, wantPermissions)
			}
			sort.Strings(manifest.DefaultEvents)
			if !reflect.DeepEqual(manifest.DefaultEvents, wantEvents) {
				t.Fatalf("events = %#v, want %#v", manifest.DefaultEvents, wantEvents)
			}
		})
	}
}

func TestProductionAndDevelopmentAppsStaySeparate(t *testing.T) {
	read := func(path string) githubAppManifest {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var manifest githubAppManifest
		if err := json.Unmarshal(body, &manifest); err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	dev := read("deploy/github-app/dev.json")
	prod := read("deploy/github-app/prod.json")
	if dev.Name == prod.Name || dev.URL == prod.URL || dev.HookAttributes.URL == prod.HookAttributes.URL {
		t.Fatal("development and production GitHub Apps must have distinct names and endpoints")
	}
	if dev.Public || !prod.Public {
		t.Fatal("development must stay private while production is publicly installable")
	}
}
