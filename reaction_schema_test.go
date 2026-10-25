package main

import (
	"os"
	"strings"
	"testing"
)

func TestReactionFeedbackHasTenantScopedIdempotencyKey(t *testing.T) {
	body, err := os.ReadFile("migrations/0006_feedback_reactions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, want := range []string{"ADD COLUMN source_id bigint", "reaction_poll_after timestamptz", "reaction_poll_expires_at timestamptz", "comments_reaction_poll_ready", "UNIQUE INDEX feedback_events_reaction_source", "installation_id, repository_id, kind, source_id", "thumbs_up", "thumbs_down"} {
		if !strings.Contains(sql, want) {
			t.Errorf("reaction migration missing %q", want)
		}
	}
}
