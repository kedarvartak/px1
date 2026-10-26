package dashboard

import (
	"testing"
	"time"
)

func TestSessionCSRFAndOrganizationSwitch(t *testing.T) {
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	// NewSession checks the real clock, so use a future expiry and validate at a
	// fixed earlier instant to keep request-boundary assertions deterministic.
	session, err := NewSession("session-1", "user-1", ProviderGitHub, "org-1", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.ValidateCSRF(session.CSRFToken, now); err != nil {
		t.Fatal(err)
	}
	if err := session.ValidateCSRF("wrong", now); err == nil {
		t.Fatal("accepted wrong CSRF token")
	}
	switched, err := session.SwitchOrganization("org-2", []Membership{{OrganizationID: "org-2", Active: true}}, now)
	if err != nil || switched.OrganizationID != "org-2" {
		t.Fatalf("session=%#v err=%v", switched, err)
	}
	if _, err := session.SwitchOrganization("org-3", []Membership{{OrganizationID: "org-3", Active: false}}, now); err == nil {
		t.Fatal("switched to inactive organization")
	}
}

func TestSessionRejectsUnknownProviderAndExpiredRequests(t *testing.T) {
	if _, err := NewSession("s", "u", Provider("other"), "org", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("accepted unsupported OAuth provider")
	}
	session, err := NewSession("s", "u", ProviderGitLab, "org", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.ValidateCSRF(session.CSRFToken, session.ExpiresAt); err == nil {
		t.Fatal("accepted expired session")
	}
}
