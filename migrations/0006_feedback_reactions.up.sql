BEGIN;

ALTER TABLE feedback_events
    ADD COLUMN source_id bigint;

ALTER TABLE comments
    ADD COLUMN reaction_poll_after timestamptz,
    ADD COLUMN reaction_poll_expires_at timestamptz,
    ADD COLUMN reaction_poll_complete boolean NOT NULL DEFAULT false;

CREATE UNIQUE INDEX feedback_events_reaction_source
    ON feedback_events (installation_id, repository_id, kind, source_id)
    WHERE source_id IS NOT NULL AND kind IN ('thumbs_up', 'thumbs_down');

CREATE INDEX comments_reaction_poll_ready
    ON comments (reaction_poll_after)
    WHERE reaction_poll_complete = false AND reaction_poll_after IS NOT NULL;

COMMIT;
