BEGIN;

ALTER TABLE chunks DROP COLUMN embedding_model_version;

COMMIT;
