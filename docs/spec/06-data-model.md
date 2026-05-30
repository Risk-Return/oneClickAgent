# 06 — Data Model

Three stores, each authoritative for its scope (see `01-architecture.md §4`):

- **Cloud (PostgreSQL 15+)** — users, devices (admin-managed), agents (customer-owned), jobs (canonical), files (metadata), skills + visibility, audit.
- **Local Device (SQLite, WAL)** — execution detail + in-flight cache.
- **Agent Container (ephemeral)** — in-memory + workspace files, no durable user data.

> **Ownership model:** `admin` (operator) manages **devices** and the **skill vault**; a `user` (customer) owns **agents/jobs/files** but never owns or sees devices. The platform places a customer's agent onto an admin-managed device.

IDs are **UUIDv7** (time-sortable) stored as `uuid`/`TEXT`. Timestamps are UTC (`timestamptz` / ISO-8601 text).

---

## 1. Cloud — PostgreSQL

### 1.1 `users`

| Column | Type | Notes |
|--------|------|-------|
| `id` | uuid PK | |
| `email` | citext UNIQUE NOT NULL | login identity |
| `username` | text UNIQUE NOT NULL | display handle |
| `password_hash` | text NOT NULL | Argon2id |
| `status` | text NOT NULL DEFAULT 'active' | `active`/`disabled` |
| `role` | text NOT NULL DEFAULT 'user' | `user`/`admin` |
| `org_id` | uuid FK→organizations NULL | optional **group/organization** the customer belongs to (null = single user) |
| `created_at` / `updated_at` | timestamptz | |

### 1.2 `devices`

| Column | Type | Notes |
|--------|------|-------|
| `id` | uuid PK | |
| `operator_id` | uuid FK→users | **admin** who manages this device (never a customer) |
| `name` | text NOT NULL | |
| `description` | text | |
| `platform` | text | `windows`/`macos`/`linux` |
| `status` | text NOT NULL DEFAULT 'enrolled' | `enrolled`/`online`/`offline` |
| `token_hash` | text NOT NULL | hash of device_token |
| `token_rotated_at` | timestamptz | |
| `last_seen_at` | timestamptz | updated by heartbeat |
| `resources` | jsonb | cpu/mem/disk capacity |
| `created_at` / `updated_at` | timestamptz | |

`UNIQUE(operator_id, name)`. Devices are admin-managed infrastructure; customers never own or directly see devices. Index `idx_devices_operator` on `operator_id`.

### 1.3 `agents`

| Column | Type | Notes |
|--------|------|-------|
| `id` | uuid PK | |
| `device_id` | uuid FK→devices NULL | host device, **assigned by the platform scheduler** (not chosen by the customer) |
| `user_id` | uuid FK→users | owning **customer** |
| `name` | text NOT NULL | |
| `description` | text | |
| `image` | text NOT NULL | docker image ref |
| `port` | int NOT NULL | fixed host port on device |
| `tags` | text[] | specialization tags |
| `status` | text NOT NULL DEFAULT 'creating' | see agent state machine |
| `limits` | jsonb NOT NULL | `{cpu, mem_mb, disk_mb}` default `{2,4096,10240}` |
| `created_at` / `updated_at` | timestamptz | |

`UNIQUE(device_id, port)`, `UNIQUE(user_id, name)`. The customer owns the agent; the platform places it on an admin-managed device. Default 1 agent per user (configurable cap). Index on `user_id`, `device_id`.

### 1.4 `jobs`

| Column | Type | Notes |
|--------|------|-------|
| `id` | uuid PK | |
| `user_id` | uuid FK→users | |
| `agent_id` | uuid FK→agents | |
| `device_id` | uuid FK→devices | denormalized for routing |
| `channel` | text NOT NULL DEFAULT 'web' | source channel |
| `command` | text NOT NULL | user instruction |
| `params` | jsonb | structured args |
| `skill_id` | uuid FK→skills NULL | **at most one** skill selected to run this job (enforced ≤ 1; must be enabled on the agent); null = no skill |
| `status` | text NOT NULL DEFAULT 'pending' | job state machine |
| `percent` | int DEFAULT 0 | 0–100 |
| `progress_message` | text | latest human-readable status |
| `result` | jsonb | terminal payload (progress-level only) |
| `error_code` / `error_message` | text | on failure |
| `submitted_at` / `started_at` / `finished_at` | timestamptz | |
| `created_at` / `updated_at` | timestamptz | |

Indexes: `idx_jobs_user_created (user_id, created_at desc)`, `idx_jobs_agent`, partial index on `status` for active jobs.

### 1.5 `job_files` (link)

| Column | Type | Notes |
|--------|------|-------|
| `job_id` | uuid FK→jobs | |
| `file_id` | uuid FK→files | |
| PK | `(job_id, file_id)` | |

### 1.6 `files`

| Column | Type | Notes |
|--------|------|-------|
| `id` | uuid PK | |
| `user_id` | uuid FK→users | |
| `name` | text NOT NULL | original filename |
| `size` | bigint NOT NULL | |
| `mime` | text | |
| `sha256` | text NOT NULL | integrity |
| `storage_uri` | text | cloud staging location |
| `status` | text NOT NULL DEFAULT 'staged_cloud' | `staged_cloud`/`staged_device`/`purged`/`error` |
| `created_at` / `purged_at` | timestamptz | |

### 1.7 `skills` (Cloud Skill Vault — catalog)

Central, **admin-owned** skill catalog. The vault is the source of truth for skill packages dispatched to devices. Customers never write here — they only see entries the admin makes visible to them (§1.12).

| Column | Type | Notes |
|--------|------|-------|
| `id` | uuid PK | |
| `key` | text UNIQUE NOT NULL | stable slug, e.g. `pdf-extract` |
| `name` | text NOT NULL | display name |
| `description` | text | |
| `visibility` | text NOT NULL DEFAULT 'restricted' | `public` (visible to all customers) / `restricted` (only granted users, see `skill_grants`) — **set by admin** |
| `latest_version` | text | currently published version |
| `status` | text NOT NULL DEFAULT 'active' | `active`/`deprecated` |
| `created_at` / `updated_at` | timestamptz | |

### 1.8 `skill_versions` (vault artifacts)

| Column | Type | Notes |
|--------|------|-------|
| `id` | uuid PK | |
| `skill_id` | uuid FK→skills | |
| `version` | text NOT NULL | semver |
| `manifest` | jsonb NOT NULL | declarative skill definition |
| `artifact_uri` | text | vault storage location of the packaged skill |
| `sha256` | text NOT NULL | integrity, verified on dispatch |
| `size` | bigint | |
| `created_at` | timestamptz | |

`UNIQUE(skill_id, version)`. Admins publish versions; devices receive a specific version via dispatch.

### 1.9 `device_skills` (link — per-device record of an admin install)

A skill installed on a device for **all of its agents**. Admin commands target the **whole fleet** (all devices); the gateway materializes one `device_skills` row per device to track and reconcile install state.

| Column | Type | Notes |
|--------|------|-------|
| `device_id` | uuid FK→devices | |
| `skill_id` | uuid FK→skills | |
| `version` | text NOT NULL | dispatched version |
| `status` | text NOT NULL DEFAULT 'installing' | `installing`/`installed`/`disabled`/`updating`/`error`/`deleting` |
| `installed_by` | uuid FK→users | admin who issued the command |
| `error_message` | text | on failure |
| `updated_at` | timestamptz | |
| PK | `(device_id, skill_id)` | |

### 1.10 `agent_skills` (link — per-agent customer selection)

`(agent_id uuid, skill_id uuid, status text DEFAULT 'enabled', selected_by uuid FK→users, updated_at timestamptz, PK(agent_id, skill_id))`. `status` ∈ `enabled`/`disabled`. An agent may have **many** enabled skills, but a single job selects **at most one** of them (`jobs.skill_id`). A customer may only enable a skill that is **(a)** visible to them (`public`, or a `skill_grants` row for the customer **or their organization**) **and (b)** `installed` (not `disabled`/`deleting`) on the device hosting the agent.

### 1.11 `organizations` (groups)

A group of customers (an organization/team). A customer optionally belongs to one organization (`users.org_id`); null = a standalone single user. Admins create organizations, assign members, and can grant skill visibility to a whole organization at once.

| Column | Type | Notes |
|--------|------|-------|
| `id` | uuid PK | |
| `name` | text UNIQUE NOT NULL | organization/group name |
| `description` | text | |
| `created_by` | uuid FK→users | admin who created it |
| `created_at` / `updated_at` | timestamptz | |

### 1.12 `skill_grants` (link — admin-managed skill visibility)

Which **principals** (a customer **or** an organization) may **see** a `restricted` skill. `public` skills need no grant. A skill is visible to a customer if it is `public`, **OR** granted directly to the customer, **OR** granted to the customer's organization. Managed exclusively by admins.

| Column | Type | Notes |
|--------|------|-------|
| `skill_id` | uuid FK→skills | |
| `principal_type` | text NOT NULL | `user` / `org` |
| `principal_id` | uuid NOT NULL | a `users.id` or `organizations.id` |
| `granted_by` | uuid FK→users | admin who granted |
| `created_at` | timestamptz | |
| PK | `(skill_id, principal_type, principal_id)` | |

### 1.13 `refresh_tokens`

| Column | Type | Notes |
|--------|------|-------|
| `id` | uuid PK | |
| `user_id` | uuid FK→users | |
| `token_hash` | text NOT NULL | |
| `expires_at` | timestamptz | |
| `revoked_at` | timestamptz | |
| `user_agent` / `ip` | text | audit |

### 1.14 `audit_log`

`(id uuid PK, user_id uuid, actor text, action text, target_type text, target_id uuid, meta jsonb, created_at timestamptz)`. Admin actions (fleet skill install/disable/update/delete, vault publish, visibility grants, org/member management, device management) are recorded here.

### 1.15 ER overview

```
admin (users.role=admin) ──owns──*  devices ──placement──*  agents
customer (users.role=user) ──owns──*  agents ──*  jobs ──*  files
organizations 1──* users            (users.org_id; null = single user)
jobs ──0..1 skills                   (jobs.skill_id; AT MOST ONE skill per job)

skills 1──* skill_versions
devices *──* skills        via device_skills  (admin fleet-wide install)
agents  *──* skills        via agent_skills   (customer selection, many enabled)
{users|orgs} *──* skills   via skill_grants   (admin visibility for 'restricted')
users   1──* refresh_tokens
audit_log
```

---

## 2. Local Device — SQLite

Mirrors cloud rows for in-flight work plus execution detail. Schema (DDL-style):

```sql
CREATE TABLE device_info (        -- single row
  device_id   TEXT PRIMARY KEY,
  name        TEXT,
  token       TEXT,               -- stored via OS keystore where possible
  gateway_url TEXT,
  enrolled_at TEXT
);

CREATE TABLE agents (
  agent_id     TEXT PRIMARY KEY,
  name         TEXT,
  image        TEXT,
  container_id TEXT,              -- docker container id
  port         INTEGER,
  tags         TEXT,              -- json array
  status       TEXT,
  limits_json  TEXT,
  restarts     INTEGER DEFAULT 0,
  created_at   TEXT,
  updated_at   TEXT
);

CREATE TABLE jobs (
  job_id        TEXT PRIMARY KEY,
  agent_id      TEXT,
  command       TEXT,
  params_json   TEXT,
  skill_id      TEXT,             -- at most one skill per job (nullable)
  status        TEXT,
  percent       INTEGER DEFAULT 0,
  workspace_dir TEXT,            -- per-job staging dir
  result_json   TEXT,
  error_json    TEXT,
  acked_by_cloud INTEGER DEFAULT 0,  -- for buffered flush
  created_at    TEXT,
  updated_at    TEXT
);

CREATE TABLE files (
  file_id     TEXT PRIMARY KEY,
  job_id      TEXT,
  name        TEXT,
  size        INTEGER,
  sha256      TEXT,
  local_path  TEXT,
  status      TEXT,              -- staged_device / purged / error
  created_at  TEXT,
  purged_at   TEXT
);

CREATE TABLE outbox (            -- at-least-once tunnel frames
  msg_id     TEXT PRIMARY KEY,
  type       TEXT,
  payload    TEXT,
  created_at TEXT,
  acked      INTEGER DEFAULT 0
);

CREATE TABLE device_skills (     -- skills installed device-wide (all agents)
  skill_id    TEXT PRIMARY KEY,
  key         TEXT,
  name        TEXT,
  version     TEXT,
  manifest    TEXT,
  artifact_path TEXT,            -- local cached skill package
  sha256      TEXT,
  status      TEXT,             -- installing/installed/disabled/updating/error/deleting
  updated_at  TEXT
);

CREATE TABLE agent_skills (      -- per-agent enable/disable of an installed skill
  agent_id   TEXT,
  skill_id   TEXT,
  status     TEXT,              -- enabled/disabled
  updated_at TEXT,
  PRIMARY KEY (agent_id, skill_id)
);
```

Notes:
- `device_skills` is the device's cache of vault skills; the device applies them to all its agents.
- `outbox` guarantees terminal results survive restarts until cloud ACK.
- `workspace_dir` is removed on job terminal state; row kept for audit until cloud confirms.
- Use `PRAGMA journal_mode=WAL;` and a single writer connection.

---

## 3. Agent Container — Ephemeral

No durable user storage. In-process state only:

```
/work/inputs/    # read-only staged input files (mounted by device)
/work/scratch/   # agent temp space
/work/output/    # result artifacts (returned, then wiped)
```

State object (in memory): `{ current_job: {job_id, status, percent, message, started_at} | null, skills:[...] }`. On job completion the executor wipes `/work/inputs`, `/work/scratch`, `/work/output`.

---

## 4. Retention & Cleanup Policy

| Data | Where | Lifetime |
|------|-------|----------|
| User input files | Agent workspace | Deleted on job terminal state |
| User input files | Device staging | Deleted on `FILE_PURGED` after job done |
| User input files | Cloud staging | Deleted after device confirms staged + job done (configurable grace, default 24h) |
| Job result (progress-level) | Cloud `jobs.result` | Retained per user policy (default 90 days) |
| Audit log | Cloud | Retained 1 year |

## 5. Migrations

- Cloud: SQL migration files in `gateway/migrations/` (e.g., `golang-migrate`), forward-only with paired up/down.
- Device: lightweight versioned migrations applied on startup; store `schema_version` in a `meta` table.
