BEGIN;

CREATE TABLE weekly_repository_metrics (
    installation_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    week_start date NOT NULL CHECK (extract(isodow FROM week_start) = 1),
    pull_requests integer NOT NULL CHECK (pull_requests >= 0),
    comments integer NOT NULL CHECK (comments >= 0),
    comments_per_pr double precision NOT NULL CHECK (comments_per_pr >= 0),
    addressed_rate double precision NOT NULL CHECK (addressed_rate BETWEEN 0 AND 1),
    downvote_rate double precision NOT NULL CHECK (downvote_rate BETWEEN 0 AND 1),
    suppressed integer NOT NULL CHECK (suppressed >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (installation_id, repository_id, week_start),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE
);

ALTER TABLE weekly_repository_metrics ENABLE ROW LEVEL SECURITY;
ALTER TABLE weekly_repository_metrics FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON weekly_repository_metrics
    USING (installation_id = current_setting('px1.installation_id', true)::bigint)
    WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);

COMMIT;
