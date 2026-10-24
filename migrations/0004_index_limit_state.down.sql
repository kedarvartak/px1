BEGIN;

ALTER TABLE repositories
    DROP COLUMN index_limit_reason,
    DROP COLUMN indexed_bytes,
    DROP COLUMN indexed_files;

COMMIT;
