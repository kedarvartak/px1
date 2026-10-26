// Package billing contains hosted billing and programme contracts.
package billing

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

type OSSStatus string

const (
	OSSSubmitted OSSStatus = "submitted"
	OSSApproved  OSSStatus = "approved"
	OSSRejected  OSSStatus = "rejected"
)

type OSSApplication struct {
	ID             string
	InstallationID int64
	RepositoryID   int64
	RepositoryURL  string
	ContactEmail   string
	Reason         string
	Status         OSSStatus
	SubmittedAt    time.Time
}

type OSSDecision struct {
	ApplicationID, Actor, Reason string
	Status                       OSSStatus
	CreditGrant                  int64
}

type OSSStore interface {
	Submit(context.Context, OSSApplication) (OSSApplication, bool, error)
	// Decide atomically records the audit decision and any approved credit grant.
	Decide(context.Context, int64, OSSDecision, OSSStatus) (OSSApplication, error)
}

type OSSProgramme struct {
	Store           OSSStore
	ApprovalCredits int64
}

func (p OSSProgramme) Apply(ctx context.Context, application OSSApplication) (OSSApplication, bool, error) {
	if p.Store == nil || application.InstallationID <= 0 || application.RepositoryID <= 0 || application.ContactEmail == "" || strings.TrimSpace(application.Reason) == "" || !publicHTTPSRepository(application.RepositoryURL) {
		return OSSApplication{}, false, errors.New("tenant, public HTTPS repository, contact, reason, and store are required")
	}
	application.Status = OSSSubmitted
	return p.Store.Submit(ctx, application)
}

func (p OSSProgramme) Decide(ctx context.Context, installationID int64, applicationID, actor string, approve bool, reason string) (OSSApplication, error) {
	if p.Store == nil || installationID <= 0 || applicationID == "" || actor == "" || strings.TrimSpace(reason) == "" {
		return OSSApplication{}, errors.New("tenant, application, actor, reason, and store are required")
	}
	decision := OSSDecision{ApplicationID: applicationID, Actor: actor, Reason: reason, Status: OSSRejected}
	if approve {
		if p.ApprovalCredits <= 0 {
			return OSSApplication{}, errors.New("positive approval credits are required")
		}
		decision.Status, decision.CreditGrant = OSSApproved, p.ApprovalCredits
	}
	return p.Store.Decide(ctx, installationID, decision, OSSSubmitted)
}

func publicHTTPSRepository(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}
