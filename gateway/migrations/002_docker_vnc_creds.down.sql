-- 002_docker_vnc_creds.down.sql
-- Reverts the Docker/VNC/credential-vault tables.

BEGIN;

DROP TABLE IF EXISTS job_credentials     CASCADE;
DROP TABLE IF EXISTS browser_credentials CASCADE;
DROP TABLE IF EXISTS vnc_sessions        CASCADE;

COMMIT;
