BEGIN;
CREATE TABLE stripe_accounts (
    installation_id bigint PRIMARY KEY REFERENCES installations(id) ON DELETE CASCADE,
    customer_id text NOT NULL UNIQUE CHECK (customer_id ~ '^cus_[A-Za-z0-9]+$'),
    subscription_id text NOT NULL DEFAULT '',
    subscription_status text NOT NULL DEFAULT 'inactive',
    price_id text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE stripe_events (
    event_id text PRIMARY KEY CHECK (length(event_id) BETWEEN 1 AND 200),
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    event_type text NOT NULL CHECK (event_type IN ('invoice.paid', 'customer.subscription.updated')),
    processed_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE stripe_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE stripe_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON stripe_accounts USING (installation_id = current_setting('px1.installation_id', true)::bigint) WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);
ALTER TABLE stripe_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE stripe_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON stripe_events USING (installation_id = current_setting('px1.installation_id', true)::bigint) WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);
COMMIT;
