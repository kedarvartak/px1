BEGIN;

CREATE TABLE credit_transactions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id bigint NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    review_id text NOT NULL DEFAULT '' CHECK (length(review_id) <= 200),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    kind text NOT NULL CHECK (kind IN ('grant', 'reserve', 'settle', 'refund')),
    credits bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (installation_id, idempotency_key),
    CHECK ((kind = 'reserve' AND credits < 0) OR (kind IN ('grant', 'refund') AND credits > 0) OR kind = 'settle')
);

CREATE INDEX credit_transactions_balance ON credit_transactions (installation_id, created_at);
CREATE UNIQUE INDEX credit_transactions_one_reserve ON credit_transactions (installation_id, review_id) WHERE kind = 'reserve';
CREATE UNIQUE INDEX credit_transactions_one_terminal ON credit_transactions (installation_id, review_id) WHERE kind IN ('settle', 'refund');
ALTER TABLE credit_transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE credit_transactions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON credit_transactions USING (installation_id = current_setting('px1.installation_id', true)::bigint) WITH CHECK (installation_id = current_setting('px1.installation_id', true)::bigint);

CREATE FUNCTION reject_credit_transaction_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'credit_transactions is append-only';
END;
$$;
CREATE TRIGGER credit_transactions_append_only BEFORE UPDATE OR DELETE ON credit_transactions FOR EACH ROW EXECUTE FUNCTION reject_credit_transaction_mutation();

COMMIT;
