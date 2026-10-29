package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/url"
	"testing"
	"time"
)

func TestVerifyRequestRejectsTamperingAndReplay(t *testing.T) {
	now := time.Unix(1000, 0)
	body := []byte("command=%2Fpx1")
	timestamp := fmt.Sprint(now.Unix())
	mac := hmac.New(sha256.New, []byte("secret"))
	fmt.Fprintf(mac, "v0:%s:%s", timestamp, body)
	signature := fmt.Sprintf("v0=%x", mac.Sum(nil))
	if err := VerifyRequest([]byte("secret"), timestamp, signature, body, now); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRequest([]byte("secret"), timestamp, signature, append(body, 'x'), now); err == nil {
		t.Fatal("accepted tampered body")
	}
	if err := VerifyRequest([]byte("secret"), timestamp, signature, body, now.Add(10*time.Minute)); err == nil {
		t.Fatal("accepted replayed request")
	}
}

func TestOAuthStateIsTenantBoundAndExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	encoded, err := SignOAuthState(OAuthState{InstallationID: 7, Nonce: "nonce", ExpiresAt: now.Add(time.Minute).Unix()}, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := VerifyOAuthState(encoded, []byte("secret"), now)
	if err != nil || state.InstallationID != 7 {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	if _, err := VerifyOAuthState(encoded, []byte("secret"), now.Add(2*time.Minute)); err == nil {
		t.Fatal("accepted expired state")
	}
}

func TestMessageAndStatusCommand(t *testing.T) {
	message, err := ReviewMessage("acme/api", 12, 1, "Inspect the evidence before merging.", "https://px1.example/review")
	if err != nil || len(message.Blocks) != 3 {
		t.Fatalf("message=%#v err=%v", message, err)
	}
	values := url.Values{"command": {"/px1"}, "text": {"status acme/api #12"}, "response_url": {"https://hooks.slack.com/commands/1"}}
	command, err := ParseStatusCommand([]byte(values.Encode()))
	if err != nil || command.PullNumber != 12 {
		t.Fatalf("command=%#v err=%v", command, err)
	}
}
