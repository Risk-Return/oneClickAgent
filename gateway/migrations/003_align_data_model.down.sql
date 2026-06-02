-- 003_align_data_model.down.sql
-- Reverts the data-model alignment changes.

BEGIN;

-- ============================================================
-- browser_credentials revert
-- ============================================================
DROP INDEX IF EXISTS idx_browser_creds_user_label;
DROP INDEX IF EXISTS idx_cred_user_origin;
CREATE UNIQUE INDEX idx_browser_creds_user_origin ON browser_credentials (user_id, origin);
ALTER TABLE browser_credentials ADD COLUMN size_bytes bigint;
ALTER TABLE browser_credentials DROP COLUMN auth_tag;
ALTER TABLE browser_credentials DROP COLUMN nonce;
ALTER TABLE browser_credentials RENAME COLUMN storage_state_enc TO ciphertext;

-- ============================================================
-- vnc_sessions revert
-- ============================================================
ALTER TABLE vnc_sessions RENAME COLUMN ended_at TO closed_at;
ALTER TABLE vnc_sessions DROP COLUMN close_reason;
ALTER TABLE vnc_sessions DROP COLUMN started_at;
ALTER TABLE vnc_sessions DROP COLUMN token_expires_at;

COMMIT;
