# 02-cloud-gateway — Dev Progress

| Field | Value |
|-------|-------|
| **Spec** | `docs/spec/02-cloud-gateway.md` |
| **Status** | Implemented |
| **Last Updated** | 2026-05-31 |
| **Build** | `go build ./...` passes |
| **Vet** | `go vet ./...` passes |
| **Tests** | `go test ./...` — 11 packages, all passing |

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
| VNC Relay | `gateway/internal/vncrelay/` | Done |
| Credential Vault | `gateway/internal/credvault/` | Done |

## Store Tables Covered

- `users`, `refresh_tokens`, `devices`, `agents`, `jobs`, `job_files`, `files`
- `skills`, `skill_versions`, `device_skills`, `agent_skills`, `skill_grants`
- `organizations`, `audit_log`
- `vnc_sessions`, `browser_credentials`, `job_credentials`

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
- Health: `/healthz`, `/readyz`, `/metrics`
- VNC: `POST /jobs/{id}/vnc`, `DELETE /vnc/{id}`, `POST /vnc/{id}/save-login`, WS `/ws/vnc/{id}`, WS `/session/{id}`
- Credentials: `GET/DELETE /credentials/{id}`

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

- [x] Database migrations (`gateway/migrations/` — up/down SQL created)
- [x] Unit tests (model, config, auth, tunnel, pubsub, pool, channel — 7 packages)
- [x] Integration tests (store layer against real PostgreSQL — 11 tests covering all tables)
- [x] Prometheus metrics (/metrics endpoint, API latency, tunnel frames, job & agent gauges)
- [x] OpenTelemetry tracing stub (InitTracing entry point, ready for OTLP wiring)
- [x] HTTP API handler tests (15 tests: auth, jobs, health — with mock stores + real router)
- [x] Redis-backed tunnel registry (Registry interface + InMemory/SimulatedRedis impls with tests)
- [x] Docker/VNC/credential-vault: vncrelay (session pairing + byte relay + reaper), credvault (AES-256-GCM encrypt/decrypt + capture + inject), new DB tables, new tunnel frames, new HTTP routes + handlers, 13 new tests
- [x] Audit fixes (2026-06-01): device token hashing with SHA-256, VNC_OPENED/CRED_CAPTURE_ACK/CRED_PUSH_ACK inbound handlers, CRED_PUSH at job dispatch, VNC_CLOSE outbound, DispatchToAllDevices + fleet handlers, tenant_scope middleware, password complexity (uppercase/lowercase/digit/special), JWT key rotation with kid

## Git History

- `3c1038a` — feat: implement Cloud Gateway module
- `f758375` — rename: IAgent → oneClickAgent
- `252d80f` — docs(dev): record cloud gateway module implementation status
- `dca4669` — feat: add migrations, align schema to spec, add unit tests
