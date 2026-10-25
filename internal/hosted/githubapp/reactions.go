package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ReactionPollInterval = 15 * time.Minute
	ReactionPollWindow   = 14 * 24 * time.Hour
	maxReactionPages     = 10
)

type ReactionComment struct {
	InstallationID  int64
	RepositoryID    int64
	Owner           string
	Repository      string
	CommentID       int64
	ReviewCreatedAt time.Time
}

type ReactionFeedback struct {
	ReactionID int64
	ActorID    int64
	Kind       string
	OccurredAt time.Time
}

type ReactionStore interface {
	DueReactionComments(context.Context, time.Time) ([]ReactionComment, error)
	SaveReactionPoll(context.Context, ReactionComment, []ReactionFeedback, time.Time, bool) error
}

type ReactionPoller struct {
	Client  APIClient
	Store   ReactionStore
	BaseURL string
	Now     func() time.Time
}

func (p ReactionPoller) Run(ctx context.Context) error {
	if p.Client == nil || p.Store == nil {
		return errors.New("GitHub client and reaction store are required")
	}
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now().UTC()
	}
	comments, err := p.Store.DueReactionComments(ctx, now)
	if err != nil {
		return fmt.Errorf("list reaction polls: %w", err)
	}
	for _, comment := range comments {
		if err := validateReactionComment(comment); err != nil {
			return err
		}
		expires := comment.ReviewCreatedAt.Add(ReactionPollWindow)
		if !now.Before(expires) {
			if err := p.Store.SaveReactionPoll(ctx, comment, nil, time.Time{}, true); err != nil {
				return err
			}
			continue
		}
		feedback, err := p.fetch(ctx, comment)
		if err != nil {
			return err
		}
		next := now.Add(ReactionPollInterval)
		if next.After(expires) {
			next = expires
		}
		if err := p.Store.SaveReactionPoll(ctx, comment, feedback, next, false); err != nil {
			return fmt.Errorf("save reaction poll: %w", err)
		}
	}
	return nil
}

func (p ReactionPoller) fetch(ctx context.Context, comment ReactionComment) ([]ReactionFeedback, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	var feedback []ReactionFeedback
	for page := 1; page <= maxReactionPages; page++ {
		endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/comments/%d/reactions?per_page=100&page=%d", baseURL, url.PathEscape(comment.Owner), url.PathEscape(comment.Repository), comment.CommentID, page)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		response, err := p.Client.Do(ctx, comment.InstallationID, request)
		if err != nil {
			return nil, fmt.Errorf("list GitHub reactions: %w", err)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			return nil, fmt.Errorf("list GitHub reactions: status %d", response.StatusCode)
		}
		var reactions []struct {
			ID        int64     `json:"id"`
			Content   string    `json:"content"`
			CreatedAt time.Time `json:"created_at"`
			User      struct {
				ID int64 `json:"id"`
			} `json:"user"`
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&reactions)
		_ = response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode GitHub reactions: %w", err)
		}
		for _, reaction := range reactions {
			kind := ""
			if reaction.Content == "+1" {
				kind = "thumbs_up"
			}
			if reaction.Content == "-1" {
				kind = "thumbs_down"
			}
			if kind != "" && reaction.ID > 0 && reaction.User.ID > 0 && !reaction.CreatedAt.IsZero() {
				feedback = append(feedback, ReactionFeedback{ReactionID: reaction.ID, ActorID: reaction.User.ID, Kind: kind, OccurredAt: reaction.CreatedAt})
			}
		}
		if len(reactions) < 100 {
			break
		}
		if page == maxReactionPages {
			return nil, errors.New("GitHub reaction result exceeded bounded page limit")
		}
	}
	return feedback, nil
}

func validateReactionComment(comment ReactionComment) error {
	if comment.InstallationID <= 0 || comment.RepositoryID <= 0 || comment.CommentID <= 0 || !validSlug(comment.Owner) || !validSlug(comment.Repository) || comment.ReviewCreatedAt.IsZero() {
		return errors.New("reaction comment requires tenant, repository, comment, and review time")
	}
	return nil
}
