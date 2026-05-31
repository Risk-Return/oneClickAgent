-- 001_initial_schema.up.sql
-- Creates the full oneClickAgent Cloud Gateway schema on PostgreSQL 15+.

BEGIN;

-- Enable citext for case-insensitive emails
CREATE EXTENSION IF NOT EXISTS citext;

-- ============================================================
-- users
-- ============================================================
CREATE TABLE users (
    id              uuid PRIMARY KEY,
    email           citext      NOT NULL UNIQUE,
    username        text        NOT NULL UNIQUE,
    password_hash   text        NOT NULL,                      -- Argon2id
    status          text        NOT NULL DEFAULT 'active',     -- active | disabled
    role            text        NOT NULL DEFAULT 'user',       -- user | admin
    tier            text        NOT NULL DEFAULT 'free',       -- free | pro | enterprise
    org_id          uuid        REFERENCES organizations(id),
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);

-- ============================================================
-- organizations
-- ============================================================
CREATE TABLE organizations (
    id          uuid PRIMARY KEY,
    name        text        NOT NULL UNIQUE,
    description text,
    created_by  uuid        REFERENCES users(id),
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

-- Add FK from users.org_id (table created above, can now be enforced)
-- Already in the users DDL.

-- ============================================================
-- devices
-- ============================================================
CREATE TABLE devices (
    id               uuid PRIMARY KEY,
    operator_id      uuid        NOT NULL REFERENCES users(id),  -- admin user
    name             text        NOT NULL,
    description      text,
    platform         text,                                        -- windows | macos | linux
    status           text        NOT NULL DEFAULT 'enrolled',     -- enrolled | online | offline
    token_hash       text        NOT NULL,
    token_rotated_at timestamptz,
    last_seen_at     timestamptz,                                 -- updated by heartbeat
    resources        jsonb,                                      -- {cpu, mem_mb, disk_mb}
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL,

    UNIQUE (operator_id, name)
);

CREATE INDEX idx_devices_operator ON devices (operator_id);

-- ============================================================
-- agents (pooled)
-- ============================================================
CREATE TABLE agents (
    id           uuid PRIMARY KEY,
    device_id    uuid        NOT NULL REFERENCES devices(id),
    user_id      uuid        REFERENCES users(id),               -- set only when allocated
    name         text        NOT NULL,
    description  text,
    image        text        NOT NULL,                            -- docker image ref
    port         int         NOT NULL,                            -- fixed host port on device
    tags         text[],                                         -- specialization tags
    status       text        NOT NULL DEFAULT 'creating',        -- creating|idle|busy|unhealthy|failed|removed
    job_id       uuid        REFERENCES jobs(id),                -- set when busy
    limits       jsonb       NOT NULL DEFAULT '{"cpu":2,"mem_mb":4096,"disk_mb":10240}',
    allocated_at timestamptz,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL,

    UNIQUE (device_id, port)
);

CREATE INDEX idx_agents_status ON agents (status);
CREATE INDEX idx_agents_device ON agents (device_id);

-- ============================================================
-- skills (cloud skill vault catalog)
-- ============================================================
CREATE TABLE skills (
    id             uuid PRIMARY KEY,
    key            text        NOT NULL UNIQUE,                   -- stable slug, e.g. pdf-extract
    name           text        NOT NULL,
    description    text,
    visibility     text        NOT NULL DEFAULT 'restricted',     -- public | restricted
    latest_version text,
    status         text        NOT NULL DEFAULT 'active',         -- active | deprecated
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL
);

-- ============================================================
-- skill_versions (vault artifacts)
-- ============================================================
CREATE TABLE skill_versions (
    id           uuid PRIMARY KEY,
    skill_id     uuid        NOT NULL REFERENCES skills(id),
    version      text        NOT NULL,                            -- semver
    manifest     jsonb       NOT NULL,
    artifact_uri text,
    sha256       text        NOT NULL,
    size         bigint,
    created_at   timestamptz NOT NULL,

    UNIQUE (skill_id, version)
);

-- ============================================================
-- jobs
-- ============================================================
CREATE TABLE jobs (
    id               uuid PRIMARY KEY,
    user_id          uuid        NOT NULL REFERENCES users(id),
    user_tier        text        NOT NULL,                        -- denormalized from users.tier at submission
    agent_id         uuid        REFERENCES agents(id),           -- set at allocation
    device_id        uuid        REFERENCES devices(id),          -- denormalized, set at allocation
    channel          text        NOT NULL DEFAULT 'web',
    command          text        NOT NULL,
    params           jsonb,
    skill_id         uuid        REFERENCES skills(id),           -- at most ONE skill
    status           text        NOT NULL DEFAULT 'pending',      -- pending|queued|dispatched|running|succeeded|failed|cancelled
    percent          int         DEFAULT 0,                       -- 0–100 progress
    progress_message text,
    result           jsonb,                                       -- terminal payload (progress-level only)
    error_code       text,                                        -- QUEUE_TIMEOUT | SKILL_NOT_ENABLED | ...
    error_message    text,
    queued_at        timestamptz,
    queue_expires_at timestamptz,                                 -- = queued_at + TTL
    submitted_at     timestamptz NOT NULL,
    started_at       timestamptz,
    finished_at      timestamptz,
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL
);

-- Queue dequeue ordering
CREATE INDEX idx_jobs_queue ON jobs (status, user_tier, created_at)
    WHERE status = 'queued';

-- User's job listing
CREATE INDEX idx_jobs_user_created ON jobs (user_id, created_at DESC);

-- Agent's jobs
CREATE INDEX idx_jobs_agent ON jobs (agent_id);

-- Active jobs lookup
CREATE INDEX idx_jobs_active ON jobs (status)
    WHERE status IN ('pending', 'queued', 'dispatched', 'running');

-- ============================================================
-- job_files (link table)
-- ============================================================
CREATE TABLE job_files (
    job_id  uuid NOT NULL REFERENCES jobs(id),
    file_id uuid NOT NULL REFERENCES files(id),
    PRIMARY KEY (job_id, file_id)
);

-- ============================================================
-- files
-- ============================================================
CREATE TABLE files (
    id          uuid PRIMARY KEY,
    user_id     uuid        NOT NULL REFERENCES users(id),
    name        text        NOT NULL,                             -- original filename
    size        bigint      NOT NULL,
    mime        text,
    sha256      text        NOT NULL,                             -- integrity
    storage_uri text,                                             -- cloud staging location
    status      text        NOT NULL DEFAULT 'staged_cloud',      -- staged_cloud | staged_device | purged | error
    created_at  timestamptz NOT NULL,
    purged_at   timestamptz
);

-- ============================================================
-- device_skills (per-device install record)
-- ============================================================
CREATE TABLE device_skills (
    device_id     uuid        NOT NULL REFERENCES devices(id),
    skill_id      uuid        NOT NULL REFERENCES skills(id),
    version       text        NOT NULL,
    status        text        NOT NULL DEFAULT 'installing',      -- installing|installed|disabled|updating|error|deleting
    installed_by  uuid        REFERENCES users(id),               -- admin
    error_message text,
    updated_at    timestamptz NOT NULL,
    PRIMARY KEY (device_id, skill_id)
);

-- ============================================================
-- agent_skills (per-agent customer selection)
-- ============================================================
CREATE TABLE agent_skills (
    agent_id    uuid NOT NULL REFERENCES agents(id),
    skill_id    uuid NOT NULL REFERENCES skills(id),
    status      text DEFAULT 'enabled',                           -- enabled | disabled
    selected_by uuid REFERENCES users(id),
    updated_at  timestamptz NOT NULL,
    PRIMARY KEY (agent_id, skill_id)
);

-- ============================================================
-- skill_grants (admin-managed visibility)
-- ============================================================
CREATE TABLE skill_grants (
    skill_id       uuid        NOT NULL REFERENCES skills(id),
    principal_type text        NOT NULL,                          -- user | org
    principal_id   uuid        NOT NULL,                          -- users.id or organizations.id
    granted_by     uuid        REFERENCES users(id),              -- admin
    created_at     timestamptz NOT NULL,
    PRIMARY KEY (skill_id, principal_type, principal_id)
);

-- ============================================================
-- refresh_tokens
-- ============================================================
CREATE TABLE refresh_tokens (
    id         uuid PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users(id),
    token_hash text        NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    user_agent text,                                              -- audit
    ip         text,                                              -- audit
    created_at timestamptz NOT NULL
);

-- ============================================================
-- audit_log
-- ============================================================
CREATE TABLE audit_log (
    id          uuid PRIMARY KEY,
    user_id     uuid,
    actor       text,
    action      text        NOT NULL,
    target_type text,
    target_id   uuid,
    meta        jsonb,
    created_at  timestamptz NOT NULL
);

COMMIT;
