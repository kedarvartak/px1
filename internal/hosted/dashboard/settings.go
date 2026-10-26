package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type ReviewSettings struct {
	Strictness        *int  `json:"strictness,omitempty"`
	AutoReview        *bool `json:"autoReview,omitempty"`
	MaxInlineComments *int  `json:"maxInlineComments,omitempty"`
}

type EffectiveSettings struct {
	Strictness        int
	AutoReview        bool
	MaxInlineComments int
	Sources           map[string]string
}

type SettingsDestination string

const (
	SettingsDatabase    SettingsDestination = "database"
	SettingsPullRequest SettingsDestination = "pull_request"
)

type SettingsWriter interface {
	WriteDatabaseSettings(context.Context, int64, int64, ReviewSettings) error
	OpenConfigPullRequest(context.Context, int64, int64, string, []byte) error
}

// ResolveSettings applies hosted defaults first and repository-owned config
// last. A repository value is therefore authoritative for every field it sets.
func ResolveSettings(database, repository ReviewSettings) (EffectiveSettings, error) {
	if err := validateSettings(database); err != nil {
		return EffectiveSettings{}, fmt.Errorf("database settings: %w", err)
	}
	if err := validateSettings(repository); err != nil {
		return EffectiveSettings{}, fmt.Errorf("repository settings: %w", err)
	}
	result := EffectiveSettings{Strictness: 2, AutoReview: true, MaxInlineComments: 15, Sources: map[string]string{}}
	applySettings(&result, database, "database")
	applySettings(&result, repository, "repository")
	return result, nil
}

func SaveSettings(ctx context.Context, writer SettingsWriter, installationID, repositoryID int64, destination SettingsDestination, settings ReviewSettings) error {
	if writer == nil || installationID <= 0 || repositoryID <= 0 {
		return errors.New("settings writer and tenant are required")
	}
	if err := validateSettings(settings); err != nil {
		return err
	}
	switch destination {
	case SettingsDatabase:
		return writer.WriteDatabaseSettings(ctx, installationID, repositoryID, settings)
	case SettingsPullRequest:
		body, err := json.MarshalIndent(struct {
			Version int `json:"version"`
			ReviewSettings
		}{Version: 1, ReviewSettings: settings}, "", "  ")
		if err != nil {
			return err
		}
		return writer.OpenConfigPullRequest(ctx, installationID, repositoryID, ".px1/config.json", append(body, '\n'))
	default:
		return errors.New("unsupported settings destination")
	}
}

func applySettings(result *EffectiveSettings, input ReviewSettings, source string) {
	if input.Strictness != nil {
		result.Strictness, result.Sources["strictness"] = *input.Strictness, source
	}
	if input.AutoReview != nil {
		result.AutoReview, result.Sources["autoReview"] = *input.AutoReview, source
	}
	if input.MaxInlineComments != nil {
		result.MaxInlineComments, result.Sources["maxInlineComments"] = *input.MaxInlineComments, source
	}
}

func validateSettings(settings ReviewSettings) error {
	if settings.Strictness != nil && (*settings.Strictness < 1 || *settings.Strictness > 3) {
		return errors.New("strictness must be between 1 and 3")
	}
	if settings.MaxInlineComments != nil && (*settings.MaxInlineComments < 1 || *settings.MaxInlineComments > 50) {
		return errors.New("max inline comments must be between 1 and 50")
	}
	return nil
}
