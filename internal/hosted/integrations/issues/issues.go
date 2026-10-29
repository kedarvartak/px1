// Package issues retrieves bounded, OAuth-authorized work-item context.
package issues

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxIssueResponse = 2 << 20

var issueKeyPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,9}-[1-9][0-9]*\b`)

type Provider string

const (
	Linear Provider = "linear"
	Jira   Provider = "jira"
)

type Issue struct {
	Provider                Provider
	Key, Title, Description string
	AcceptanceCriteria      []string
	URL                     string
}
type TokenSource interface {
	OAuthToken(context.Context, int64, Provider) (string, error)
}
type Tracker interface {
	FetchIssue(context.Context, int64, string) (Issue, error)
}

type OAuthGrant struct {
	Provider     Provider
	AccountID    string
	BaseURL      string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}
type OAuthExchanger interface {
	ExchangeOAuthCode(context.Context, Provider, string, string) (OAuthGrant, error)
}
type ConnectionStore interface {
	SaveOAuthConnection(context.Context, int64, OAuthGrant) error
}

// CompleteOAuth binds the provider and tenant from the already-verified
// dashboard session rather than trusting callback query parameters.
func CompleteOAuth(ctx context.Context, exchanger OAuthExchanger, store ConnectionStore, installationID int64, provider Provider, code, redirectURI string) error {
	if exchanger == nil || store == nil || installationID <= 0 || (provider != Linear && provider != Jira) || code == "" || !strings.HasPrefix(redirectURI, "https://") {
		return errors.New("OAuth exchanger, store, tenant, provider, code, and HTTPS redirect are required")
	}
	grant, err := exchanger.ExchangeOAuthCode(ctx, provider, code, redirectURI)
	if err != nil {
		return err
	}
	if grant.Provider != provider || grant.AccountID == "" || grant.AccessToken == "" {
		return errors.New("issue tracker OAuth grant is incomplete or for the wrong provider")
	}
	return store.SaveOAuthConnection(ctx, installationID, grant)
}

func DetectIssueKeys(branch, pullBody string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, source := range []string{branch, pullBody} {
		for _, key := range issueKeyPattern.FindAllString(strings.ToUpper(source), -1) {
			if !seen[key] && len(keys) < 10 {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func ExtractAcceptanceCriteria(description string) []string {
	var criteria []string
	inSection := false
	for _, raw := range strings.Split(description, "\n") {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(strings.Trim(line, "#*: "))
		if lower == "acceptance criteria" || lower == "acceptance criterion" {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "#") {
			break
		}
		if strings.HasPrefix(line, "- [ ] ") || strings.HasPrefix(line, "- [x] ") || strings.HasPrefix(line, "- [X] ") {
			criteria = append(criteria, strings.TrimSpace(line[6:]))
			continue
		}
		if inSection && (strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ")) {
			criteria = append(criteria, strings.TrimSpace(line[2:]))
		}
		if len(criteria) == 20 {
			break
		}
	}
	return criteria
}

type LinearClient struct {
	HTTP    *http.Client
	Tokens  TokenSource
	BaseURL string
}

func (c LinearClient) FetchIssue(ctx context.Context, installationID int64, key string) (Issue, error) {
	if !issueKeyPattern.MatchString(key) {
		return Issue{}, errors.New("valid Linear issue key is required")
	}
	var response struct {
		Data struct {
			Issue struct{ Identifier, Title, Description, URL string } `json:"issue"`
		} `json:"data"`
		Errors []any `json:"errors"`
	}
	payload := map[string]any{"query": "query Px1Issue($id: String!) { issue(id: $id) { identifier title description url } }", "variables": map[string]string{"id": key}}
	if err := request(ctx, c.HTTP, c.Tokens, installationID, Linear, first(c.BaseURL, "https://api.linear.app/graphql"), http.MethodPost, payload, &response); err != nil {
		return Issue{}, err
	}
	if len(response.Errors) > 0 || response.Data.Issue.Identifier == "" {
		return Issue{}, errors.New("Linear issue was not found")
	}
	item := response.Data.Issue
	return Issue{Provider: Linear, Key: item.Identifier, Title: item.Title, Description: item.Description, AcceptanceCriteria: ExtractAcceptanceCriteria(item.Description), URL: item.URL}, nil
}

type JiraClient struct {
	HTTP    *http.Client
	Tokens  TokenSource
	BaseURL string
}

func (c JiraClient) FetchIssue(ctx context.Context, installationID int64, key string) (Issue, error) {
	if !issueKeyPattern.MatchString(key) || !strings.HasPrefix(c.BaseURL, "https://") {
		return Issue{}, errors.New("valid Jira URL and issue key are required")
	}
	var response struct {
		Key    string `json:"key"`
		Fields struct {
			Summary     string `json:"summary"`
			Description any    `json:"description"`
		} `json:"fields"`
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/rest/api/3/issue/" + url.PathEscape(key) + "?fields=summary,description"
	if err := request(ctx, c.HTTP, c.Tokens, installationID, Jira, endpoint, http.MethodGet, nil, &response); err != nil {
		return Issue{}, err
	}
	description := flattenText(response.Fields.Description)
	return Issue{Provider: Jira, Key: response.Key, Title: response.Fields.Summary, Description: description, AcceptanceCriteria: ExtractAcceptanceCriteria(description), URL: strings.TrimRight(c.BaseURL, "/") + "/browse/" + url.PathEscape(response.Key)}, nil
}

func request(ctx context.Context, client *http.Client, tokens TokenSource, installationID int64, provider Provider, endpoint, method string, payload, output any) error {
	if tokens == nil || installationID <= 0 {
		return errors.New("OAuth token source and tenant are required")
	}
	token, err := tokens.OAuthToken(ctx, installationID, provider)
	if err != nil || token == "" {
		return errors.New("OAuth token is unavailable")
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxIssueResponse))
		return fmt.Errorf("%s returned status %d", provider, res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, maxIssueResponse)).Decode(output)
}

func flattenText(value any) string {
	var parts []string
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				parts = append(parts, typed)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case map[string]any:
			if text, ok := typed["text"]; ok {
				walk(text)
			} else if content, ok := typed["content"]; ok {
				walk(content)
			}
		}
	}
	walk(value)
	return strings.Join(parts, "\n")
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
