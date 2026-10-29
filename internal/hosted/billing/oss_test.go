package billing

import (
	"context"
	"errors"
	"testing"
)

type ossStore struct {
	application OSSApplication
	decisions   int
}

func (s *ossStore) Submit(_ context.Context, app OSSApplication) (OSSApplication, bool, error) {
	s.application = app
	return app, true, nil
}
func (s *ossStore) Decide(_ context.Context, tenant int64, decision OSSDecision, expected OSSStatus) (OSSApplication, error) {
	if s.application.InstallationID != tenant || s.application.Status != expected {
		return OSSApplication{}, errors.New("stale or cross-tenant decision")
	}
	s.decisions++
	s.application.Status = decision.Status
	return s.application, nil
}

func TestOSSProgrammeApplicationAndApproval(t *testing.T) {
	store := &ossStore{}
	programme := OSSProgramme{Store: store, ApprovalCredits: 1000}
	application, created, err := programme.Apply(context.Background(), OSSApplication{ID: "app-1", InstallationID: 1, RepositoryID: 2, RepositoryURL: "https://github.com/acme/project", ContactEmail: "maintainer@example.com", Reason: "public project"})
	if err != nil || !created || application.Status != OSSSubmitted {
		t.Fatalf("application=%#v created=%v err=%v", application, created, err)
	}
	approved, err := programme.Decide(context.Background(), 1, "app-1", "admin@example.com", true, "eligible public project")
	if err != nil || approved.Status != OSSApproved || store.decisions != 1 {
		t.Fatalf("approved=%#v err=%v", approved, err)
	}
	if _, err := programme.Decide(context.Background(), 1, "app-1", "admin@example.com", false, "changed mind"); err == nil {
		t.Fatal("allowed a second terminal decision")
	}
}

func TestOSSProgrammeRejectsUnverifiableRepository(t *testing.T) {
	_, _, err := (OSSProgramme{Store: &ossStore{}}).Apply(context.Background(), OSSApplication{InstallationID: 1, RepositoryID: 2, RepositoryURL: "http://localhost/repo", ContactEmail: "x@y.z", Reason: "test"})
	if err == nil {
		t.Fatal("accepted non-HTTPS repository evidence")
	}
}
