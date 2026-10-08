package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultResponsesEndpoint = "https://api.openai.com/v1/responses"
	maxExplanationDiffBytes  = 120_000
	maxResponsesBodyBytes    = 2_000_000
)

type explanationGenerator struct {
	client   *http.Client
	endpoint string
	apiKey   string
	model    string
}

type generatedExplanationPayload struct {
	Explanations []staticExplanation `json:"explanations"`
}

type repeatedStringFlag []string

func (values *repeatedStringFlag) String() string { return strings.Join(*values, ",") }
func (values *repeatedStringFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("exclude glob cannot be empty")
	}
	*values = append(*values, value)
	return nil
}

type responsesAPIResponse struct {
	Status     string `json:"status"`
	Incomplete *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Refusal string `json:"refusal"`
		} `json:"content"`
	} `json:"output"`
}

func runGenerateExplanations(args []string) error {
	fs := flag.NewFlagSet("generate-explanations", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	base := fs.String("base", "", "base commit or ref (required)")
	head := fs.String("head", "HEAD", "head commit or ref")
	root := fs.String("root", ".", "repository root")
	out := fs.String("out", "px1-explanations.json", "explanation JSON output path")
	model := fs.String("model", "", "OpenAI model (required)")
	endpoint := fs.String("endpoint", defaultResponsesEndpoint, "Responses API endpoint")
	var excludeGlobs repeatedStringFlag
	fs.Var(&excludeGlobs, "exclude-glob", "repository-relative glob excluded from model input (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*base) == "" || strings.TrimSpace(*model) == "" {
		return errors.New("generate-explanations requires --base and --model")
	}
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return errors.New("generate-explanations requires OPENAI_API_KEY")
	}
	repoRoot, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	file, err := generateExplanationsWithExclusions(context.Background(), repoRoot, *base, *head, excludeGlobs, explanationGenerator{
		client: &http.Client{Timeout: 90 * time.Second}, endpoint: strings.TrimSpace(*endpoint), apiKey: apiKey, model: strings.TrimSpace(*model),
	})
	if err != nil {
		return err
	}
	if err := writeExplanationFile(*out, file); err != nil {
		return err
	}
	fmt.Printf("commit-pinned explanations written to %s\n", *out)
	return nil
}

func generateExplanations(ctx context.Context, root, base, head string, generator explanationGenerator) (staticExplanationFile, error) {
	return generateExplanationsWithExclusions(ctx, root, base, head, nil, generator)
}

func generateExplanationsWithExclusions(ctx context.Context, root, base, head string, excludeGlobs []string, generator explanationGenerator) (staticExplanationFile, error) {
	baseSHA, err := resolveCommit(root, base)
	if err != nil {
		return staticExplanationFile{}, err
	}
	headSHA, err := resolveCommit(root, head)
	if err != nil {
		return staticExplanationFile{}, err
	}
	diff, err := gitDiffBetween(root, baseSHA, headSHA)
	if err != nil {
		return staticExplanationFile{}, err
	}
	diff, err = filterExplanationDiff(diff, excludeGlobs)
	if err != nil {
		return staticExplanationFile{}, err
	}
	result := staticExplanationFile{Version: 1, Revision: headSHA, Explanations: []staticExplanation{}}
	paths := explanationDiffPaths(diff)
	if len(paths) == 0 {
		return result, nil
	}
	changed := make(map[string]bool, len(paths))
	for _, path := range paths {
		changed[path] = true
	}
	payload, err := generator.request(ctx, boundedDiff(diff))
	if err != nil {
		return staticExplanationFile{}, err
	}
	validated, err := validateStaticExplanations(payload.Explanations, changed, "generated explanations")
	if err != nil {
		return staticExplanationFile{}, err
	}
	result.Explanations = validated
	return result, nil
}

func explanationDiffPaths(diff string) []string {
	seen := map[string]bool{}
	paths := []string{}
	for _, raw := range strings.Split(diff, "diff --git ") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		path := staticDiffPath("diff --git " + raw)
		if path != "" && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

func filterExplanationDiff(diff string, excludeGlobs []string) (string, error) {
	if len(excludeGlobs) == 0 {
		return diff, nil
	}
	compiled := make([]*regexp.Regexp, 0, len(excludeGlobs))
	for _, pattern := range excludeGlobs {
		glob, err := globRegexp(pattern)
		if err != nil {
			return "", fmt.Errorf("exclude glob %q: %w", pattern, err)
		}
		if glob != nil {
			compiled = append(compiled, glob)
		}
	}
	var kept strings.Builder
	for _, raw := range strings.Split(diff, "diff --git ") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		block := "diff --git " + raw
		path := staticDiffPath(block)
		if path != "" && matchesAnyGlob(compiled, path) {
			continue
		}
		kept.WriteString(block)
	}
	return kept.String(), nil
}

func boundedDiff(diff string) string {
	if len(diff) <= maxExplanationDiffBytes {
		return diff
	}
	return diff[:maxExplanationDiffBytes] + "\n\n[diff truncated by px1]"
}

func (g explanationGenerator) request(ctx context.Context, diff string) (generatedExplanationPayload, error) {
	if g.client == nil || g.endpoint == "" || g.apiKey == "" || g.model == "" {
		return generatedExplanationPayload{}, errors.New("explanation generator is not configured")
	}
	requestBody := map[string]any{
		"model":             g.model,
		"store":             false,
		"instructions":      "Explain only meaningful behavior changes in this pull request. Use plain language for a reviewer. Anchor every explanation to a changed file and a new-file line range visible in the diff. Do not invent behavior or repeat the diff. Return no more than 30 concise explanations.",
		"input":             diff,
		"text":              map[string]any{"format": explanationOutputFormat()},
		"max_output_tokens": 6000,
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return generatedExplanationPayload{}, fmt.Errorf("encode Responses API request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, bytes.NewReader(body))
	if err != nil {
		return generatedExplanationPayload{}, fmt.Errorf("create Responses API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return generatedExplanationPayload{}, fmt.Errorf("call Responses API: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponsesBodyBytes+1))
	if err != nil {
		return generatedExplanationPayload{}, fmt.Errorf("read Responses API response: %w", err)
	}
	if len(responseBody) > maxResponsesBodyBytes {
		return generatedExplanationPayload{}, errors.New("Responses API response exceeded size limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return generatedExplanationPayload{}, fmt.Errorf("Responses API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var apiResponse responsesAPIResponse
	if err := json.Unmarshal(responseBody, &apiResponse); err != nil {
		return generatedExplanationPayload{}, fmt.Errorf("decode Responses API response: %w", err)
	}
	if apiResponse.Status == "incomplete" {
		reason := "unknown reason"
		if apiResponse.Incomplete != nil && apiResponse.Incomplete.Reason != "" {
			reason = apiResponse.Incomplete.Reason
		}
		return generatedExplanationPayload{}, fmt.Errorf("Responses API output incomplete: %s", reason)
	}
	for _, output := range apiResponse.Output {
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Type == "refusal" {
				return generatedExplanationPayload{}, fmt.Errorf("Responses API refused explanation generation: %s", content.Refusal)
			}
			if content.Type == "output_text" && content.Text != "" {
				var payload generatedExplanationPayload
				if err := json.Unmarshal([]byte(content.Text), &payload); err != nil {
					return generatedExplanationPayload{}, fmt.Errorf("decode structured explanation output: %w", err)
				}
				return payload, nil
			}
		}
	}
	return generatedExplanationPayload{}, errors.New("Responses API returned no structured explanation output")
}

func explanationOutputFormat() map[string]any {
	return map[string]any{
		"type": "json_schema", "name": "px1_review_explanations", "strict": true,
		"schema": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"explanations"},
			"properties": map[string]any{"explanations": map[string]any{
				"type": "array", "maxItems": 30,
				"items": map[string]any{
					"type": "object", "additionalProperties": false,
					"required": []string{"id", "path", "lineStart", "lineEnd", "title", "summary"},
					"properties": map[string]any{
						"id": map[string]any{"type": "string"}, "path": map[string]any{"type": "string"},
						"lineStart": map[string]any{"type": "integer", "minimum": 1}, "lineEnd": map[string]any{"type": "integer", "minimum": 1},
						"title": map[string]any{"type": "string", "maxLength": explanationMaxTitle}, "summary": map[string]any{"type": "string", "maxLength": explanationMaxText},
					},
				},
			}},
		},
	}
}

func writeExplanationFile(path string, file staticExplanationFile) error {
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode explanation file: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create explanation directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".px1-explanations-*")
	if err != nil {
		return fmt.Errorf("create temporary explanation file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(append(body, '\n')); err != nil {
		temp.Close()
		return fmt.Errorf("write explanation file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close explanation file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish explanation file: %w", err)
	}
	return nil
}
