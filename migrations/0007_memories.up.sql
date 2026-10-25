BEGIN;

CREATE TABLE memories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint,
    scope text NOT NULL CHECK (scope IN ('organization', 'repository', 'path', 'language')),
    kind text NOT NULL CHECK (kind IN ('preference', 'suppression', 'convention', 'fact')),
    path_glob text,
    language text,
    memory_text text NOT NULL CHECK (length(memory_text) BETWEEN 1 AND 8000),
    evidence jsonb NOT NULL DEFAULT '[]'::jsonb,
    weight real NOT NULL DEFAULT 1 CHECK (weight >= 0 AND weight <= 1),
    embedding_model text NOT NULL,
    embedding vector(1536) NOT NULL,
    last_confirmed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (installation_id, id),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE,
    CHECK ((scope = 'organization' AND repository_id IS NULL) OR (scope <> 'organization' AND repository_id IS NOT NULL)),
    CHECK ((scope = 'path') = (path_glob IS NOT NULL)),
    CHECK ((scope = 'language') = (language IS NOT NULL))
);

CREATE INDEX memories_scope_lookup ON memories (installation_id, repository_id, scope, kind, last_confirmed_at DESC);
CREATE INDEX memories_embedding_hnsw ON memories USING hnsw (embedding vector_cosine_ops);

ALTER TABLE memories ENABLE ROW LEVEL SECURITY;
ALTER TABLE memories FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON memories
    USING (installation_id = current_setting('px1.installation_id', true)::bigint)
    WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);

COMMIT;
