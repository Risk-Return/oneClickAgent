# 02-cloud-gateway — Dev Progress

| Field | Value |
|-------|-------|
| **Spec** | `docs/spec/02-cloud-gateway.md` |
| **Status** | Implemented (core) — **spec extended** with Docker/VNC/credential-vault feature (see Pending) |
| **Last Updated** | 2026-05-31 |
| **Build** | `go build ./...` passes |
| **Vet** | `go vet ./...` passes |
| **Tests** | `go test ./...` — 9 packages, all passing (7 unit + 1 integration + 1 handler) |

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
- **Pending (new spec):** `vnc_sessions`, `browser_credentials`, `job_credentials` (`06-data-model §1.15–§1.17`)

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
- **Pending (new spec):** VNC sessions (`POST/GET /jobs/{id}/vnc`, `DELETE /vnc/{id}`, `POST /vnc/{id}/save-login`), credential vault (`GET/PATCH/DELETE /credentials`), session relay socket `/session/{id}`, noVNC socket `/ws/vnc/{id}`; job submit `credential_ids`

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

## Pending — Docker / Browser / VNC / Credential-Vault Feature

Spec extended on 2026-05-31 (goal `docker` section). The following must be implemented to proceed; ordered by dependency:

### Data layer
- [ ] Migrations + repos for `vnc_sessions`, `browser_credentials`, `job_credentials` (`06-data-model §1.15–§1.17`).
- [ ] `jobs` submit path: accept + validate `credential_ids` (owned by caller, else 403).

### `credvault/` package (`02 §17`, `08 §13`)
- [ ] AES-256-GCM encrypt/decrypt with `IAGENT_CRED_KEY` (env) and optional `IAGENT_CRED_KMS` (envelope), `key_id` rotation.
- [ ] Capture flow: `CRED_CAPTURE` → verify sha256 → encrypt → insert; `CRED_CAPTURE_ACK`.
- [ ] Inject flow: on dispatch, decrypt in memory → `CRED_PUSH` (chunk if > frame) → `CRED_PUSH_ACK`.
- [ ] Customer routes `GET/PATCH/DELETE /credentials` (tenant-scoped; never return cookie content).
- [ ] Startup guard: refuse credential routes without a configured key.

### `vncrelay/` package (`02 §16`, `05 §9`, `08 §13.1`)
- [ ] `vnc_sessions` registry + single-use `session_token` (hash, 60s TTL, bound to session/device/user).
- [ ] `POST /jobs/{id}/vnc` open flow: assert running + agent VNC-enabled → `VNC_OPEN` → await `VNC_OPENED`.
- [ ] Device session socket `/session/{id}` (subproto `iagent.session.v1`, binary) auth via `session_token`.
- [ ] Browser noVNC socket `/ws/vnc/{id}` (binary) auth via JWT + session ownership.
- [ ] Byte-pump pairing both sockets; per-session buffer cap; idle/max-TTL reaper → `VNC_CLOSE`.
- [ ] Per-user concurrency cap; Redis registry routing for multi-instance (reuse tunnel registry pattern).

### Tunnel codec
- [ ] New frames: `VNC_OPEN/VNC_OPENED/VNC_CLOSE`, `CRED_PUSH/CRED_PUSH_ACK`, `CRED_CAPTURE/CRED_CAPTURE_ACK` (`05 §4.8–§4.9`).
- [ ] `JOB_DISPATCH` payload: add `credential_ids`.

### Config
- [ ] `IAGENT_VNC_IDLE_TTL`, `IAGENT_VNC_MAX_TTL`, `IAGENT_VNC_MAX_SESSIONS_PER_USER`, `IAGENT_VNC_SESSION_BUF_BYTES`, `IAGENT_CRED_KEY`/`IAGENT_CRED_KMS` (`02 §12`).

### Tests
- [ ] Unit: credential encrypt/decrypt round-trip; session token issue/verify.
- [ ] Integration: fake device session socket ↔ noVNC byte relay; `CRED_PUSH`/`CRED_CAPTURE` round-trip with a fake device.

> **Cross-component dependency:** also requires device-side `vncbridge/` + `creds/` (`03 §10–§11`) and the new agent image (`04`); coordinate the agent image build (`10 §4`) so devices can pre-pull before E2E.

## Git History

- `3c1038a` — feat: implement Cloud Gateway module
- `f758375` — rename: IAgent → oneClickAgent
- `252d80f` — docs(dev): record cloud gateway module implementation status
- `dca4669` — feat: add migrations, align schema to spec, add unit tests
