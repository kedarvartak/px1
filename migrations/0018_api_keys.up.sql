BEGIN;
CREATE TABLE api_keys (
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    id text NOT NULL CHECK (id ~ '^[0-9a-f]{16}$'),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    scopes text[] NOT NULL CHECK (cardinality(scopes) > 0 AND scopes <@ ARRAY['reviews:read', 'reviews:trigger', 'findings:read', 'findings:write', 'codebase:read', 'rules:read', 'memories:read']::text[]),
    secret_sha256 bytea NOT NULL CHECK (length(secret_sha256) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    replaced_by text,
    PRIMARY KEY (installation_id, id),
    FOREIGN KEY (installation_id, replaced_by) REFERENCES api_keys(installation_id, id),
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '366 days')
);
ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON api_keys USING (installation_id = current_setting('px1.installation_id', true)::bigint) WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);
COMMIT;
