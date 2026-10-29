BEGIN;
DROP TABLE IF EXISTS credit_transactions;
DROP FUNCTION IF EXISTS reject_credit_transaction_mutation();
COMMIT;
