package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExplanationGeneratorUsesStructuredResponsesOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["model"] != "review-model" || request["store"] != false {
			t.Fatalf("unexpected request: %#v", request)
		}
		text, ok := request["text"].(map[string]any)
		if !ok {
			t.Fatalf("text format missing: %#v", request["text"])
		}
		format, ok := text["format"].(map[string]any)
		if !ok || format["type"] != "json_schema" || format["strict"] != true {
			t.Fatalf("structured output not strict: %#v", text)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{\"explanations\":[{\"id\":\"retry\",\"path\":\"client.go\",\"lineStart\":12,\"lineEnd\":18,\"title\":\"Retries failed calls\",\"summary\":\"The client now retries temporary failures before returning an error.\"}]}"}]}]}`))
	}))
	defer server.Close()

	generator := explanationGenerator{client: server.Client(), endpoint: server.URL, apiKey: "test-secret", model: "review-model"}
	payload, err := generator.request(context.Background(), "diff --git a/client.go b/client.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Explanations) != 1 || payload.Explanations[0].Path != "client.go" {
		t.Fatalf("unexpected explanations: %#v", payload.Explanations)
	}
}

func TestExplanationGeneratorRejectsIncompleteAndRefusedOutput(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"incomplete", `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}`, "output incomplete"},
		{"refusal", `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"cannot comply"}]}]}`, "refused"},
		{"missing", `{"status":"completed","output":[]}`, "no structured explanation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tt.body)) }))
			defer server.Close()
			generator := explanationGenerator{client: server.Client(), endpoint: server.URL, apiKey: "key", model: "model"}
			_, err := generator.request(context.Background(), "diff")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestBoundedDiffCapsProviderInput(t *testing.T) {
	diff := strings.Repeat("x", maxExplanationDiffBytes+100)
	bounded := boundedDiff(diff)
	if !strings.HasSuffix(bounded, "[diff truncated by px1]") {
		t.Fatalf("missing truncation marker")
	}
	if len(bounded) > maxExplanationDiffBytes+64 {
		t.Fatalf("bounded diff too large: %d", len(bounded))
	}
}

func TestFilterExplanationDiffOmitsExcludedFiles(t *testing.T) {
	diff := "diff --git a/src/app.go b/src/app.go\n--- a/src/app.go\n+++ b/src/app.go\n@@ -1 +1 @@\n-old\n+new\n" +
		"diff --git a/config/private.env b/config/private.env\n--- a/config/private.env\n+++ b/config/private.env\n@@ -1 +1 @@\n-old-secret\n+new-secret\n"
	filtered, err := filterExplanationDiff(diff, []string{"config/**", "**/*.pem"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filtered, "src/app.go") || strings.Contains(filtered, "private.env") || strings.Contains(filtered, "new-secret") {
		t.Fatalf("filtered diff = %q", filtered)
	}
	paths := explanationDiffPaths(filtered)
	if len(paths) != 1 || paths[0] != "src/app.go" {
		t.Fatalf("included paths = %#v", paths)
	}
}

func TestFilterExplanationDiffOmitsBinaryAndRenameOnlyFiles(t *testing.T) {
	diff := "diff --git a/assets/private.png b/assets/private.png\nnew file mode 100644\nBinary files /dev/null and b/assets/private.png differ\n" +
		"diff --git a/docs/private.md b/docs/public.md\nsimilarity index 100%\nrename from docs/private.md\nrename to docs/public.md\n"
	filtered, err := filterExplanationDiff(diff, []string{"assets/**", "docs/**"})
	if err != nil {
		t.Fatal(err)
	}
	if filtered != "" {
		t.Fatalf("filtered diff retained excluded metadata: %q", filtered)
	}
}
