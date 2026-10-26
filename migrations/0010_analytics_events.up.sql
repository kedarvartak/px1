BEGIN;

CREATE TABLE analytics_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id text NOT NULL CHECK (length(source_id) BETWEEN 1 AND 200),
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository_id bigint NOT NULL,
    team text NOT NULL DEFAULT '' CHECK (length(team) <= 200),
    author_login text NOT NULL DEFAULT '' CHECK (length(author_login) <= 200),
    event_kind text NOT NULL CHECK (event_kind IN ('review', 'finding', 'feedback')),
    severity text CHECK (severity IN ('P0', 'P1', 'P2')),
    addressed boolean NOT NULL DEFAULT false,
    downvoted boolean NOT NULL DEFAULT false,
    review_latency_ms bigint NOT NULL DEFAULT 0 CHECK (review_latency_ms >= 0),
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (event_kind = 'review' AND severity IS NULL AND NOT addressed AND NOT downvoted) OR
        (event_kind = 'finding' AND severity IS NOT NULL AND review_latency_ms = 0) OR
        (event_kind = 'feedback' AND severity IS NULL AND NOT addressed AND review_latency_ms = 0)
    ),
    UNIQUE (installation_id, source_id),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE
);

CREATE TABLE daily_metrics (
    installation_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    metric_day date NOT NULL,
    team text NOT NULL DEFAULT '',
    author_login text NOT NULL DEFAULT '',
    reviews integer NOT NULL CHECK (reviews >= 0),
    findings integer NOT NULL CHECK (findings >= 0),
    addressed_findings integer NOT NULL CHECK (addressed_findings BETWEEN 0 AND findings),
    downvoted_findings integer NOT NULL CHECK (downvoted_findings BETWEEN 0 AND findings),
    median_review_latency_ms bigint NOT NULL CHECK (median_review_latency_ms >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (installation_id, repository_id, metric_day, team, author_login),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE
);

CREATE INDEX analytics_events_rollup ON analytics_events (installation_id, repository_id, occurred_at);

ALTER TABLE analytics_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE analytics_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON analytics_events USING (installation_id = current_setting('px1.installation_id', true)::bigint) WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);
ALTER TABLE daily_metrics ENABLE ROW LEVEL SECURITY;
ALTER TABLE daily_metrics FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON daily_metrics USING (installation_id = current_setting('px1.installation_id', true)::bigint) WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);

COMMIT;
