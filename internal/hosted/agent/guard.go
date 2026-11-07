package agent

import (
	"context"
	"errors"
	"path"
)

// Guarded wraps a GitRepo with the privacy rules of a run. A model can ask to
// read any file at the commit, not only those in the diff, so excluding a path
// from the diff is not enough: the same policy has to hold for every tool.
//
//   - Allow decides whether a path may be shown to the model at all.
//   - Redact rewrites text before it is returned, for secret-like values that
//     appear in files that are otherwise allowed.
//
// Redaction is a safety net, not access control: it recognizes known secret
// shapes and nothing else.
type Guarded struct {
	Repo   GitRepo
	Allow  func(path string) (allowed bool, reason string)
	Redact func(text string) string
}

var errExcluded = errors.New("path is excluded from model input")

func (g Guarded) check(p string) error {
	clean, err := repositoryPath(p)
	if err != nil {
		return err
	}
	if g.Allow != nil {
		if allowed, _ := g.Allow(clean); !allowed {
			return errExcluded
		}
	}
	return nil
}

func (g Guarded) redact(text string) string {
	if g.Redact == nil {
		return text
	}
	return g.Redact(text)
}

func (g Guarded) ReadLines(ctx context.Context, scope Scope, p string, start, end int) (string, error) {
	if err := g.check(p); err != nil {
		return "", err
	}
	text, err := g.Repo.ReadLines(ctx, scope, p, start, end)
	return g.redact(text), err
}

// Search over-fetches because excluded paths are filtered after the search,
// and returns at most limit allowed hits.
func (g Guarded) Search(ctx context.Context, scope Scope, query string, limit int) ([]SearchHit, error) {
	hits, err := g.Repo.Search(ctx, scope, query, min(limit*4, 4*MaxSearchResults))
	if err != nil {
		return nil, err
	}
	allowed := make([]SearchHit, 0, len(hits))
	for _, hit := range hits {
		if g.check(hit.Path) != nil {
			continue
		}
		hit.Snippet = g.redact(hit.Snippet)
		if allowed = append(allowed, hit); len(allowed) >= limit {
			break
		}
	}
	return allowed, nil
}

func (g Guarded) Blame(ctx context.Context, scope Scope, p string, start, end int) ([]BlameLine, error) {
	if err := g.check(p); err != nil {
		return nil, err
	}
	lines, err := g.Repo.Blame(ctx, scope, p, start, end)
	for i := range lines {
		lines[i].Summary = g.redact(lines[i].Summary)
	}
	return lines, err
}

func (g Guarded) Log(ctx context.Context, scope Scope, p string, limit int) ([]Commit, error) {
	if err := g.check(p); err != nil {
		return nil, err
	}
	commits, err := g.Repo.Log(ctx, scope, p, limit)
	for i := range commits {
		commits[i].Summary = g.redact(commits[i].Summary)
	}
	return commits, err
}

// ListFiles hides excluded files, so a model is not even told they exist.
// Directories are always listed; their contents are filtered when listed.
func (g Guarded) ListFiles(ctx context.Context, scope Scope, directory string) ([]string, error) {
	entries, err := g.Repo.ListFiles(ctx, scope, directory)
	if err != nil {
		return nil, err
	}
	visible := make([]string, 0, len(entries))
	for _, entry := range entries {
		if len(entry) > 0 && entry[len(entry)-1] != '/' && entry[0] != '[' && g.check(path.Join(directory, entry)) != nil {
			continue
		}
		visible = append(visible, entry)
	}
	return visible, nil
}
