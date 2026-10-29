BEGIN;
CREATE TABLE slack_installations (
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    team_id text NOT NULL CHECK (length(team_id) BETWEEN 1 AND 100),
    team_name text NOT NULL DEFAULT '',
    encrypted_bot_token bytea NOT NULL,
    scopes text[] NOT NULL DEFAULT '{}',
    oauth_nonce text NOT NULL CHECK (length(oauth_nonce) BETWEEN 1 AND 200),
    installed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (installation_id, team_id),
    UNIQUE (installation_id, oauth_nonce)
);
ALTER TABLE slack_installations ENABLE ROW LEVEL SECURITY;
ALTER TABLE slack_installations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON slack_installations USING (installation_id = current_setting('px1.installation_id', true)::bigint) WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);
COMMIT;
