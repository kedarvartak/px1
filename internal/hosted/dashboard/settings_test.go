package dashboard

import (
	"context"
	"strings"
	"testing"
)

type settingsWriterStub struct {
	database bool
	path     string
	body     string
}

func (s *settingsWriterStub) WriteDatabaseSettings(context.Context, int64, int64, ReviewSettings) error {
	s.database = true
	return nil
}
func (s *settingsWriterStub) OpenConfigPullRequest(_ context.Context, _, _ int64, path string, body []byte) error {
	s.path, s.body = path, string(body)
	return nil
}

func pointer[T any](value T) *T { return &value }

func TestRepositorySettingsOverrideDatabasePerField(t *testing.T) {
	got, err := ResolveSettings(ReviewSettings{Strictness: pointer(1), MaxInlineComments: pointer(20)}, ReviewSettings{Strictness: pointer(3)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Strictness != 3 || got.MaxInlineComments != 20 || got.Sources["strictness"] != "repository" || got.Sources["maxInlineComments"] != "database" {
		t.Fatalf("settings = %#v", got)
	}
}

func TestSaveSettingsUsesExplicitDestination(t *testing.T) {
	writer := &settingsWriterStub{}
	settings := ReviewSettings{Strictness: pointer(2), AutoReview: pointer(true)}
	if err := SaveSettings(context.Background(), writer, 1, 2, SettingsPullRequest, settings); err != nil {
		t.Fatal(err)
	}
	if writer.database || writer.path != ".px1/config.json" || !strings.Contains(writer.body, `"version": 1`) {
		t.Fatalf("writer = %#v", writer)
	}
	if err := SaveSettings(context.Background(), writer, 1, 2, SettingsDestination("direct"), settings); err == nil {
		t.Fatal("accepted unsupported destination")
	}
}
