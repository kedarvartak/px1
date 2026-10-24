package report

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type OAuthFlow struct {
	ClientID, ClientSecret, CallbackURL string
	StateKey                            []byte
	HTTP                                *http.Client
	AuthorizeURL, TokenURL              string
	Now                                 func() time.Time
}

type oauthState struct {
	Redirect string `json:"redirect"`
	Expires  int64  `json:"expires"`
	Nonce    string `json:"nonce"`
	Binding  string `json:"binding"`
}

func (f OAuthFlow) Begin(redirect, sessionBinding string) (string, error) {
	if f.ClientID == "" || !httpURL(f.CallbackURL) || len(f.StateKey) < 32 || !safeRedirect(redirect) || len(sessionBinding) < 16 {
		return "", errors.New("OAuth client, callback, state key, and safe redirect are required")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	payload, _ := json.Marshal(oauthState{Redirect: redirect, Expires: now().Add(10 * time.Minute).Unix(), Nonce: base64.RawURLEncoding.EncodeToString(nonce), Binding: bindingHash(sessionBinding)})
	state := signState(payload, f.StateKey)
	base := f.AuthorizeURL
	if base == "" {
		base = "https://github.com/login/oauth/authorize"
	}
	values := url.Values{"client_id": {f.ClientID}, "redirect_uri": {f.CallbackURL}, "scope": {"read:user repo"}, "state": {state}}
	return base + "?" + values.Encode(), nil
}

func (f OAuthFlow) ValidateState(state, sessionBinding string) (string, error) {
	parts := strings.Split(state, ".")
	if len(parts) != 2 {
		return "", errors.New("invalid OAuth state")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("invalid OAuth state")
	}
	want := hmac.New(sha256.New, f.StateKey)
	_, _ = want.Write(payload)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, want.Sum(nil)) {
		return "", errors.New("invalid OAuth state")
	}
	var value oauthState
	if json.Unmarshal(payload, &value) != nil || value.Nonce == "" || !safeRedirect(value.Redirect) || !hmac.Equal([]byte(value.Binding), []byte(bindingHash(sessionBinding))) {
		return "", errors.New("invalid OAuth state")
	}
	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	if now().Unix() > value.Expires {
		return "", errors.New("expired OAuth state")
	}
	return value.Redirect, nil
}

func (f OAuthFlow) Exchange(ctx context.Context, code string) (string, error) {
	if f.ClientID == "" || f.ClientSecret == "" || strings.TrimSpace(code) == "" {
		return "", errors.New("OAuth code and client credentials are required")
	}
	endpoint := f.TokenURL
	if endpoint == "" {
		endpoint = "https://github.com/login/oauth/access_token"
	}
	body, _ := json.Marshal(map[string]string{"client_id": f.ClientID, "client_secret": f.ClientSecret, "code": code, "redirect_uri": f.CallbackURL})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	client := f.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil || result.AccessToken == "" {
		return "", fmt.Errorf("GitHub OAuth exchange failed: status %d", response.StatusCode)
	}
	return result.AccessToken, nil
}

type AccessAuthorizer struct {
	HTTP    *http.Client
	BaseURL string
}

func (a AccessAuthorizer) CanRead(ctx context.Context, oauthToken, owner, repo string) (bool, error) {
	if oauthToken == "" || !slug(owner) || !slug(repo) {
		return false, errors.New("OAuth token and valid repository are required")
	}
	base := strings.TrimRight(a.BaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s", base, url.PathEscape(owner), url.PathEscape(repo)), nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("Authorization", "Bearer "+oauthToken)
	request.Header.Set("Accept", "application/vnd.github+json")
	client := a.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode == 200 {
		return true, nil
	}
	if response.StatusCode == 403 || response.StatusCode == 404 {
		return false, nil
	}
	return false, fmt.Errorf("check GitHub repository access: status %d", response.StatusCode)
}

func signState(payload, key []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func bindingHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
func safeRedirect(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && !strings.ContainsAny(value, "\r\n")
}
func httpURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}
func slug(value string) bool {
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
