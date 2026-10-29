// Package slack implements authenticated Slack OAuth, messages, and commands.
package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const MaxSlackBodyBytes = 1 << 20

func VerifyRequest(secret []byte, timestamp, signature string, body []byte, now time.Time) error {
	if len(secret) == 0 || len(body) > MaxSlackBodyBytes || !strings.HasPrefix(signature, "v0=") {
		return errors.New("Slack request signature is invalid")
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || now.Sub(time.Unix(seconds, 0)) > 5*time.Minute || time.Unix(seconds, 0).Sub(now) > 5*time.Minute {
		return errors.New("Slack request timestamp is outside tolerance")
	}
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "v0:%s:%s", timestamp, body)
	want, err := hex.DecodeString(strings.TrimPrefix(signature, "v0="))
	if err != nil || !hmac.Equal(want, mac.Sum(nil)) {
		return errors.New("Slack request signature is invalid")
	}
	return nil
}

type OAuthState struct {
	InstallationID int64  `json:"installation_id"`
	Nonce          string `json:"nonce"`
	ExpiresAt      int64  `json:"expires_at"`
}

func SignOAuthState(state OAuthState, secret []byte) (string, error) {
	if state.InstallationID <= 0 || state.Nonce == "" || state.ExpiresAt <= 0 || len(secret) == 0 {
		return "", errors.New("tenant, nonce, expiry, and state secret are required")
	}
	payload, _ := json.Marshal(state)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func VerifyOAuthState(value string, secret []byte, now time.Time) (OAuthState, error) {
	encoded, signature, ok := strings.Cut(value, ".")
	if !ok || len(secret) == 0 {
		return OAuthState{}, errors.New("Slack OAuth state is malformed")
	}
	want, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return OAuthState{}, errors.New("Slack OAuth state signature is malformed")
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(encoded))
	if !hmac.Equal(want, mac.Sum(nil)) {
		return OAuthState{}, errors.New("Slack OAuth state signature is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return OAuthState{}, errors.New("Slack OAuth state payload is malformed")
	}
	var state OAuthState
	if json.Unmarshal(payload, &state) != nil || state.InstallationID <= 0 || state.Nonce == "" || now.Unix() > state.ExpiresAt {
		return OAuthState{}, errors.New("Slack OAuth state is invalid or expired")
	}
	return state, nil
}

type OAuthGrant struct {
	TeamID, TeamName, BotToken string
	Scopes                     []string
}
type OAuthClient interface {
	ExchangeOAuthV2(context.Context, string) (OAuthGrant, error)
}
type InstallationStore interface {
	SaveSlackInstallation(context.Context, int64, OAuthGrant, string) error
}

func CompleteOAuth(ctx context.Context, client OAuthClient, store InstallationStore, code, signedState string, stateSecret []byte, now time.Time) error {
	if client == nil || store == nil || code == "" {
		return errors.New("Slack OAuth client, store, and code are required")
	}
	state, err := VerifyOAuthState(signedState, stateSecret, now)
	if err != nil {
		return err
	}
	grant, err := client.ExchangeOAuthV2(ctx, code)
	if err != nil {
		return err
	}
	if grant.TeamID == "" || grant.BotToken == "" {
		return errors.New("Slack OAuth grant is incomplete")
	}
	granted := map[string]bool{}
	for _, scope := range grant.Scopes {
		granted[scope] = true
	}
	if !granted["chat:write"] || !granted["commands"] {
		return errors.New("Slack OAuth grant is missing chat:write or commands")
	}
	return store.SaveSlackInstallation(ctx, state.InstallationID, grant, state.Nonce)
}

type Text struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type Block struct {
	Type   string `json:"type"`
	Text   *Text  `json:"text,omitempty"`
	Fields []Text `json:"fields,omitempty"`
}
type Message struct {
	Text   string  `json:"text"`
	Blocks []Block `json:"blocks"`
}

func ReviewMessage(repository string, pull int, p0 int, summary, reviewURL string) (Message, error) {
	if repository == "" || len(repository) > 200 || pull <= 0 || p0 < 0 || summary == "" || len(summary) > 2800 || !strings.HasPrefix(reviewURL, "https://") {
		return Message{}, errors.New("repository, pull request, summary, and HTTPS review URL are required")
	}
	heading := fmt.Sprintf("px1 review: %s !%d", repository, pull)
	if p0 > 0 {
		heading = fmt.Sprintf("🚨 %d P0 finding(s): %s !%d", p0, repository, pull)
	}
	return Message{Text: heading, Blocks: []Block{{Type: "header", Text: &Text{Type: "plain_text", Text: heading}}, {Type: "section", Text: &Text{Type: "mrkdwn", Text: summary}}, {Type: "section", Text: &Text{Type: "mrkdwn", Text: "<" + reviewURL + "|Open commit-pinned review>"}}}}, nil
}

type StatusCommand struct {
	ResponseURL, Repository string
	PullNumber              int
}

func ParseStatusCommand(body []byte) (StatusCommand, error) {
	if len(body) == 0 || len(body) > MaxSlackBodyBytes {
		return StatusCommand{}, errors.New("Slack command body is empty or too large")
	}
	values, err := url.ParseQuery(string(body))
	if err != nil || values.Get("command") != "/px1" {
		return StatusCommand{}, errors.New("unsupported Slack command")
	}
	parts := strings.Fields(values.Get("text"))
	if len(parts) != 3 || parts[0] != "status" {
		return StatusCommand{}, errors.New("usage: /px1 status owner/repo PR")
	}
	pull, err := strconv.Atoi(strings.TrimPrefix(parts[2], "#"))
	if err != nil || pull <= 0 || !strings.Contains(parts[1], "/") {
		return StatusCommand{}, errors.New("usage: /px1 status owner/repo PR")
	}
	responseURL := values.Get("response_url")
	if !strings.HasPrefix(responseURL, "https://hooks.slack.com/") {
		return StatusCommand{}, errors.New("Slack response URL is invalid")
	}
	return StatusCommand{ResponseURL: responseURL, Repository: parts[1], PullNumber: pull}, nil
}
