-- 002_docker_vnc_creds.up.sql
-- Adds Docker/VNC/credential-vault tables per 06-data-model §1.15–§1.17.

BEGIN;

-- ============================================================
-- vnc_sessions (§1.15)
-- ============================================================
CREATE TABLE vnc_sessions (
    id               uuid PRIMARY KEY,
    job_id           uuid        NOT NULL REFERENCES jobs(id),
    user_id          uuid        NOT NULL REFERENCES users(id),
    device_id        uuid        NOT NULL REFERENCES devices(id),
    agent_id         uuid        NOT NULL REFERENCES agents(id),
    session_token_hash text      NOT NULL,                       -- single-use relay token hash
    rfb_password     text,                                       -- VNC password for the browser
    status           text        NOT NULL DEFAULT 'pending',     -- pending | ready | active | closed
    gateway_node     text,                                       -- owning gateway instance (multi-instance)
    idle_ttl_secs    int         NOT NULL DEFAULT 300,           -- IAGENT_VNC_IDLE_TTL
    max_ttl_secs     int         NOT NULL DEFAULT 1800,          -- IAGENT_VNC_MAX_TTL
    last_active_at   timestamptz,
    created_at       timestamptz NOT NULL,
    closed_at        timestamptz
);

CREATE INDEX idx_vnc_sessions_user   ON vnc_sessions (user_id);
CREATE INDEX idx_vnc_sessions_job    ON vnc_sessions (job_id);
CREATE INDEX idx_vnc_sessions_status ON vnc_sessions (status)
    WHERE status IN ('pending', 'ready', 'active');

-- ============================================================
-- browser_credentials (§1.16)
-- ============================================================
CREATE TABLE browser_credentials (
    id           uuid PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users(id),
    label        text        NOT NULL,                           -- user-given name
    origin       text        NOT NULL,                           -- e.g. https://example.com
    ciphertext   bytea       NOT NULL,                           -- AES-256-GCM: nonce(12) || tag(16) || ct
    key_id       text        NOT NULL DEFAULT 'default',         -- supports KMS key rotation
    sha256       text        NOT NULL,                           -- plaintext integrity verification
    size_bytes   bigint      NOT NULL,                           -- original plaintext size
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);

CREATE INDEX idx_browser_creds_user ON browser_credentials (user_id);
CREATE UNIQUE INDEX idx_browser_creds_user_origin ON browser_credentials (user_id, origin);

-- ============================================================
-- job_credentials (§1.17, link table)
-- ============================================================
CREATE TABLE job_credentials (
    job_id        uuid NOT NULL REFERENCES jobs(id),
    credential_id uuid NOT NULL REFERENCES browser_credentials(id),
    PRIMARY KEY (job_id, credential_id)
);

COMMIT;
