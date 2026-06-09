-- 005_add_job_files_role.up.sql
-- Adds role column to job_files to distinguish input from output files.

BEGIN;

ALTER TABLE job_files
  ADD COLUMN IF NOT EXISTS role text NOT NULL DEFAULT 'input'
  CHECK (role IN ('input', 'output'));

CREATE INDEX IF NOT EXISTS idx_job_files_job_role ON job_files (job_id, role);

COMMIT;
