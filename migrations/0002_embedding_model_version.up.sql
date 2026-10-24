BEGIN;

ALTER TABLE chunks
    ADD COLUMN embedding_model_version text NOT NULL DEFAULT 'legacy';

ALTER TABLE chunks
    ALTER COLUMN embedding_model_version DROP DEFAULT;

COMMIT;
