package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

type Command struct {
	Name string
	Path string
	Text string
}

// ParseCommand accepts one explicit command on the first non-empty line.
func ParseCommand(body string) (Command, bool) {
	var line string
	for _, candidate := range strings.Split(body, "\n") {
		if strings.TrimSpace(candidate) != "" {
			line = strings.TrimSpace(candidate)
			break
		}
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "@px1") {
		return Command{}, false
	}
	switch strings.ToLower(fields[1]) {
	case "review", "ignore", "why":
		if len(fields) != 2 {
			return Command{}, false
		}
		return Command{Name: strings.ToLower(fields[1])}, true
	case "explain":
		if len(fields) != 3 || !validCommandPath(fields[2]) {
			return Command{}, false
		}
		return Command{Name: "explain", Path: filepath.ToSlash(filepath.Clean(fields[2]))}, true
	case "remember":
		if len(fields) < 3 {
			return Command{}, false
		}
		text := strings.TrimSpace(strings.Join(fields[2:], " "))
		if text == "" || len(text) > 2000 {
			return Command{}, false
		}
		return Command{Name: "remember", Text: text}, true
	default:
		return Command{}, false
	}
}

type CommandAuthorizer struct {
	Client  APIClient
	BaseURL string
}

// Authorize returns a parsed command only when GitHub reports write, maintain,
// or admin access for the commenter. The caller must not enqueue before this.
func (a CommandAuthorizer) Authorize(ctx context.Context, installationID int64, owner, repo, actor, body string) (Command, error) {
	command, ok := ParseCommand(body)
	if !ok {
		return Command{}, errors.New("comment does not contain a supported px1 command")
	}
	if a.Client == nil || installationID <= 0 || !validSlug(owner) || !validSlug(repo) || !validSlug(actor) {
		return Command{}, errors.New("GitHub client and valid command identity are required")
	}
	baseURL := strings.TrimRight(a.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/collaborators/%s/permission", baseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(actor))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Command{}, err
	}
	response, err := a.Client.Do(ctx, installationID, request)
	if err != nil {
		return Command{}, fmt.Errorf("check command permission: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return Command{}, fmt.Errorf("check command permission: status %d", response.StatusCode)
	}
	var result struct {
		Permission string `json:"permission"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return Command{}, fmt.Errorf("decode command permission: %w", err)
	}
	if !map[string]bool{"write": true, "maintain": true, "admin": true}[result.Permission] {
		return Command{}, errors.New("commenter does not have repository write access")
	}
	return command, nil
}

func validCommandPath(value string) bool {
	clean := filepath.ToSlash(filepath.Clean(value))
	return value != "" && !filepath.IsAbs(value) && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}
