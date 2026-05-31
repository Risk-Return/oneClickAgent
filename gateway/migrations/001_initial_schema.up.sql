-- 001_initial_schema.up.sql
-- Creates the full oneClickAgent Cloud Gateway schema on PostgreSQL 15+.
-- Tables ordered by dependency; circular FKs added via ALTER TABLE at the end.

BEGIN;

-- Enable citext for case-insensitive emails
CREATE EXTENSION IF NOT EXISTS citext;

-- ============================================================
-- organizations (no FKs — created first)
-- ============================================================
CREATE TABLE organizations (
    id          uuid PRIMARY KEY,
    name        text        NOT NULL UNIQUE,
    description text,
    created_by  uuid,        -- FK added later
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

-- ============================================================
-- users
-- ============================================================
CREATE TABLE users (
    id              uuid PRIMARY KEY,
    email           citext      NOT NULL UNIQUE,
    username        text        NOT NULL UNIQUE,
    password_hash   text        NOT NULL,
    status          text        NOT NULL DEFAULT 'active',
    role            text        NOT NULL DEFAULT 'user',
    tier            text        NOT NULL DEFAULT 'free',
    org_id          uuid,        -- FK added later
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);

-- Add FKs between users and organizations
ALTER TABLE users ADD CONSTRAINT fk_users_org FOREIGN KEY (org_id) REFERENCES organizations(id);
ALTER TABLE organizations ADD CONSTRAINT fk_orgs_created_by FOREIGN KEY (created_by) REFERENCES users(id);

-- ============================================================
-- devices
-- ============================================================
CREATE TABLE devices (
    id               uuid PRIMARY KEY,
    operator_id      uuid        NOT NULL REFERENCES users(id),
    name             text        NOT NULL,
    description      text,
    platform         text,
    status           text        NOT NULL DEFAULT 'enrolled',
    token_hash       text        NOT NULL,
    token_rotated_at timestamptz,
    last_seen_at     timestamptz,
    resources        jsonb,
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL,
    UNIQUE (operator_id, name)
);
CREATE INDEX idx_devices_operator ON devices (operator_id);

-- ============================================================
-- skills (cloud skill vault catalog)
-- ============================================================
CREATE TABLE skills (
    id             uuid PRIMARY KEY,
    key            text        NOT NULL UNIQUE,
    name           text        NOT NULL,
    description    text,
    visibility     text        NOT NULL DEFAULT 'restricted',
    latest_version text,
    status         text        NOT NULL DEFAULT 'active',
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL
);

-- ============================================================
-- agents (pooled) — no FK to jobs yet (circular)
-- ============================================================
CREATE TABLE agents (
    id           uuid PRIMARY KEY,
    device_id    uuid        NOT NULL REFERENCES devices(id),
    user_id      uuid        REFERENCES users(id),
    name         text        NOT NULL,
    description  text,
    image        text        NOT NULL,
    port         int         NOT NULL,
    tags         text[],
    status       text        NOT NULL DEFAULT 'creating',
    job_id       uuid,        -- FK added after jobs table created
    limits       jsonb       NOT NULL DEFAULT '{"cpu":2,"mem_mb":4096,"disk_mb":10240}',
    allocated_at timestamptz,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL,
    UNIQUE (device_id, port)
);
CREATE INDEX idx_agents_status ON agents (status);
CREATE INDEX idx_agents_device ON agents (device_id);

-- ============================================================
-- jobs
-- ============================================================
CREATE TABLE jobs (
    id               uuid PRIMARY KEY,
    user_id          uuid        NOT NULL REFERENCES users(id),
    user_tier        text        NOT NULL,
    agent_id         uuid,        -- FK added after agents table
    device_id        uuid        REFERENCES devices(id),
    channel          text        NOT NULL DEFAULT 'web',
    command          text        NOT NULL,
    params           jsonb,
    skill_id         uuid        REFERENCES skills(id),
    status           text        NOT NULL DEFAULT 'pending',
    percent          int         DEFAULT 0,
    progress_message text,
    result           jsonb,
    error_code       text,
    error_message    text,
    queued_at        timestamptz,
    queue_expires_at timestamptz,
    submitted_at     timestamptz NOT NULL,
    started_at       timestamptz,
    finished_at      timestamptz,
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL
);

-- Resolve circular FK between agents and jobs
ALTER TABLE agents ADD CONSTRAINT fk_agents_job FOREIGN KEY (job_id) REFERENCES jobs(id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE jobs ADD CONSTRAINT fk_jobs_agent FOREIGN KEY (agent_id) REFERENCES agents(id) DEFERRABLE INITIALLY DEFERRED;

-- Job indexes
CREATE INDEX idx_jobs_queue ON jobs (status, user_tier, created_at)
    WHERE status = 'queued';
CREATE INDEX idx_jobs_user_created ON jobs (user_id, created_at DESC);
CREATE INDEX idx_jobs_agent ON jobs (agent_id);
CREATE INDEX idx_jobs_active ON jobs (status)
    WHERE status IN ('pending', 'queued', 'dispatched', 'running');

-- ============================================================
-- files
-- ============================================================
CREATE TABLE files (
    id          uuid PRIMARY KEY,
    user_id     uuid        NOT NULL REFERENCES users(id),
    name        text        NOT NULL,
    size        bigint      NOT NULL,
    mime        text,
    sha256      text        NOT NULL,
    storage_uri text,
    status      text        NOT NULL DEFAULT 'staged_cloud',
    created_at  timestamptz NOT NULL,
    purged_at   timestamptz
);

-- ============================================================
-- job_files (link table — now both jobs and files exist)
-- ============================================================
CREATE TABLE job_files (
    job_id  uuid NOT NULL REFERENCES jobs(id),
    file_id uuid NOT NULL REFERENCES files(id),
    PRIMARY KEY (job_id, file_id)
);

-- ============================================================
-- skill_versions (vault artifacts)
-- ============================================================
CREATE TABLE skill_versions (
    id           uuid PRIMARY KEY,
    skill_id     uuid        NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    version      text        NOT NULL,
    manifest     jsonb       NOT NULL,
    artifact_uri text,
    sha256       text        NOT NULL,
    size         bigint,
    created_at   timestamptz NOT NULL,
    UNIQUE (skill_id, version)
);

-- ============================================================
-- device_skills (per-device install record)
-- ============================================================
CREATE TABLE device_skills (
    device_id     uuid        NOT NULL REFERENCES devices(id),
    skill_id      uuid        NOT NULL REFERENCES skills(id),
    version       text        NOT NULL,
    status        text        NOT NULL DEFAULT 'installing',
    installed_by  uuid        REFERENCES users(id),
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
    status      text DEFAULT 'enabled',
    selected_by uuid REFERENCES users(id),
    updated_at  timestamptz NOT NULL,
    PRIMARY KEY (agent_id, skill_id)
);

-- ============================================================
-- skill_grants (admin-managed visibility)
-- ============================================================
CREATE TABLE skill_grants (
    skill_id       uuid        NOT NULL REFERENCES skills(id),
    principal_type text        NOT NULL,
    principal_id   uuid        NOT NULL,
    granted_by     uuid        REFERENCES users(id),
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
    user_agent text,
    ip         text,
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
