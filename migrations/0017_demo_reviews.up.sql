BEGIN;
CREATE TABLE demo_reviews (
    installation_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    pull_number integer NOT NULL CHECK (pull_number > 0),
    head_sha text NOT NULL CHECK (head_sha ~ '^[0-9a-f]{40}$'),
    source_url text NOT NULL CHECK (source_url LIKE 'https://%'),
    provenance text NOT NULL CHECK (length(provenance) BETWEEN 1 AND 1000),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    PRIMARY KEY (installation_id, repository_id, head_sha),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE,
    CHECK ((status IN ('queued', 'running') AND completed_at IS NULL) OR (status IN ('completed', 'failed') AND completed_at IS NOT NULL))
);
ALTER TABLE demo_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE demo_reviews FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON demo_reviews USING (installation_id = current_setting('px1.installation_id', true)::bigint) WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);
COMMIT;
