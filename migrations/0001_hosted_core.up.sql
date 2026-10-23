BEGIN;

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE installations (
    id bigint PRIMARY KEY,
    account_login text NOT NULL,
    account_type text NOT NULL,
    suspended_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE repositories (
    id bigint PRIMARY KEY,
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    owner text NOT NULL,
    name text NOT NULL,
    default_branch text NOT NULL,
    private boolean NOT NULL,
    indexing_state text NOT NULL DEFAULT 'pending' CHECK (indexing_state IN ('pending', 'indexing', 'ready', 'partial', 'failed')),
    indexed_head_sha char(40),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (installation_id, owner, name),
    UNIQUE (installation_id, id)
);

CREATE TABLE files (
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint NOT NULL,
    path text NOT NULL,
    blob_sha char(40) NOT NULL,
    language text,
    summary text,
    generated boolean NOT NULL DEFAULT false,
    indexed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, path),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE
);

CREATE TABLE chunks (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint NOT NULL,
    path text NOT NULL,
    blob_sha char(40) NOT NULL,
    symbol text,
    start_line integer NOT NULL CHECK (start_line > 0),
    end_line integer NOT NULL CHECK (end_line >= start_line),
    content text NOT NULL,
    token_count integer NOT NULL CHECK (token_count >= 0),
    embedding_model text NOT NULL,
    embedding vector(1536) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (repository_id, path, blob_sha, start_line, end_line, embedding_model),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE
);

CREATE INDEX chunks_embedding_hnsw ON chunks USING hnsw (embedding vector_cosine_ops);
CREATE INDEX chunks_repository_path ON chunks (repository_id, path);

CREATE TABLE pull_requests (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint NOT NULL,
    number integer NOT NULL CHECK (number > 0),
    base_sha char(40) NOT NULL,
    head_sha char(40) NOT NULL,
    author_login text NOT NULL,
    state text NOT NULL CHECK (state IN ('open', 'closed', 'merged')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (repository_id, number),
    UNIQUE (installation_id, id),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE
);

CREATE TABLE reviews (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint NOT NULL,
    pull_request_id bigint NOT NULL,
    base_sha char(40) NOT NULL,
    head_sha char(40) NOT NULL,
    status text NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
    verifier_status text NOT NULL DEFAULT 'pending' CHECK (verifier_status IN ('pending', 'passed', 'failed', 'partial')),
    model text,
    prompt_version text,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    cost_microusd bigint NOT NULL DEFAULT 0,
    started_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (pull_request_id, head_sha),
    UNIQUE (installation_id, id),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE,
    FOREIGN KEY (installation_id, pull_request_id) REFERENCES pull_requests(installation_id, id) ON DELETE CASCADE
);

CREATE TABLE findings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint NOT NULL,
    review_id uuid NOT NULL,
    path text NOT NULL,
    start_line integer NOT NULL CHECK (start_line > 0),
    end_line integer NOT NULL CHECK (end_line >= start_line),
    severity text NOT NULL CHECK (severity IN ('P0', 'P1', 'P2')),
    finding_type text NOT NULL,
    title text NOT NULL,
    body text NOT NULL,
    evidence text NOT NULL,
    suggestion text,
    confidence real NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    rule_id text,
    fingerprint text NOT NULL,
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'dismissed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (review_id, fingerprint),
    UNIQUE (installation_id, id),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE,
    FOREIGN KEY (installation_id, review_id) REFERENCES reviews(installation_id, id) ON DELETE CASCADE
);

CREATE TABLE comments (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint NOT NULL,
    finding_id uuid,
    github_comment_id bigint NOT NULL,
    github_thread_id text,
    state text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'resolved', 'deleted')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (repository_id, github_comment_id),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE,
    FOREIGN KEY (installation_id, finding_id) REFERENCES findings(installation_id, id)
);

CREATE TABLE feedback_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint NOT NULL,
    finding_id uuid,
    github_actor_id bigint,
    kind text NOT NULL CHECK (kind IN ('thumbs_up', 'thumbs_down', 'reply', 'resolved', 'line_changed')),
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE,
    FOREIGN KEY (installation_id, finding_id) REFERENCES findings(installation_id, id)
);

CREATE TABLE jobs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint,
    delivery_id text,
    kind text NOT NULL,
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
    args jsonb NOT NULL DEFAULT '{}'::jsonb,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (installation_id, delivery_id),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE
);

CREATE INDEX repositories_installation ON repositories (installation_id);
CREATE INDEX pull_requests_repository ON pull_requests (repository_id, updated_at DESC);
CREATE INDEX reviews_pull_request ON reviews (pull_request_id, created_at DESC);
CREATE INDEX findings_review_status ON findings (review_id, status, severity);
CREATE INDEX feedback_events_finding ON feedback_events (finding_id, occurred_at DESC);
CREATE INDEX jobs_ready ON jobs (state, available_at) WHERE state = 'queued';

ALTER TABLE installations ENABLE ROW LEVEL SECURITY;
ALTER TABLE installations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_installations ON installations
    USING (id = current_setting('px1.installation_id', true)::bigint);

DO $policies$
DECLARE table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY['repositories', 'files', 'chunks', 'pull_requests', 'reviews', 'findings', 'comments', 'feedback_events', 'jobs']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I USING (installation_id = current_setting(''px1.installation_id'', true)::bigint) WITH CHECK (installation_id = current_setting(''px1.installation_id'', true)::bigint)',
            table_name
        );
    END LOOP;
END
$policies$;

COMMIT;
