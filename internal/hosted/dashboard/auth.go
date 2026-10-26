// Package dashboard defines provider-neutral contracts for the hosted UI.
package dashboard

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

type Provider string

const (
	ProviderGitHub Provider = "github"
	ProviderGitLab Provider = "gitlab"
)

type Session struct {
	ID             string
	UserID         string
	Provider       Provider
	OrganizationID string
	CSRFToken      string
	ExpiresAt      time.Time
}

type Membership struct {
	OrganizationID string
	Active         bool
}

func NewSession(id, userID string, provider Provider, organizationID string, expiresAt time.Time) (Session, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(userID) == "" || !validProvider(provider) || strings.TrimSpace(organizationID) == "" || !expiresAt.After(time.Now()) {
		return Session{}, errors.New("valid session identity, provider, organization, and expiry are required")
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return Session{}, err
	}
	return Session{ID: id, UserID: userID, Provider: provider, OrganizationID: organizationID, CSRFToken: base64.RawURLEncoding.EncodeToString(token), ExpiresAt: expiresAt.UTC()}, nil
}

func (s Session) ValidateCSRF(token string, now time.Time) error {
	if s.ID == "" || !now.Before(s.ExpiresAt) || token == "" || !hmac.Equal([]byte(s.CSRFToken), []byte(token)) {
		return errors.New("invalid or expired session request")
	}
	return nil
}

func (s Session) SwitchOrganization(organizationID string, memberships []Membership, now time.Time) (Session, error) {
	if s.ID == "" || !now.Before(s.ExpiresAt) || organizationID == "" {
		return Session{}, errors.New("active session and organization are required")
	}
	for _, membership := range memberships {
		if membership.Active && hmac.Equal([]byte(membership.OrganizationID), []byte(organizationID)) {
			s.OrganizationID = organizationID
			return s, nil
		}
	}
	return Session{}, errors.New("user is not an active organization member")
}

func validProvider(provider Provider) bool {
	return provider == ProviderGitHub || provider == ProviderGitLab
}
