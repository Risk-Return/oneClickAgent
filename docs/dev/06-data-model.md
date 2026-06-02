# 06 — Data Model — Dev Record

## Status: Implemented & Aligned

## Scope

Three stores aligned with `docs/spec/06-data-model.md`:

- **Cloud PostgreSQL** — users, devices, agents (pooled), jobs, files, skills, skill_versions, device_skills, agent_skills, organizations, skill_grants, refresh_tokens, audit_log, vnc_sessions, browser_credentials, job_credentials
- **Local SQLite** — device_info, agents, jobs, files, outbox, device_skills, agent_skills, vnc_sessions
- **Agent Ephemeral** — in-memory JobRecord + skills + VNC state + workspace dirs

## Implementation Files

| Layer | Files |
|-------|-------|
| Cloud schema | `gateway/migrations/001_initial_schema.up.sql`, `002_docker_vnc_creds.up.sql`, `003_align_data_model.up.sql` |
| Cloud models | `gateway/internal/model/types.go` |
| Cloud stores | `gateway/internal/store/users.go`, `devices.go`, `agents.go`, `jobs.go`, `files.go`, `skills.go`, `tokens.go`, `audit.go`, `vnc_sessions.go`, `credentials.go`, `orgs.go` |
| Device schema | `device/iagent_device/store/connection.py` |
| Device repos | `device/iagent_device/store/repositories.py` |
| Agent state | `agent/iagent_agent/runtime/context.py`, `agent/iagent_agent/runtime/executor.py`, `agent/iagent_agent/workspace.py` |

## Migration 003 — Alignment Changes

Migration `003_align_data_model` aligns vnc_sessions and browser_credentials with the spec:

### vnc_sessions
- Added `token_expires_at`, `started_at`, `close_reason` columns
- Renamed `closed_at` → `ended_at`
- Added `error` status to `VNCSessionStatus` Go enum
- Retained operational columns (`rfb_password`, `gateway_node`, `idle_ttl_secs`, `max_ttl_secs`, `last_active_at`)

### browser_credentials
- Renamed `ciphertext` → `storage_state_enc`
- Added separate `nonce` and `auth_tag` columns (previously combined in `ciphertext`)
- Removed `size_bytes` column
- Changed UNIQUE from `(user_id, origin)` → `(user_id, label)`
- Added regular index `idx_cred_user_origin ON (user_id, origin)` for origin-lookup queries
- Added ownership validation in `LinkToJob` (credential.user_id must match job.user_id)

### credvault API change
- `Encrypt(plaintext) (EncryptResult, error)` — returns separated `StorageStateEnc`, `Nonce`, `AuthTag`, `SHA256`
- `Decrypt(storageStateEnc, nonce, authTag []byte, expectedSHA256 string) ([]byte, error)`

### jsonb type fixes
- `Job.Params`: `*string` → `*json.RawMessage`
- `Job.Result`: `*string` → `*json.RawMessage`
- `SkillVersion.Manifest`: `string` → `json.RawMessage`
- `AuditLog.Meta`: `*string` → `*json.RawMessage`

### Device SQLite
- `vnc_sessions.closed_at` → `vnc_sessions.ended_at`
- `files.status` initial value: `"staged"` → `"staged_device"`
- Migration tracking: `schema_version` table → `meta` table (key-value, per spec §5)

### Agent
- Added `started_at` to `JobRecord`
- Set `started_at` on job RUNNING state transition in executor

## Test Coverage

- Gateway: store integration tests (`store_test.go`), credvault unit tests (`vault_test.go`), VNC relay tests (`relay_test.go`)
- Device: SQLite repo tests, VNC bridge tests (`test_vnc_bridge.py`)
- Agent: server tests (`test_server.py`)
