-- 004_add_token_family.up.sql
-- Adds family column to refresh_tokens for rotating refresh token theft detection.
-- Also adds an index on token_hash for fast lookups.

BEGIN;

ALTER TABLE refresh_tokens ADD COLUMN family text NOT NULL DEFAULT '';
CREATE INDEX idx_refresh_tokens_hash ON refresh_tokens (token_hash);

COMMIT;
