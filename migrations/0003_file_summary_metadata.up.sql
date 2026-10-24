BEGIN;

ALTER TABLE files
    ADD COLUMN summary_model text,
    ADD COLUMN summary_prompt_version text,
    ADD COLUMN summary_source_truncated boolean NOT NULL DEFAULT false;

COMMIT;
