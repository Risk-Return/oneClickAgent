-- 003_align_data_model.up.sql
-- Aligns vnc_sessions and browser_credentials tables with 06-data-model spec.
-- vnc_sessions: add token_expires_at, started_at, close_reason; rename closed_at→ended_at
-- browser_credentials: rename ciphertext→storage_state_enc; separate nonce/auth_tag;
--                      fix UNIQUE to (user_id,label); remove size_bytes

BEGIN;

-- ============================================================
-- vnc_sessions (§1.15)
-- ============================================================
ALTER TABLE vnc_sessions ADD COLUMN token_expires_at timestamptz;
ALTER TABLE vnc_sessions ADD COLUMN started_at       timestamptz;
ALTER TABLE vnc_sessions ADD COLUMN close_reason      text;
ALTER TABLE vnc_sessions RENAME COLUMN closed_at TO ended_at;

-- ============================================================
-- browser_credentials (§1.16)
-- ============================================================
ALTER TABLE browser_credentials RENAME COLUMN ciphertext TO storage_state_enc;
ALTER TABLE browser_credentials ADD COLUMN nonce    bytea NOT NULL DEFAULT '\x'::bytea;
ALTER TABLE browser_credentials ADD COLUMN auth_tag bytea NOT NULL DEFAULT '\x'::bytea;
ALTER TABLE browser_credentials DROP COLUMN IF EXISTS size_bytes;
DROP INDEX IF EXISTS idx_browser_creds_user_origin;
CREATE UNIQUE INDEX idx_browser_creds_user_label ON browser_credentials (user_id, label);
CREATE INDEX idx_cred_user_origin ON browser_credentials (user_id, origin);

COMMIT;
