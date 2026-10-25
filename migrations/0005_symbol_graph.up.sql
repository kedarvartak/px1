BEGIN;

CREATE TABLE symbols (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint NOT NULL,
    path text NOT NULL,
    blob_sha char(40) NOT NULL,
    kind text NOT NULL CHECK (kind IN ('module', 'namespace', 'type', 'class', 'interface', 'function', 'method', 'variable', 'constant')),
    name text NOT NULL,
    signature text,
    start_line integer NOT NULL CHECK (start_line > 0),
    end_line integer NOT NULL CHECK (end_line >= start_line),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (repository_id, path, blob_sha, kind, name, start_line, end_line),
    UNIQUE (installation_id, repository_id, id),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE
);

CREATE TABLE symbol_edges (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint NOT NULL,
    source_symbol_id bigint NOT NULL,
    target_symbol_id bigint,
    unresolved_target text,
    kind text NOT NULL CHECK (kind IN ('contains', 'calls', 'imports', 'reads', 'writes', 'implements', 'extends', 'tests')),
    confidence real NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    source_path text NOT NULL,
    source_line integer NOT NULL CHECK (source_line > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (target_symbol_id IS NOT NULL OR unresolved_target IS NOT NULL),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE,
    FOREIGN KEY (installation_id, repository_id, source_symbol_id) REFERENCES symbols(installation_id, repository_id, id) ON DELETE CASCADE,
    FOREIGN KEY (installation_id, repository_id, target_symbol_id) REFERENCES symbols(installation_id, repository_id, id) ON DELETE CASCADE
);

CREATE INDEX symbols_repository_name ON symbols (repository_id, name, kind);
CREATE INDEX symbols_repository_path ON symbols (repository_id, path, start_line);
CREATE INDEX symbol_edges_forward ON symbol_edges (repository_id, source_symbol_id, kind);
CREATE INDEX symbol_edges_reverse ON symbol_edges (repository_id, target_symbol_id, kind) WHERE target_symbol_id IS NOT NULL;

ALTER TABLE symbols ENABLE ROW LEVEL SECURITY;
ALTER TABLE symbols FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON symbols
    USING (installation_id = current_setting('px1.installation_id', true)::bigint)
    WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);

ALTER TABLE symbol_edges ENABLE ROW LEVEL SECURITY;
ALTER TABLE symbol_edges FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON symbol_edges
    USING (installation_id = current_setting('px1.installation_id', true)::bigint)
    WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);

COMMIT;
