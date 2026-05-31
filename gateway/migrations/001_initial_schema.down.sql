-- 001_initial_schema.down.sql
-- Reverts the initial schema migration.

BEGIN;

DROP TABLE IF EXISTS audit_log       CASCADE;
DROP TABLE IF EXISTS refresh_tokens   CASCADE;
DROP TABLE IF EXISTS skill_grants     CASCADE;
DROP TABLE IF EXISTS agent_skills     CASCADE;
DROP TABLE IF EXISTS device_skills    CASCADE;
DROP TABLE IF EXISTS job_files        CASCADE;
DROP TABLE IF EXISTS files            CASCADE;
DROP TABLE IF EXISTS jobs             CASCADE;
DROP TABLE IF EXISTS skill_versions   CASCADE;
DROP TABLE IF EXISTS skills           CASCADE;
DROP TABLE IF EXISTS agents           CASCADE;
DROP TABLE IF EXISTS devices          CASCADE;
DROP TABLE IF EXISTS users            CASCADE;
DROP TABLE IF EXISTS organizations    CASCADE;

DROP EXTENSION IF EXISTS citext;

COMMIT;
