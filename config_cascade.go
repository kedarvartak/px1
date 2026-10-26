package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

const (
	configFileLimit  = 64 << 10
	configDepthLimit = 20
)

type configInput struct {
	Version           int      `json:"version"`
	Strictness        *int     `json:"strictness,omitempty"`
	CommentTypes      []string `json:"commentTypes,omitempty"`
	DisabledRules     []string `json:"disabledRules,omitempty"`
	MaxInlineComments *int     `json:"maxInlineComments,omitempty"`
	FailOnP0          *bool    `json:"failOnP0,omitempty"`
	Context           *struct {
		Repos []string `json:"repos,omitempty"`
	} `json:"context,omitempty"`
}

type resolvedConfig struct {
	Sources           []string `json:"sources"`
	Strictness        int      `json:"strictness"`
	CommentTypes      []string `json:"commentTypes"`
	DisabledRules     []string `json:"disabledRules"`
	MaxInlineComments int      `json:"maxInlineComments"`
	FailOnP0          bool     `json:"failOnP0"`
	ContextRepos      []string `json:"contextRepos,omitempty"`
}

// resolveCascadingConfig applies root-to-leaf .px1/config.json files. Gating
// settings only become stricter: strictness is the maximum and failOnP0 is OR.
func resolveCascadingConfig(target string, files map[string][]byte) (resolvedConfig, error) {
	clean := filepath.ToSlash(filepath.Clean(target))
	if target == "" || filepath.IsAbs(target) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return resolvedConfig{}, errors.New("config target must be a repository-relative file")
	}
	if depth := len(strings.Split(filepath.ToSlash(filepath.Dir(clean)), "/")); depth > configDepthLimit {
		return resolvedConfig{}, errors.New("config target exceeds directory depth limit")
	}
	result := resolvedConfig{Strictness: 2, CommentTypes: []string{"logic", "security", "style", "syntax"}, MaxInlineComments: 15}
	disabled := map[string]bool{}
	contextRepos := map[string]bool{}
	for _, candidate := range configCandidates(clean) {
		body, ok := files[candidate]
		if !ok {
			continue
		}
		input, err := decodeConfig(body)
		if err != nil {
			return resolvedConfig{}, fmt.Errorf("%s: %w", candidate, err)
		}
		result.Sources = append(result.Sources, candidate)
		if input.Strictness != nil && *input.Strictness > result.Strictness {
			result.Strictness = *input.Strictness
		}
		if input.FailOnP0 != nil && *input.FailOnP0 {
			result.FailOnP0 = true
		}
		if input.MaxInlineComments != nil {
			result.MaxInlineComments = *input.MaxInlineComments
		}
		if input.CommentTypes != nil {
			result.CommentTypes = append([]string(nil), input.CommentTypes...)
		}
		for _, id := range input.DisabledRules {
			disabled[id] = true
		}
		if input.Context != nil {
			for _, repository := range input.Context.Repos {
				contextRepos[strings.ToLower(repository)] = true
			}
		}
	}
	for id := range disabled {
		result.DisabledRules = append(result.DisabledRules, id)
	}
	sort.Strings(result.DisabledRules)
	for repository := range contextRepos {
		result.ContextRepos = append(result.ContextRepos, repository)
	}
	if len(result.ContextRepos) > 10 {
		return resolvedConfig{}, errors.New("resolved context.repos cannot exceed 10 repositories")
	}
	sort.Strings(result.ContextRepos)
	return result, nil
}

func decodeConfig(body []byte) (configInput, error) {
	if len(body) == 0 || len(body) > configFileLimit {
		return configInput{}, errors.New("config must be non-empty and at most 64 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var input configInput
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return input, errors.New("config contains trailing data")
	}
	if input.Version != 1 {
		return input, errors.New("version must be 1")
	}
	if input.Strictness != nil && (*input.Strictness < 1 || *input.Strictness > 3) {
		return input, errors.New("strictness must be between 1 and 3")
	}
	if input.MaxInlineComments != nil && (*input.MaxInlineComments < 1 || *input.MaxInlineComments > 50) {
		return input, errors.New("maxInlineComments must be between 1 and 50")
	}
	allowedTypes := map[string]bool{"logic": true, "syntax": true, "style": true, "security": true}
	seen := map[string]bool{}
	for _, kind := range input.CommentTypes {
		if !allowedTypes[kind] || seen[kind] {
			return input, errors.New("commentTypes contains an invalid or duplicate value")
		}
		seen[kind] = true
	}
	disabledSeen := map[string]bool{}
	for _, id := range input.DisabledRules {
		if strings.TrimSpace(id) == "" || len(id) > 80 {
			return input, errors.New("disabledRules contains an invalid id")
		}
		if disabledSeen[id] {
			return input, errors.New("disabledRules contains a duplicate id")
		}
		disabledSeen[id] = true
	}
	if input.Context != nil {
		if len(input.Context.Repos) > 10 {
			return input, errors.New("context.repos cannot exceed 10 repositories")
		}
		seenRepos := map[string]bool{}
		for _, repository := range input.Context.Repos {
			parts := strings.Split(repository, "/")
			key := strings.ToLower(repository)
			if len(parts) != 2 || !configSlug(parts[0]) || !configSlug(parts[1]) || seenRepos[key] {
				return input, errors.New("context.repos contains an invalid or duplicate owner/repository")
			}
			seenRepos[key] = true
		}
	}
	return input, nil
}

func configSlug(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func configCandidates(target string) []string {
	result := []string{".px1/config.json"}
	dir := filepath.ToSlash(filepath.Dir(target))
	if dir == "." {
		return result
	}
	parts := strings.Split(dir, "/")
	for i := range parts {
		result = append(result, strings.Join(parts[:i+1], "/")+"/.px1/config.json")
	}
	return result
}
