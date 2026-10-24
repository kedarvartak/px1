BEGIN;

ALTER TABLE files
    DROP COLUMN summary_source_truncated,
    DROP COLUMN summary_prompt_version,
    DROP COLUMN summary_model;

COMMIT;
