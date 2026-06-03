-- 004_add_token_family.down.sql

BEGIN;

DROP INDEX IF EXISTS idx_refresh_tokens_hash;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS family;

COMMIT;
