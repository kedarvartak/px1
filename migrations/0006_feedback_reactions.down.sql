BEGIN;

DROP INDEX IF EXISTS feedback_events_reaction_source;
DROP INDEX IF EXISTS comments_reaction_poll_ready;
ALTER TABLE comments
    DROP COLUMN reaction_poll_complete,
    DROP COLUMN reaction_poll_expires_at,
    DROP COLUMN reaction_poll_after;
ALTER TABLE feedback_events DROP COLUMN source_id;

COMMIT;
