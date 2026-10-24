BEGIN;

ALTER TABLE repositories
    ADD COLUMN indexed_files bigint NOT NULL DEFAULT 0,
    ADD COLUMN indexed_bytes bigint NOT NULL DEFAULT 0,
    ADD COLUMN index_limit_reason text;

COMMIT;
