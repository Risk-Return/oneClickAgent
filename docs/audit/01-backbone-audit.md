# Audit 01 — Backbone vs Goal

**Date:** 2026-05-30
**Scope:** Verify that the project backbone (specs + skeleton code) fully covers `docs/braionstorm/goal.md`, that all specs have clear design scope, and that cross-component interactions have well-defined protocols/schemas/APIs.

---

## 1. Goal Coverage

Every feature in `goal.md` is addressed by at least one spec document and has a corresponding skeleton in the codebase. No gaps.

| Goal Feature | Spec(s) | Backbone Skeletons |
|---|---|---|
| Friendly web UI to control AI agents | `09-web-ui.md` | `web/src/` (24 .ts/.tsx files) |
| Safe Gateway | `02-cloud-gateway.md`, `08-auth-security.md` | `gateway/` (36 .go files) |
| Multi-channel (web now, others stubbed) | `07-api.md §10`, `02 §11` | `gateway/internal/channel/adapter.go` |
| Multi-agent per Docker container | `04-agent-container.md`, `03 §7` | `agent/` (12 .py files), `device/docker/manager.py` |
| Multi-local-device | `02`, `03-local-device.md` | `gateway/internal/tunnel/hub.go`, `device/` (24 .py files) |
| Web UI: command + upload + skills + send/cancel/status + results | `09-web-ui.md` | `web/src/pages/JobsPage.tsx`, `web/src/components/FileDropzone.tsx`, `web/src/components/SkillSelector.tsx` |
| No terminal access, progress-only | `04 §3`, `05 §4.3`, `09 §2` | Cross-cutting constraint enforced in all channel layers |
| User registration and authentication | `08 §2`, `07 §2` | `gateway/internal/auth/`, `web/src/auth/` |
| Per-user agent cap (default 1) | `02 §12` (`IAGENT_AGENTS_PER_USER`) | Pool model — agents allocated per job, released after |
| Gateway flow (User→Gateway→Device→Agent→Result) | `01-architecture.md §3.2` | `gateway/internal/relay/`, `device/jobs/dispatcher.py` |
| Local device registration, receive commands, send results, manage Docker agents | `03-local-device.md` | `device/tunnel/client.py`, `device/docker/manager.py`, `device/jobs/dispatcher.py` |
| Agent container: receive commands, receive files, remove data after job, send results, manage skills, limited resources, monitoring, health check, recovery | `04-agent-container.md`, `03 §7-§10` | `agent/server.py`, `agent/workspace.py`, `agent/skills/loader.py`, `device/docker/manager.py`, `device/monitor/monitor.py`, `device/docker/reconcile.py` |
| User data in database | `06-data-model.md §1.1-§1.6` (PostgreSQL) | `gateway/internal/store/` (users, agents, jobs, files repos) |
| Local device data in database | `06-data-model.md §2` (SQLite) | `device/store/connection.py`, `device/store/repositories.py` |
| Cloud gateway data in database | `06-data-model.md §1` (PostgreSQL) | `gateway/internal/store/` (8 repo files, 15 tables) |
| Tunnel management (create, status, transfer, security, recovery) | `05-tunnel-protocol.md` | `gateway/internal/tunnel/`, `device/tunnel/` |
| Development (gateway installation, device installation) | `10-deployment.md` | `deploy/cloud/docker-compose.yml`, `deploy/device/iagent-device.service`, `Makefile` |
| Skill management (install/disable/update/delete fleet-wide, vault) | `02 §9`, `07 §7.1-§7.3`, `03 §9`, `04 §5` | `gateway/internal/skillvault/`, `device/skills/manager.py`, `agent/skills/loader.py` |
| Skill selection by user (per job, one skill max) | `07 §7.4`, `04 §5` | `gateway/internal/httpapi/skills_handler.go`, `web/src/components/SkillSelector.tsx` |
| Organizations/groups | `07 §8`, `06 §1.11` | `gateway/store/orgs.go`, `web/pages/admin/OrganizationsPage.tsx` |
| Queue (tiered FIFO, TTL, per-user cap) | `02 §10`, `07 §5` | `gateway/internal/pool/allocator.go` |
| Cross-platform (Windows + macOS + Linux) | `00 §4`, `03 §3`, `10` | Constraint; device uses `platformdirs`, `pathlib` |
| Agent resource limits (cpu=2, mem=4GB, disk=10GB) | `04 §6`, `03 §7` | `device/docker/manager.py` container create flags |

**Verdict: 0 uncovered features. All 19+ goal requirements have spec + skeleton coverage.**

---

## 2. Spec Design Scope & Clarity

### 2.1 Scope boundaries — clear per document

| Spec | Owns | Does NOT leak into |
|---|---|---|
| `00-overview.md` | Topology, glossary, tech stack decisions | Implementation detail |
| `01-architecture.md` | Component layers, sequences, state machines, data flow | API routes, DB schemas |
| `02-cloud-gateway.md` | Go module layout, tunnel hub, HTTP layer, file relay, skill vault, pool allocator, config | Device or agent internals |
| `03-local-device.md` | Tunnel client, Docker pool, job dispatch, file staging, skills, monitor, SQLite | Gateway API or auth |
| `04-agent-container.md` | HTTP API contract, workspace, skill lifecycle, brain adapter protocol | Tunnel protocol or device management |
| `05-tunnel-protocol.md` | Framing, message types, ack/retransmit, close codes, file/skill chunking | HTTP API or UI |
| `06-data-model.md` | PostgreSQL + SQLite schemas, retention policies, ER relationships | API routes or component logic |
| `07-api.md` | REST + WS endpoints, channel adapter interface, device↔agent API | Tunnel framing or UI screens |
| `08-auth-security.md` | Auth flows, RBAC, tenant scoping, hardening, threat model | Deployment or data models |
| `09-web-ui.md` | Page inventory, component states, realtime integration, UX | Backend logic or tunnel |
| `10-deployment.md` | Builds, compose files, env vars, CI/CD, E2E verification | Auth or data models |

### 2.2 Intentionally deferred features (not vague — explicitly stubbed)

| Feature | Where | Status |
|---|---|---|
| Multi-instance tunnel registry (Redis) | `02 §4` | v1 single-instance; registry interface abstracted for future swap |
| `FILE_PULL_*` for large result artifacts | `05 §5` | Marked "reserved"; small results use `JOB_RESULT` payload |
| Non-web channel (Feishu/QQ) account linking | `07 §10`, `02 §11` | Adapter interface defined; Feishu/QQ registered as no-op stubs |
| Agent brain implementations | `04 §2` | `AgentBrain` protocol defined; `brain_stub.py` for testing; concrete brains swappable |

### 2.3 Issues resolved in this audit

| # | Issue | Resolution |
|---|---|---|
| 1 | `06-data-model.md` line 5 said `"agents (customer-owned)"`, contradicting the pooled/temporary-allocation model used everywhere else | Changed to `"agents (admin-managed pool, temporarily allocated per job)"` |
| 2 | `09-web-ui.md` §3.3 said customer sees allocated agents with `"status (IDLE/BUSY)"`, but an allocated agent is always BUSY | Changed to `"status (BUSY)"` only |
| 3 | `device/pyproject.toml`, `agent/pyproject.toml`, and `web/package.json` had no dependency declarations | Added `[project.dependencies]` and `devDependencies` matching spec §3 libraries |

---

## 3. Cross-Component Interaction Protocols

### 3.1 Protocol matrix

| Interface | Direction | Protocol | Defined In | Status |
|---|---|---|---|---|
| Web UI ↔ Gateway (REST) | Bidirectional | HTTPS + JSON | `07-api.md §2-§12` | All endpoints specified with method, path, body, response codes |
| Web UI ↔ Gateway (WS realtime) | Bidirectional | WSS + JSON | `07-api.md §9` | Topics (`job:{id}`, `agent:{id}`, `device:{id}`), event types, tenant scoping |
| Gateway ↔ Device (Tunnel) | Bidirectional | WSS + JSON frames | `05-tunnel-protocol.md` | 24 message types, envelope schema, ack/retransmit, chunked file/skill, close codes |
| Device ↔ Agent (HTTP) | Unidirectional (device→agent) | HTTP + JSON | `04-agent-container.md §3` | 10 endpoints with exact request/response shapes |
| Agent → Device (progress callback) | Unidirectional | HTTP POST | `04-agent-container.md §3` | `POST {callback_url}/jobs/{job_id}/events` with event schema |
| Gateway ↔ PostgreSQL | Unidirectional | SQL | `06-data-model.md §1` | 15 tables with columns, types, constraints, indexes |
| Device ↔ SQLite | Unidirectional | SQL | `06-data-model.md §2` | 7 tables with DDL, WAL mode specified |
| Channel Adapters | Internal | Go interface | `02 §11`, `07 §10` | `ParseInbound` / `SendOutbound` / `Authenticate` |

### 3.2 Internal module contracts (informal — not formal interfaces)

| Between | Interaction | Clarity |
|---|---|---|
| `pool/allocator.go` ↔ `store/agents.go` + `store/jobs.go` | Allocator queries `FindIdleAgent()`, `DequeueJob()`, `CountQueuedByUser()` from store | Good — spec describes behavior; store docstrings name expected methods |
| `device/jobs/dispatcher.py` ↔ `tunnel/outbox.py` ↔ `tunnel/client.py` | Dispatcher enqueues frames to outbox; outbox flushes via tunnel client on connect | Good — standard Python module dependency pattern |
| `gateway/skillvault/dispatch.go` ↔ `device/skills/manager.py` | Skill packages sent via `SKILL_DISPATCH_*` tunnel frames | Good — wire format in `05 §4.6`; device-side handler in `03 §9` |
| `device/skills/manager.py` ↔ `agent/skills/loader.py` | Device pushes skills to agent via `POST /skills` | Good — agent HTTP API in `04 §3`; device fan-out in `03 §9` |
| `gateway/pubsub/pubsub.go` ↔ `httpapi/ws_handler.go` | PubSub fan-out for WS subscriptions | Good — topic names and event schemas in `07 §9`; Go interface implicit in docstrings |

### 3.3 UUID and addressing

All routable entities use UUIDv7. Routing keys: `(user_id, agent_id, job_id)`. Device resolution internally maps `agent_id → device_id`. This is consistent across all specs and backbone files.

---

## 4. Backbone File Inventory

### Gateway (Go) — 36 files, all stubs with docstrings

| Package | Files | Covers |
|---|---|---|
| `cmd/gateway` | `main.go` | Wiring, config, graceful shutdown |
| `internal/config` | `config.go` | Env config + `.env` loading |
| `internal/model` | `types.go` | Domain types, DTOs, enums |
| `internal/auth` | `jwt.go`, `password.go`, `rbac.go` | JWT, Argon2id, RBAC + tenant scoping |
| `internal/store` | `pgx.go`, `users.go`, `devices.go`, `agents.go`, `jobs.go`, `files.go`, `skills.go`, `orgs.go`, `audit.go` | PostgreSQL repos for all 15 tables |
| `internal/tunnel` | `hub.go`, `device_conn.go`, `codec.go`, `router.go` | Tunnel hub, per-conn pumps, framing, frame dispatch |
| `internal/pool` | `allocator.go` | Agent pool allocation, dequeue, queue expiry |
| `internal/relay` | `relay.go` | File staging + chunked push |
| `internal/skillvault` | `vault.go`, `dispatch.go` | Skill catalog + fleet dispatch |
| `internal/channel` | `adapter.go` | Multi-channel adapter interface |
| `internal/pubsub` | `pubsub.go` | In-process event fan-out |
| `internal/obs` | `obs.go` | Structured logging, metrics, tracing |
| `internal/httpapi` | `router.go`, `middleware.go`, `auth_handler.go`, `devices_handler.go`, `agents_handler.go`, `jobs_handler.go`, `files_handler.go`, `skills_handler.go`, `admin_handler.go`, `ws_handler.go` | All REST + WS routes |

### Device (Python) — 24 files, all stubs with docstrings

| Package | Files | Covers |
|---|---|---|
| `iagent_device` | `__init__.py`, `__main__.py`, `config.py` | CLI entrypoint, env config |
| `tunnel` | `__init__.py`, `client.py`, `codec.py`, `outbox.py` | WSS client, framing, durable outbox |
| `jobs` | `__init__.py`, `models.py`, `dispatcher.py` | Job state models, dispatch loop |
| `docker` | `__init__.py`, `manager.py`, `reconcile.py` | Docker pool lifecycle, boot reconciliation |
| `files` | `__init__.py`, `stager.py` | File receive, workspace staging, cleanup |
| `agentclient` | `__init__.py`, `client.py` | Async HTTP client to agent |
| `skills` | `__init__.py`, `manager.py` | Skill dispatch receive, fleet-wide apply |
| `monitor` | `__init__.py`, `monitor.py` | Resource + status sampling |
| `store` | `__init__.py`, `connection.py`, `repositories.py` | SQLite connection, repos for 7 tables |

### Agent (Python) — 12 files, all stubs with docstrings

| Package | Files | Covers |
|---|---|---|
| `iagent_agent` | `__init__.py`, `__main__.py`, `server.py`, `workspace.py` | uvicorn entrypoint, FastAPI routes, workspace mgmt |
| `runtime` | `__init__.py`, `context.py`, `executor.py` | JobContext, ProgressEmitter, job executor |
| `adapter` | `__init__.py`, `protocol.py`, `brain_stub.py` | AgentBrain protocol, stub impl |
| `skills` | `__init__.py`, `loader.py` | Skill install/enable/disable/delete |

### Web (TypeScript/React) — 24 files, all stubs with comments

| Module | Files | Covers |
|---|---|---|
| `api` | `client.ts`, `schemas.ts`, `ws.ts` | REST client, Zod schemas, WebSocket client |
| `auth` | `TokenManager.ts`, `AuthGuard.tsx` | Token lifecycle, route guard |
| `store` | `uiStore.ts` | Zustand: theme, sidebar, toasts |
| `features` | `useAgents.ts`, `useJobs.ts`, `useFiles.ts`, `useSkills.ts` | TanStack Query hooks |
| `components` | `Layout.tsx`, `AgentStatusBadge.tsx`, `ResourceBar.tsx`, `JobProgressCard.tsx`, `FileDropzone.tsx`, `SkillSelector.tsx` | Shared UI components |
| `pages` | `LoginPage.tsx`, `RegisterPage.tsx`, `DashboardPage.tsx`, `AgentsPage.tsx`, `JobsPage.tsx`, `JobHistoryPage.tsx`, `SkillsPage.tsx`, `SettingsPage.tsx`, `NotFoundPage.tsx` | Customer-facing screens |
| `pages/admin` | `DeviceFleetPage.tsx`, `SkillVaultPage.tsx`, `FleetRolloutPage.tsx`, `VisibilityPage.tsx`, `OrganizationsPage.tsx` | Admin console screens |

---

## 5. Verification Summary

| Criterion | Status | Notes |
|---|---|---|
| All goal features have spec coverage | PASS | 19/19 mapped |
| All goal features have backbone skeletons | PASS | 96 stub files across 4 components |
| All specs have clear, non-overlapping scope | PASS | Each spec owns one layer; no functionality duplicated across specs |
| No vague/undefined concepts | PASS | 3 deferred features explicitly flagged; 2 inconsistencies fixed in this audit |
| Cross-component protocols are defined | PASS | 8 interaction interfaces with schemas |
| Package manifests contain dependencies | PASS | `pyproject.toml` x2 + `package.json` updated in this audit |
| Backbone file trees match spec module layouts | PASS | All 4 components align with their respective spec §2 |

---

## 6. Artifacts Updated

| File | Change |
|---|---|
| `docs/spec/06-data-model.md` | Corrected agent ownership phrasing (line 5) |
| `docs/spec/09-web-ui.md` | Removed impossible IDLE status from customer agents view (line 45) |
| `device/pyproject.toml` | Added 8 runtime + 4 dev dependencies |
| `agent/pyproject.toml` | Added 5 runtime + 6 dev dependencies |
| `web/package.json` | Added 13 runtime + 18 dev dependencies + 6 npm scripts |
| `docs/audit/01-backbone-audit.md` | This document |
