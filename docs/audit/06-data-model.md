# 06-data-model — Implementation Audit

> Audited against: `docs/spec/06-data-model.md`, `docs/spec/01-architecture.md §4`, `docs/spec/01-architecture.md §5`
> Dev record: `docs/dev/06-data-model.md`
> Date: 2026-06-02

## Summary

| Category | Count |
|----------|-------|
| Critical gaps | 0 |
| Significant gaps | 2 |
| Minor gaps | 3 |

## 1. Critical Gaps

*None identified.*

## 2. Significant Gaps

### 2.1 Missing `idx_cred_user_origin` regular index on `browser_credentials`

- **File:** `gateway/migrations/003_align_data_model.up.sql:24`
- **Severity:** Significant
- **What's wrong:** Migration 002 created `CREATE UNIQUE INDEX idx_browser_creds_user_origin ON browser_credentials (user_id, origin)` — a unique index, which was the wrong constraint (spec requires UNIQUE on `label`, not `origin`). Migration 003 correctly drops this index and creates `UNIQUE INDEX idx_browser_creds_user_label ON browser_credentials (user_id, label)`. However, it **never recreates** a regular (non-unique) index on `(user_id, origin)`. The spec explicitly requires: `Index idx_cred_user_origin (user_id, origin)` for querying credentials by user + origin.
- **Spec ref:** `docs/spec/06-data-model.md §1.16`: "`UNIQUE(user_id, label)`. Index `idx_cred_user_origin (user_id, origin)`. Credentials are customer-owned (not shared); only the owning customer may reference them in a job."
- **Impact:** Queries filtering `browser_credentials` by `(user_id, origin)` — e.g., checking if a credential already exists for a site before saving from a VNC session — will perform full scans on `user_id`. This degrades linearly with the number of credentials per user. At scale, this adds measurable latency to credential-save operations and duplicate-detection logic.

### 2.2 `CredentialStore.LinkToJob` does not validate user/credential ownership

- **File:** `gateway/internal/store/credentials.go:88-92`
- **Severity:** Significant
- **What's wrong:** The `LinkToJob` method inserts into `job_credentials` with a bare `INSERT … ON CONFLICT DO NOTHING` — it never verifies that `credential.user_id` matches `job.user_id`. The spec requires the link must only be created when the credential belongs to the same user as the job. If this check is missing in the handler too, a customer could link another customer's credential to their own job, causing cross-tenant credential injection.
- **Spec ref:** `docs/spec/06-data-model.md §1.17`: "Which saved logins are injected for a job. … `credential_id` uuid FK→browser_credentials — **must belong to the same `user_id` as the job**."
- **Impact:** Without enforcement at either the store or handler layer, a tenant-boundary violation is possible. A malicious API call could reference another user's `credential_id` and trigger injection of their encrypted cookie into the wrong job.

## 3. Minor Gaps

### 3.1 Device `files` status uses `"staged"` instead of `"staged_device"`

- **File:** `device/iagent_device/store/repositories.py:176`
- **Severity:** Minor
- **What's wrong:** The spec defines file status values as `staged_device / purged / error`. The `FileRepo.create` method writes the status as `"staged"` (missing the `_device` suffix).
- **Spec ref:** `docs/spec/06-data-model.md §2` (files table): "`status TEXT, -- staged_device / purged / error`"

### 3.2 Device uses standalone `schema_version` table instead of `meta` table

- **File:** `device/iagent_device/store/connection.py:28`
- **Severity:** Minor
- **What's wrong:** The spec says "store `schema_version` in a `meta` table." The implementation creates a separate `schema_version` table instead of embedding the version field in a more general `meta` table. Functionally identical, but diverges structurally from the stated design.
- **Spec ref:** `docs/spec/06-data-model.md §5`: "Device: lightweight versioned migrations applied on startup; store `schema_version` in a `meta` table."

### 3.3 Device `vnc_sessions` stores operational columns beyond the spec

- **File:** `device/iagent_device/store/connection.py:117-128`, `device/iagent_device/store/repositories.py:228-250`
- **Severity:** Minor
- **What's wrong:** The spec's device `vnc_sessions` table lists only `session_id, job_id, agent_id, rfb_port, status, created_at, ended_at`. The implementation adds `rfb_password` (plaintext RFB password), `relay_url`, and `session_token` (plaintext). The spec explicitly states "vnc_sessions tracks only bridge state (ports/status); the RFB byte stream is relayed live and never stored." While these extra columns are operationally needed by the VNC bridge to function, storing the `session_token` and `rfb_password` in plaintext in SQLite is a security surface that the spec's minimized design intentionally avoids.
- **Spec ref:** `docs/spec/06-data-model.md §2` (vnc_sessions table), and the note: "vnc_sessions tracks only bridge state (ports/status); the RFB byte stream is relayed live and never stored."

## 4. What's Solidly Implemented

| Area | Files | Notes |
|------|-------|-------|
| **Cloud PostgreSQL schema** | `gateway/migrations/001_initial_schema.up.sql`, `002_docker_vnc_creds.up.sql`, `003_align_data_model.up.sql` | All 18 spec tables present with correct columns, types, constraints, and FKs. Paired up/down migrations exist for all 3 files. |
| **Cloud Go models** | `gateway/internal/model/types.go` | Complete domain types for every table with correct `db:` tags. Includes all required enums (UserRole, JobStatus, AgentStatus, DeviceStatus, SkillInstallStatus, etc.), frame types, DTOs, and tunnel payloads. |
| **UUIDv7** | `gateway/internal/model/types.go:16-21` | `uuid.NewV7()` used for all IDs. |
| **citext emails** | `gateway/migrations/001_initial_schema.up.sql:8,27` | `CREATE EXTENSION IF NOT EXISTS citext` + `email citext NOT NULL UNIQUE`. |
| **Argon2id password_hash** | `gateway/internal/model/types.go:195` | `PasswordHash string` column present. |
| **Pooled agent model** | `gateway/migrations/001_initial_schema.up.sql:80-98`, `gateway/internal/store/agents.go:47-58` | `user_id` nullable (NULL=idle), `job_id` nullable, `FindIdle` with `FOR UPDATE SKIP LOCKED`. |
| **Tiered queue** | `gateway/migrations/001_initial_schema.up.sql:133-134`, `gateway/internal/model/types.go:53-62` | Partial index `WHERE status='queued'` on `(status, user_tier, created_at)`. `TierPriority()` maps free/pro/enterprise to numeric priorities. |
| **Denormalized routing** | `gateway/migrations/001_initial_schema.up.sql:108`, `gateway/internal/model/types.go:262` | `jobs.device_id` + `jobs.user_tier` denormalized at submission. |
| | `gateway/internal/store/jobs.go` | Full CRUD on all 22 job columns. Queue position computation. |
| **Job state machine** | `gateway/internal/model/types.go:68-87` | All 7 states: pending→queued→dispatched→running→succeeded/failed/cancelled. `IsTerminal()`, `IsActive()` helpers. |
| **Agent state machine** | `gateway/internal/model/types.go:96-105` | All 6 states: creating/idle/busy/unhealthy/failed/removed. |
| **Skill vault catalog** | `gateway/migrations/001_initial_schema.up.sql:65-75` | `skills` + `skill_versions` with semver, manifest, artifacts. |
| **Fleet skill dispatch** | `gateway/migrations/001_initial_schema.up.sql:183-192` | `device_skills` with per-device install status tracking. |
| **Per-agent skill enable** | `gateway/migrations/001_initial_schema.up.sql:197-204` | `agent_skills` with `selected_by` audit. |
| **Skill grants & organizations** | `gateway/migrations/001_initial_schema.up.sql:209-216`, `001_initial_schema.up.sql:13-20` | `skill_grants` with `principal_type` (user/org). `organizations` with `users.org_id` FK. |
| **browser_credentials (encrypted vault)** | `gateway/migrations/002_docker_vnc_creds.up.sql:34-49`, `003_align_data_model.up.sql:20-25` | Correctly migrated to `storage_state_enc` + separate `nonce`/`auth_tag` columns, UNIQUE on `(user_id, label)`. |
| **vnc_sessions** | `gateway/migrations/002_docker_vnc_creds.up.sql:9-30`, `003_align_data_model.up.sql:12-15` | All spec columns + operational columns. `token_hash` for relay token. `token_expires_at`, `started_at`, `ended_at`, `close_reason` added in 003. Status enum includes `error`. |
| **Refresh tokens** | `gateway/migrations/001_initial_schema.up.sql:221-230` | `token_hash`, `expires_at`, `revoked_at`, `user_agent`, `ip`. |
| **Audit log** | `gateway/migrations/001_initial_schema.up.sql:235-244` | `actor`, `action`, `target_type`, `target_id`, `meta` jsonb. |
| **Device SQLite schema** | `device/iagent_device/store/connection.py` | All 8 tables (device_info, agents, jobs, files, outbox, device_skills, agent_skills, vnc_sessions). WAL mode, single writer, versioned migration. |
| **Device repositories** | `device/iagent_device/store/repositories.py` | Full CRUD repos: DeviceRepo, AgentRepo, JobRepo, OutboxRepo, FileRepo, SkillRepo, VNCSessionRepo. Agent pool allocate/release cycle. Outbox with ack/purge. |
| **Agent ephemeral state** | `agent/iagent_agent/runtime/context.py`, `executor.py` | `JobRecord` with `started_at`, `finished_at`, cancel support. `JobState` enum with terminal set. |
| **Agent workspace** | `agent/iagent_agent/workspace.py` | `/work/{inputs,scratch,output,profile}` directories. `wipe()` clears all 4 dirs on job completion. `_DEFAULT_QUOTA_MB = 10 * 1024` (10 GB). |
| **Agent skill manager** | `agent/iagent_agent/skills/loader.py` | Full install/update/enable/disable/delete lifecycle. Registry persisted to disk but loaded into memory per-job. |
| **Agent browser + VNC** | `agent/iagent_agent/browser/manager.py` | `BrowserManager` with `inject_state`/`export_state` for login cookies. `VNCStack` with `running`, `rfb_port`, `rfb_password` properties matching spec's `vnc:{enabled, rfb_port}` concept. |
| **Gateway store layer** | `gateway/internal/store/*.go` | All 11 store files present with pgx-backed implementations. Consistent error handling (pgx.ErrNoRows → nil, nil). |

## 5. Fix Priority

| Priority | Gap | Impact |
|----------|-----|--------|
| P0 | 2.2 — `LinkToJob` missing user-ownership check | Cross-tenant credential injection possible if handler also lacks the check. |
| P1 | 2.1 — Missing `idx_cred_user_origin` index | Unindexed `(user_id, origin)` lookups on `browser_credentials`. Degrades at scale. |
| P2 | 3.1 — Device files `"staged"` vs `"staged_device"` | Naming inconsistency; any code comparing against `"staged_device"` would break. |
| P3 | 3.3 — Device vnc_sessions extra plaintext columns | Security surface: `session_token` and `rfb_password` in SQLite plaintext. |
| P3 | 3.2 — `schema_version` table vs `meta` table | Cosmetic divergence from spec; functionally identical. |
