package review

import (
	"context"
	"errors"
	"testing"
)

type contextAccess struct {
	installation int64
	denied       string
	err          error
}

func (a *contextAccess) CanReadRepository(_ context.Context, installationID int64, owner, name string) (bool, error) {
	a.installation = installationID
	return owner+"/"+name != a.denied, a.err
}

func TestAuthorizeContextRepositoriesPreservesInstallationBoundary(t *testing.T) {
	access := &contextAccess{}
	got, err := AuthorizeContextRepositories(context.Background(), access, 42, []ContextRepository{{Owner: "Acme", Name: "API"}, {Owner: "acme", Name: "api"}})
	if err != nil || len(got) != 1 || access.installation != 42 {
		t.Fatalf("repositories=%#v installation=%d err=%v", got, access.installation, err)
	}
}

func TestAuthorizeContextRepositoriesFailsClosed(t *testing.T) {
	for name, access := range map[string]*contextAccess{
		"denied": {denied: "acme/private"},
		"error":  {err: errors.New("GitHub unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := AuthorizeContextRepositories(context.Background(), access, 7, []ContextRepository{{Owner: "acme", Name: "private"}}); err == nil {
				t.Fatal("accepted unavailable repository context")
			}
		})
	}
}
