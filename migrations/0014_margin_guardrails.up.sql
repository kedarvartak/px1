BEGIN;
CREATE TABLE weekly_billing_metrics (
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    week_start date NOT NULL,
    revenue_micros bigint NOT NULL CHECK (revenue_micros >= 0),
    llm_cost_micros bigint NOT NULL CHECK (llm_cost_micros >= 0),
    margin double precision,
    low_margin boolean NOT NULL DEFAULT false,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    alerted_at timestamptz,
    PRIMARY KEY (installation_id, week_start),
    CHECK ((revenue_micros = 0 AND margin IS NULL) OR (revenue_micros > 0 AND margin IS NOT NULL))
);
ALTER TABLE weekly_billing_metrics ENABLE ROW LEVEL SECURITY;
ALTER TABLE weekly_billing_metrics FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON weekly_billing_metrics USING (installation_id = current_setting('px1.installation_id', true)::bigint) WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);
COMMIT;
