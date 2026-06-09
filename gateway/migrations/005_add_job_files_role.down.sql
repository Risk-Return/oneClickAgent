-- 005_add_job_files_role.down.sql

BEGIN;

DROP INDEX IF EXISTS idx_job_files_job_role;
ALTER TABLE job_files DROP COLUMN IF EXISTS role;

COMMIT;
