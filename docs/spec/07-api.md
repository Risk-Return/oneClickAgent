# 07 — API Reference

Covers the **Cloud Gateway HTTP/WS API** (web channel), the **channel adapter interface** (Feishu/QQ/etc. stubs), and the **device↔agent HTTP API**. The tunnel protocol is in `05-tunnel-protocol.md`.

Base URL: `https://<gateway-host>/api/v1`. All bodies are JSON unless noted. All timestamps ISO-8601 UTC.

## 1. Conventions

- **Auth**: `Authorization: Bearer <access_jwt>` on all routes except `auth/*` register/login/refresh.
- **Errors**: uniform shape

  ```json
  { "error": { "code": "AGENT_NOT_FOUND", "message": "…", "request_id": "01J…" } }
  ```

- **Pagination**: `?limit=20&cursor=<opaque>` → `{ "items": [...], "next_cursor": "…|null" }`.
- **Idempotency**: mutating POSTs accept `Idempotency-Key` header.
- **Tenant scoping**: every resource access is validated against the authenticated `user_id`.

## 2. Auth

| Method | Path | Body | Result |
|--------|------|------|--------|
| POST | `/auth/register` | `{email, username, password}` | `201 {user, access, refresh}` |
| POST | `/auth/login` | `{email, password}` | `200 {user, access, refresh}` |
| POST | `/auth/refresh` | `{refresh}` | `200 {access, refresh}` (rotates refresh) |
| POST | `/auth/logout` | `{refresh}` | `204` (revokes refresh) |
| GET  | `/auth/me` | — | `200 {user}` |

`access` JWT TTL = 15 min; `refresh` TTL = 30 days, rotating. `user` object includes `tier` (free/pro/enterprise). See `08-auth-security.md`.

## 3. Devices (admin only)

Devices are admin-managed infrastructure. **Customers never see or manage devices.** All routes here require `role=admin` except `/devices/enroll` (called by the device itself with an admin-issued code).

| Method | Path | Notes |
|--------|------|-------|
| GET | `/devices` | **admin** list all managed devices (+ online status) |
| POST | `/devices` | **admin** `{name, description}` → creates device + returns one-time `enrollment_code` |
| GET | `/devices/{id}` | **admin** detail incl. resources, last_seen, hosted agents |
| PATCH | `/devices/{id}` | **admin** rename / describe |
| DELETE | `/devices/{id}` | **admin** decommission device (must be offline or force; reschedules/affects hosted agents) |
| POST | `/devices/{id}/rotate-token` | **admin** issues new `device_token`, revokes old |
| POST | `/devices/enroll` | **called by the device**, body `{enrollment_code}` → `{device_id, device_token}` |

## 4. Agents (pool — admin managed)

Agents are a **pool** of containers maintained by the admin across all devices. Customers never create, own, or manage agents directly. The pool is transparent to customers — they submit jobs and the system allocates an idle agent.

### Admin pool management

| Method | Path | Notes |
|--------|------|-------|
| GET | `/admin/agents` | list all agents across the fleet (pool state, device, current job if busy) |
| GET | `/admin/agents/{id}` | detail incl. status, usage, job history |
| POST | `/admin/agents/{id}/release` | force-release a stuck agent back to `idle` |
| POST | `/admin/agents/{id}/drain` | finish current job then remove from pool |
| DELETE | `/admin/agents/{id}` | remove from pool immediately |
| POST | `/admin/devices/{id}/pool` | set pool size `{size:N}` on a device (scales up/down) |

Default pool size per device = 4 (configurable). Limits per agent: `{ "cpu": 2, "mem_mb": 4096, "disk_mb": 10240 }`.

### Customer visibility

Customers see only the agents **currently allocated to their active jobs**:

| Method | Path | Notes |
|--------|------|-------|
| GET | `/agents` | list agents **currently allocated to the caller's active jobs** — shown as "active agents" with status + usage |
| GET | `/agents/{id}` | detail of a currently allocated agent (only while job is running) |

## 5. Jobs

| Method | Path | Notes |
|--------|------|-------|
| POST | `/jobs` | `{command, params?, file_ids?, skill_id?}` → `201 {job}` if agent allocated immediately (status `DISPATCHED`), or `202 {job}` if queued (status `QUEUED` with queue info). **At most one** `skill_id`; if present it must be `enabled` on the allocated agent, else `422 SKILL_NOT_ENABLED`. Returns `429 QUEUE_FULL` if user at queue cap. |
| GET | `/jobs?agent_id=&status=` | list jobs (paginated) |
| GET | `/jobs/{id}` | full job incl. progress + result + allocated agent info; when `status=QUEUED` also includes `queue_position`, `estimated_wait_seconds` |
| POST | `/jobs/{id}/cancel` | `{reason?}` → routes JOB_CANCEL; on completion, agent is released back to pool and allocator dequeues next |
| GET | `/jobs/{id}/result` | terminal result (progress-level only) |

Job control buttons in the UI map to: submit (`POST /jobs`), cancel (`POST /jobs/{id}/cancel`), status (`GET /jobs/{id}` or WS).

> **One-skill-per-job constraint:** a job runs with **at most one** skill. The submit payload accepts a single optional `skill_id` (never an array); the gateway rejects more than one (`422`).

> **Agent allocation:** on job submit, the gateway selects an `idle` agent from the pool, sets it `busy`, and schedules the job. On terminal state, the agent returns to `idle` and triggers dequeue. Multiple concurrent jobs allocate separate agents.

> **Queue behaviour:** if no idle agent is available, the job enters a tiered FIFO queue (`enterprise > pro > free`). The response includes `queue_position` and `estimated_wait_seconds`. Jobs expire after `IAGENT_QUEUE_TTL` (default 1h) with error `QUEUE_TIMEOUT`. Per-user cap is `IAGENT_MAX_QUEUED_PER_USER` (default 10); exceeding returns `429 QUEUE_FULL`.

## 6. Files

| Method | Path | Notes |
|--------|------|-------|
| POST | `/files` | `multipart/form-data` upload → `201 {file_id, status:"staged_cloud"}` |
| GET | `/files/{id}` | metadata + status |
| DELETE | `/files/{id}` | purge staged copy (if not bound to active job) |

Upload limits configurable (default 100 MB/file). Server computes `sha256`. Files become usable in a job by referencing `file_ids` at submit time.

## 7. Skills

The **admin owns the entire skill lifecycle**; a **customer can only choose which (visible) skills to use**. Four concerns:

1. **Vault** (admin): the catalog + versioned artifacts.
2. **Fleet management** (admin): install / disable / update / delete a skill across **all** local devices.
3. **Visibility** (admin): decide which skills each customer can **see**.
4. **Selection** (customer): pick from the skills visible to them which to use on an agent.

### 7.1 Cloud skill vault (admin)

| Method | Path | Notes |
|--------|------|-------|
| GET | `/admin/skills` | list full vault catalog (all skills + versions + visibility) |
| POST | `/admin/skills` | create catalog entry `{key, name, description, visibility}` |
| GET | `/admin/skills/{id}` | catalog entry + versions + grants |
| POST | `/admin/skills/{id}/versions` | publish a version (multipart: `manifest` + artifact package) → `{version, sha256}` |
| PATCH | `/admin/skills/{id}` | rename / deprecate |
| DELETE | `/admin/skills/{id}` | remove from vault (also removes from all devices) |

### 7.2 Fleet skill management (admin)

Operates across **all local devices** (every agent on every device); the gateway fans out and tracks per-device state. Routed via tunnel `SKILL_ACTION scope=device` to each device.

| Method | Path | Notes |
|--------|------|-------|
| GET | `/admin/skills/{id}/rollout` | per-device install status of a skill across the fleet |
| POST | `/admin/skills/{id}/install` | `{version?}` install on **all** devices (dispatch + install on every agent) |
| POST | `/admin/skills/{id}/disable` | disable on **all** devices |
| POST | `/admin/skills/{id}/enable` | re-enable on **all** devices |
| POST | `/admin/skills/{id}/update` | `{version}` update to a new vault version on **all** devices |
| DELETE | `/admin/skills/{id}/install` | delete from **all** devices |

> A specific device may be targeted with `?device_id=` for ops/repair, but the default action is fleet-wide.

### 7.3 Skill visibility (admin)

Controls which customers can **see** a skill. `public` skills are visible to all customers; `restricted` skills only to granted **principals** (a customer **or** an organization — every member of a granted org can see it).

| Method | Path | Notes |
|--------|------|-------|
| PATCH | `/admin/skills/{id}/visibility` | set `{visibility:"public"|"restricted"}` |
| GET | `/admin/skills/{id}/grants` | list principals (users + orgs) granted a `restricted` skill |
| POST | `/admin/skills/{id}/grants` | `{principal_type:"user"|"org", principal_id}` grant visibility to a customer **or** organization |
| DELETE | `/admin/skills/{id}/grants/{principal_type}/{principal_id}` | revoke visibility |

### 7.4 Customer selection

A customer sees only skills **visible** to them, and may set skill preferences per agent **type** (by tags). When an agent is temporarily allocated, it uses the skill preferences that match its tags. Skills must also be **installed** on the device hosting that agent.

| Method | Path | Notes |
|--------|------|-------|
| GET | `/skills` | list skills **visible to the caller** (resolved as `public` ∪ direct grants ∪ the caller's org grants) |
| POST | `/jobs` | `{command, skill_id?}` — select **at most one** skill for the job; the allocated agent must have it enabled, else `422 SKILL_NOT_ENABLED` |

Skill preferences are per-job (via `skill_id` at submit time), not per-agent — since agents are ephemeral and pooled. The agent's enabled skills are determined by which skills are `installed` on the host device (admin-managed).

Skill `manifest` is a declarative JSON document (capability name, entrypoint, params schema, resource hints). Implementation detail of execution lives in the agent runtime (`04-agent-container.md`). Admin-only routes require `role=admin` (see `08-auth-security.md`).

## 8. Organizations & Membership (admin)

A customer may be standalone (single) or belong to an **organization** (group). Admins manage orgs and membership; granting a skill to an org makes it visible to **every** member at once (see §7.3).

| Method | Path | Notes |
|--------|------|-------|
| GET | `/admin/orgs` | list organizations |
| POST | `/admin/orgs` | `{name, description?}` create organization |
| GET | `/admin/orgs/{id}` | detail + members + granted skills |
| PATCH | `/admin/orgs/{id}` | rename / describe |
| DELETE | `/admin/orgs/{id}` | delete (members revert to single; org grants removed) |
| GET | `/admin/orgs/{id}/members` | list member users |
| POST | `/admin/orgs/{id}/members` | `{user_id}` add a customer to the org (sets `users.org_id`) |
| DELETE | `/admin/orgs/{id}/members/{user_id}` | remove a customer from the org |

A user belongs to at most one organization. Changing membership re-resolves that user's visible-skill set immediately.

### 8.1 User Tier Management (admin)

| Method | Path | Notes |
|--------|------|-------|
| PATCH | `/admin/users/{id}/tier` | `{tier:"free"|"pro"|"enterprise"}` set a customer's tier for queue priority. Changes affect subsequent job submissions. |

## 9. Realtime — Web WebSocket

- **Endpoint**: `wss://<gateway-host>/ws` with `?token=<access_jwt>` or `Authorization` header.
- **Subprotocol**: `iagent.web.v1`.
- Client → server:

  ```json
  { "type": "subscribe", "topics": ["job:01J…", "agent:01J…", "device:01J…"] }
  ```

- Server → client events (progress-level only):

  | Event | Payload |
  |-------|---------|
  | `job.progress` | `{ job_id, status, percent, message, ts }` |
  | `job.queue_update` | `{ job_id, queue_position, estimated_wait_seconds }` (sent when position changes) |
  | `job.result` | `{ job_id, status, result?, error?, finished_at }` |
  | `agent.status` | `{ agent_id, status, usage }` |
  | `device.status` | `{ device_id, status, last_seen }` |
  | `skill.status` | `{ device_id, skill_id, version, status }` (device-wide install progress) |

- Subscriptions are tenant-scoped; the server rejects topics the user does not own.

## 10. Channel Adapter Interface (multi-channel)

Only **web** is implemented now. Other channels (Feishu, QQ, …) plug in behind a normalized interface so the core never changes.

```
ChannelAdapter:
  parse_inbound(raw_event) -> CanonicalCommand{ user_ref, command, params, files[], skill_id? }
  send_outbound(user_ref, OutboundMessage{ kind:"progress"|"result", payload })
  authenticate(raw_event) -> user_id            # maps channel identity → IAgent user
```

- Each adapter is a separate module registered with a `channel` key; inbound events are converted to the same internal job-submit path used by web.
- Account linking (channel identity ↔ IAgent user) is required before a non-web channel can submit jobs. (Linking flow left as a stub for now.)

## 11. Device ↔ Agent HTTP API (intra-device)

The device talks to each agent container over `http://127.0.0.1:<agent_port>`. Detailed in `04-agent-container.md`; summarized:

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/healthz` | liveness/readiness |
| GET | `/status` | current job + resource self-report |
| POST | `/jobs` | `{job_id, command, params, inputs_dir, skill_id?}` start one job (at most one skill) |
| GET | `/jobs/{id}` | poll progress (fallback to webhook) |
| POST | `/jobs/{id}/cancel` | cancel running job |
| GET | `/skills` | list installed skills + status |
| POST | `/skills` | install/update a skill `{skill_id, version, manifest, artifact_path}` |
| POST | `/skills/{id}/disable` / `/enable` | toggle a skill on this agent |
| DELETE | `/skills/{id}` | remove a skill |

Agent → device progress is delivered via callback `POST {device_callback}/jobs/{id}/events` (preferred) or polling.

## 12. Health & Ops (gateway)

| Method | Path | Notes |
|--------|------|-------|
| GET | `/healthz` | gateway liveness |
| GET | `/readyz` | DB + tunnel hub readiness |
| GET | `/metrics` | Prometheus (internal network only) |

## 13. Rate Limits

- Auth endpoints: per-IP + per-account throttling (e.g., 10/min login).
- Job submit: per-user burst cap; agent single-job concurrency enforced downstream.
- WS subscriptions: capped per connection.
