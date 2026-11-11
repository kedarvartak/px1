//go:build gitlabce

package gitlabce

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestGitLabCEProviderContract(t *testing.T) {
	base, token := os.Getenv("GITLAB_CE_URL"), os.Getenv("GITLAB_TOKEN")
	if base == "" || token == "" {
		t.Fatal("GITLAB_CE_URL and GITLAB_TOKEN are required")
	}
	client := &http.Client{Timeout: 20 * time.Second}

	projectName := fmt.Sprintf("px1-contract-%d", time.Now().UnixNano())
	project := requestJSON(t, client, token, http.MethodPost, base+"/api/v4/projects", map[string]any{"name": projectName, "initialize_with_readme": true})
	projectID := int(project["id"].(float64))
	requestJSON(t, client, token, http.MethodPost, fmt.Sprintf("%s/api/v4/projects/%d/repository/branches", base, projectID), map[string]any{"branch": "feature", "ref": "main"})
	requestJSON(t, client, token, http.MethodPost, fmt.Sprintf("%s/api/v4/projects/%d/repository/files/demo.txt", base, projectID), map[string]any{"branch": "feature", "content": "contract\n", "commit_message": "add contract file"})
	mergeRequest := requestJSON(t, client, token, http.MethodPost, fmt.Sprintf("%s/api/v4/projects/%d/merge_requests", base, projectID), map[string]any{"source_branch": "feature", "target_branch": "main", "title": "Provider contract"})
	iid := int(mergeRequest["iid"].(float64))
	changes := requestJSONList(t, client, token, fmt.Sprintf("%s/api/v4/projects/%d/merge_requests/%d/diffs?per_page=100", base, projectID, iid))
	if len(changes) != 1 {
		t.Fatalf("changes=%#v", changes)
	}
	requestJSON(t, client, token, http.MethodPost, fmt.Sprintf("%s/api/v4/projects/%d/merge_requests/%d/notes", base, projectID, iid), map[string]any{"body": "<!-- px1-summary --> contract"})
	discussions := requestJSONList(t, client, token, fmt.Sprintf("%s/api/v4/projects/%d/merge_requests/%d/discussions", base, projectID, iid))
	if len(discussions) == 0 {
		t.Fatal("summary note was not visible through discussions")
	}
}

func requestJSON(t *testing.T, client *http.Client, token, method, endpoint string, payload any) map[string]any {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		t.Fatalf("%s %s: %d %s", method, endpoint, response.StatusCode, data)
	}
	var output map[string]any
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&output); err != nil {
		t.Fatal(err)
	}
	return output
}

func requestJSONList(t *testing.T, client *http.Client, token, endpoint string) []any {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, endpoint, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var output []any
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&output) != nil {
		t.Fatalf("GET %s: %d", endpoint, response.StatusCode)
	}
	return output
}
