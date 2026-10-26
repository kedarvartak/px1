BEGIN;
CREATE TABLE oss_applications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    repository_url text NOT NULL CHECK (repository_url LIKE 'https://%'),
    contact_email text NOT NULL CHECK (length(contact_email) BETWEEN 3 AND 320),
    reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 4000),
    status text NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted', 'approved', 'rejected')),
    submitted_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz,
    decided_by text NOT NULL DEFAULT '',
    decision_reason text NOT NULL DEFAULT '',
    credit_grant bigint NOT NULL DEFAULT 0 CHECK (credit_grant >= 0),
    FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id) ON DELETE CASCADE,
    CHECK ((status = 'submitted' AND decided_at IS NULL AND decided_by = '') OR (status <> 'submitted' AND decided_at IS NOT NULL AND decided_by <> ''))
);
ALTER TABLE oss_applications ENABLE ROW LEVEL SECURITY;
ALTER TABLE oss_applications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON oss_applications USING (installation_id = current_setting('px1.installation_id', true)::bigint) WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);
COMMIT;
