package githubapp

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

type ExplicitMemory struct {
	InstallationID int64
	RepositoryID   int64
	FindingID      string
	ThreadID       string
	ActorID        int64
	Text           string
	EvidenceURL    string
	CreatedAt      time.Time
}

type ExplicitMemoryStore interface {
	WriteExplicitMemory(context.Context, ExplicitMemory) error
}

type RememberHandler struct {
	Store ExplicitMemoryStore
	Now   func() time.Time
}

// Handle writes only an already-authorized remember command. Callers must use
// CommandAuthorizer first so read-only repository users cannot create memory.
func (h RememberHandler) Handle(ctx context.Context, command Command, input ExplicitMemory) error {
	if h.Store == nil || command.Name != "remember" || strings.TrimSpace(command.Text) == "" {
		return errors.New("authorized remember command and memory store are required")
	}
	if input.InstallationID <= 0 || input.RepositoryID <= 0 || input.FindingID == "" || input.ThreadID == "" || input.ActorID <= 0 || input.EvidenceURL == "" {
		return errors.New("remember requires tenant-scoped thread evidence")
	}
	evidenceURL, err := url.Parse(input.EvidenceURL)
	if err != nil || evidenceURL.Scheme != "https" || !strings.EqualFold(evidenceURL.Host, "github.com") {
		return errors.New("remember evidence must be an HTTPS GitHub URL")
	}
	input.Text = command.Text
	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now().UTC()
	}
	input.CreatedAt = now
	return h.Store.WriteExplicitMemory(ctx, input)
}
