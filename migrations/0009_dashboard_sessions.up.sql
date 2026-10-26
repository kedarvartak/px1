BEGIN;

CREATE TABLE dashboard_users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider text NOT NULL CHECK (provider IN ('github', 'gitlab')),
    provider_user_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_user_id)
);

CREATE TABLE dashboard_memberships (
    user_id uuid NOT NULL REFERENCES dashboard_users(id) ON DELETE CASCADE,
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, installation_id)
);

CREATE TABLE dashboard_sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id uuid NOT NULL,
    installation_id bigint NOT NULL,
    csrf_hash bytea NOT NULL CHECK (octet_length(csrf_hash) = 32),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, installation_id) REFERENCES dashboard_memberships(user_id, installation_id) ON DELETE CASCADE
);

CREATE INDEX dashboard_sessions_expiry ON dashboard_sessions (expires_at);

COMMIT;
