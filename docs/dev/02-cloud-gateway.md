# 02-cloud-gateway — Dev Progress

| Field | Value |
|-------|-------|
| **Spec** | `docs/spec/02-cloud-gateway.md` |
| **Status** | Implemented |
| **Last Updated** | 2026-05-31 |
| **Build** | `go build ./...` passes |
| **Vet** | `go vet ./...` passes |

## Packages Implemented

| Package | Path | Status |
|---------|------|--------|
| Entry Point | `gateway/cmd/gateway/main.go` | Done |
| Config | `gateway/internal/config/` | Done |
| Model | `gateway/internal/model/` | Done |
| Observability | `gateway/internal/obs/` | Done |
| Auth (JWT/Password/RBAC) | `gateway/internal/auth/` | Done |
| Store (PostgreSQL repos) | `gateway/internal/store/` | Done |
| Tunnel Hub | `gateway/internal/tunnel/` | Done |
| PubSub Broker | `gateway/internal/pubsub/` | Done |
| Agent Pool & Allocator | `gateway/internal/pool/` | Done |
| File Relay | `gateway/internal/relay/` | Done |
| Skill Vault & Dispatch | `gateway/internal/skillvault/` | Done |
| Channel Adapters | `gateway/internal/channel/` | Done |
| HTTP API (Router + Handlers) | `gateway/internal/httpapi/` | Done |

## Store Tables Covered

- `users`, `refresh_tokens`, `devices`, `agents`, `jobs`, `job_files`, `files`
- `skills`, `skill_versions`, `device_skills`, `agent_skills`, `skill_grants`
- `organizations`, `audit_log`

## API Endpoints Covered

- Auth: register, login, refresh, logout, me
- Devices (admin): enroll, list, get, delete, set-pool-size, rotate-token
- Agents: list-my-agents, get, enable-skill, disable-skill; admin: list-all, drain, release
- Jobs: submit (with queue), list, get, cancel, result
- Files: upload, list, get, delete
- Skills: list-visible, get; admin: CRUD, publish-version, fleet-install/disable/update/delete, visibility, grants
- Orgs (admin): CRUD, members
- Users (admin): update-tier
- WebSocket: `/ws` realtime endpoint
- Health: `/healthz`, `/readyz`

## Key Design Decisions Matched to Spec

- Tiered FIFO job queue (`enterprise` > `pro` > `free`) per `02 §10`
- Queue TTL with `IAGENT_QUEUE_TTL`, per-user cap `IAGENT_MAX_QUEUED_PER_USER`
- Agent pool: `IDLE → BUSY → IDLE` lifecycle via DB-stored allocation
- Reverse tunnel hub with read/write pumps, ack tracking, liveness checker
- Chunked file push (256KiB) with SHA-256 + backpressure (≤8 in-flight)
- Skill visibility: `public` / `user grant` / `org grant` resolution
- Skill fleet dispatch: `DISPATCH_BEGIN → CHUNK → DISPATCH_END → SKILL_ACTION`
- JWT access + rotating refresh tokens with theft detection
- Argon2id password hashing with 12-char minimum

## Known Gaps / TODOs

- [ ] Database migrations (`gateway/migrations/` is empty — DDL SQL needed)
- [ ] Unit tests (all packages)
- [ ] Integration tests (testcontainers + fake device WS client)
- [ ] Prometheus metrics wiring (obs package has placeholder)
- [ ] OpenTelemetry tracing
- [ ] Redis-backed tunnel registry for multi-instance (v1 is single-instance)
- [ ] `dispatch.go`: `Stat()` call uses undocumented interface — replace with proper approach
- [ ] `IAGENT_FILE_STORE` `local:` prefix stripping before passing to relay

## Git History

- `3c1038a` — feat: implement Cloud Gateway module
- `f758375` — rename: IAgent → oneClickAgent
