package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const MaxContextRepositories = 10

type ContextRepository struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type InstallationRepositoryAccess interface {
	CanReadRepository(context.Context, int64, string, string) (bool, error)
}

// AuthorizeContextRepositories fails closed: callers must not silently review
// with partial cross-repository context that configuration said was required.
func AuthorizeContextRepositories(ctx context.Context, access InstallationRepositoryAccess, installationID int64, repositories []ContextRepository) ([]ContextRepository, error) {
	if access == nil || installationID <= 0 {
		return nil, errors.New("installation repository access is required")
	}
	if len(repositories) > MaxContextRepositories {
		return nil, fmt.Errorf("context.repos exceeds %d repositories", MaxContextRepositories)
	}
	seen := map[string]bool{}
	result := make([]ContextRepository, 0, len(repositories))
	for _, repository := range repositories {
		if !repositorySlug(repository.Owner) || !repositorySlug(repository.Name) {
			return nil, errors.New("context.repos contains an invalid repository")
		}
		key := strings.ToLower(repository.Owner + "/" + repository.Name)
		if seen[key] {
			continue
		}
		allowed, err := access.CanReadRepository(ctx, installationID, repository.Owner, repository.Name)
		if err != nil {
			return nil, fmt.Errorf("authorize context repository %s: %w", key, err)
		}
		if !allowed {
			return nil, fmt.Errorf("installation cannot read context repository %s", key)
		}
		seen[key] = true
		result = append(result, repository)
	}
	return result, nil
}

func repositorySlug(value string) bool {
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
