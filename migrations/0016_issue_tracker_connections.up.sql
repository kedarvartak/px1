BEGIN;
CREATE TABLE issue_tracker_connections (
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('linear', 'jira')),
    account_id text NOT NULL CHECK (length(account_id) BETWEEN 1 AND 200),
    base_url text NOT NULL DEFAULT '',
    encrypted_access_token bytea NOT NULL,
    encrypted_refresh_token bytea,
    expires_at timestamptz,
    connected_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (installation_id, provider, account_id)
);
ALTER TABLE issue_tracker_connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE issue_tracker_connections FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON issue_tracker_connections USING (installation_id = current_setting('px1.installation_id', true)::bigint) WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);
COMMIT;
