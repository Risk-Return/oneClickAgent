# 02 — Cloud Gateway (Go)

The only internet-facing component. Serves the web API + realtime WS, terminates the device tunnels, enforces auth/tenancy, relays files, and owns the canonical PostgreSQL store.

## 1. Goals & Non-Goals

**Goals**
- Single safe edge: TLS termination, authn/authz, tenant isolation.
- Tunnel hub: maintain a live registry of devices and route frames.
- Stateless horizontally-scalable HTTP/WS layer (state in PostgreSQL + tunnel affinity).

**Non-Goals**
- Running agents or Docker.
- Persisting user input files long-term (relay + short staging only).
- Exposing terminals or raw agent internals.

## 2. Module Layout

```
gateway/
├── cmd/gateway/main.go            # wiring, config, graceful shutdown
└── internal/
    ├── config/                    # env + file config, validation
    ├── httpapi/                   # chi routers, handlers, middleware
    │   ├── auth/ devices/ agents/ jobs/ files/ skills/ ws/
    ├── auth/                      # JWT issue/verify, password hashing, RBAC
    ├── tunnel/                    # WS hub, device registry, frame codec, router
    ├── pool/                      # agent pool allocator: select idle agent, allocate, release, scale
    ├── relay/                     # file staging + chunked push over tunnel
    ├── skillvault/                # skill catalog + artifact storage + dispatch
    ├── channel/                   # channel adapter interface (web impl + stubs)
    ├── store/                     # PostgreSQL repositories (pgx)
    ├── model/                     # domain types + DTOs
    ├── pubsub/                    # in-proc fan-out for web WS
    └── obs/                       # logging, metrics, tracing
```

## 3. Key Libraries

- Router: `go-chi/chi`. WS: `gorilla/websocket` (or `nhooyr.io/websocket`).
- DB: `jackc/pgx/v5` + `pgxpool`; migrations via `golang-migrate`.
- JWT: `golang-jwt/jwt/v5`. Hashing: `alexedwards/argon2id`.
- Config: `caarlos0/env` + `.env`. Logging: `log/slog`. Metrics: `prometheus/client_golang`.
- Validation: `go-playground/validator`.

## 4. Tunnel Hub

Central in-memory registry plus per-connection pumps.

```go
type Hub struct {
    mu       sync.RWMutex
    devices  map[DeviceID]*DeviceConn  // online devices
    pending  map[MsgID]*PendingAck     // outstanding acks (per conn, see conn)
}

type DeviceConn struct {
    deviceID DeviceID
    userID   UserID
    ws       *websocket.Conn
    outbound chan Frame      // buffered write queue
    acks     *AckTracker     // retransmit unacked frames
    lastSeen atomic.Int64
}
```

- **Read pump**: decode frames → validate → dispatch to handler (`JOB_PROGRESS`, `JOB_RESULT`, `AGENT_STATUS`, `STATE_SYNC`, `FILE_ACK`, …). Update DB + publish to `pubsub`.
- **Write pump**: drains `outbound`; tracks acks; retransmits per `05-tunnel-protocol §2`.
- **Liveness**: PING/PONG; mark `OFFLINE` + close on 45s silence.
- **Supersede**: a new tunnel for an existing `device_id` closes the old with `4002`.
- **Routing**: gateway → device by looking up `devices[device_id]`; if absent (offline), job submit returns `409 DEVICE_OFFLINE` (or queues per policy).

### Scaling note
Tunnels are sticky to the gateway instance holding the socket. For multi-instance deployments, route device-targeted actions to the owning instance via a shared registry (Redis) + node-to-node forwarding, or pin devices with consistent hashing. v1 targets single-instance; the registry interface is abstracted to allow a Redis-backed implementation later.

## 5. HTTP API Layer

- Routes per `07-api.md`. Middleware chain: `request_id → recover → cors → rate_limit → auth(jwt) → tenant_scope → handler`.
- `tenant_scope` loads the resource and asserts ownership before the handler runs.
- DTO validation at the boundary; domain types never leak DB rows directly.

## 6. Auth Responsibilities

- Register/login/refresh/logout; Argon2id hashing; JWT (access 15m, refresh 30d rotating).
- Device enrollment: validate one-time `enrollment_code`, mint `device_token` (store only hash), support rotation/revocation.
- Detail in `08-auth-security.md`.

## 7. File Relay

```
POST /files → write to staging (local dir or S3-compatible) → row status=staged_cloud
On job submit with file_ids:
  for each not-yet-on-device: open FILE_PUSH_BEGIN → stream FILE_CHUNK (256KiB) → FILE_PUSH_END
  on FILE_ACK(staged_device): update files.status
On JOB_RESULT terminal: schedule cloud staging cleanup (grace window)
```

Backpressure ≤ 8 chunks in flight per file. Integrity via `sha256`.

## 8. Realtime Fan-out

- `pubsub` is an in-process topic broker keyed by `job:{id}`, `agent:{id}`, `device:{id}`, scoped to `user_id`.
- Tunnel read-pump handlers publish events; web WS connections subscribe to owned topics only.

## 9. Skill Vault

The admin-owned source of truth for skills. The admin controls the **entire** lifecycle across the **whole fleet**; a customer only selects from skills made **visible** to them.

- **Storage**: catalog in PostgreSQL (`skills`, `skill_versions`, `skill_grants`); artifacts in the file store (same backend as `relay`, e.g. local disk or S3).
- **Admin vault ops** (`role=admin`): create catalog entries, publish versions (manifest + artifact, `sha256` computed), deprecate/delete. See `07-api.md §7.1`.
- **Fleet dispatch** (`07-api.md §7.2`): an admin install/update/disable/delete targets **all devices**. For each online device the gateway:
  1. records desired state in `device_skills` (status `installing`/`updating`),
  2. streams the skill package over the tunnel (`SKILL_DISPATCH_*`, chunked like files, ≤ 8 chunks in flight),
  3. emits `SKILL_ACTION scope=device`,
  4. updates `device_skills` from `SKILL_STATE` and fans out `skill.status` (rollout) to subscribed admins.
  Offline devices are reconciled on reconnect (below). The `rollout` endpoint reports per-device progress.
- **Visibility** (`07-api.md §7.3`): admin sets `skills.visibility` (`public`/`restricted`) and manages `skill_grants` targeting a **user or an organization**. A skill is visible to a customer if `public`, granted to them, **or granted to their org** (`users.org_id`). The customer-facing catalog (`GET /skills`) returns only visible skills; non-visible skills are never disclosed.
- **Organizations** (`07-api.md §8`): admins create groups and assign members; granting a skill to an org makes it visible to every member. Resolution joins `users.org_id` → `skill_grants(principal_type='org')`.
- **Customer selection** (`07-api.md §7.4`): enable/disable routes `SKILL_ACTION scope=agent`; the gateway validates **ownership + visibility + installed-on-device** before routing.
- **Reconciliation**: on tunnel (re)connect the gateway sends `SKILL_SYNC` (full desired `device_skills`/`agent_skills`) so devices converge after downtime.
- **Authorization**: vault, fleet, and visibility routes require `role=admin`; customer selection requires agent ownership + skill visibility (`08-auth-security.md`).

## 10. Agent Pool & Per-Job Allocation

**Agents are pooled, not customer-owned.** The admin maintains a pool of agent containers across devices. When a customer submits a job, the gateway's **allocator** picks an idle agent from the pool.

### Pool lifecycle (admin-managed)

- Admin configures pool size per device (e.g., `IAGENT_POOL_SIZE=4`).
- On device enrollment/reconnect, the gateway ensures the desired number of agent containers via `AGENT_CREATE` frames.
- Agents are created with `status=IDLE`, `user_id=NULL` — no customer association.
- The allocator tracks pool state in `agents` table: `IDLE` agents are available; `BUSY` agents are assigned to a customer's job.

### Allocation flow (job submit)

```
POST /jobs:
  1. Check per-user queue cap → if exceeded, 429 QUEUE_FULL
  2. Persist job (status=PENDING)
  3. Allocator: SELECT one IDLE agent (FIFO or resource-fit)
     3a. If found:
         SET agent.user_id = job.user_id, agent.status = BUSY
         job.status = DISPATCHED → Route JOB_DISPATCH
         Return 201
     3b. If NOT found:
         job.status = QUEUED
         SET queued_at = now(), queue_expires_at = now() + TTL
         Return 202 { queue_position, estimated_wait_seconds }
  4. On job terminal (SUCCEEDED/FAILED/CANCELLED):
     SET agent.user_id = NULL, agent.status = IDLE
     Wake-up allocator → dequeue next job (see above)
```

### Job Queue (when all agents are occupied)

When no idle agent is available at job submission, the job enters a **gateway-side queue** (`status=QUEUED`) and waits for an agent to become free.

#### Queue ordering

```
ORDER BY tier_priority ASC, created_at ASC
```

- Primary: **user tier** (`enterprise` → `pro` → `free`)
- Secondary: FIFO within tier (earliest submitted first)

Tier priority mapping:
| Tier | Priority |
|------|----------|
| `enterprise` | 0 (highest) |
| `pro` | 1 |
| `free` | 2 (lowest) |

Tier is set by admin on the user record; it affects queue position only, not resource limits.

#### Wake-up mechanism

On every agent release (job terminal state), the allocator runs:

```
1. SELECT * FROM jobs WHERE status = 'QUEUED'
   ORDER BY user_tier ASC, created_at ASC
   LIMIT 1
2. If found: allocate agent → JOB_DISPATCH
3. Repeat until no queued job or no idle agent
```

When a new device comes online or a scaled-up pool adds idle agents, the same wake-up runs.

#### Queue TTL

- Configurable `IAGENT_QUEUE_TTL` (default 1 hour).
- Each queued job records `queued_at` and `queue_expires_at`.
- A lightweight background ticker (every 30s) or the dequeue check expires jobs:

```
queued_at + TTL < now() → status = FAILED, error_code = QUEUE_TIMEOUT
```

The UI shows a friendly "Job expired in queue" message.

#### Per-user queue cap

- Configurable `IAGENT_MAX_QUEUED_PER_USER` (default 10).
- If a user already has this many QUEUED jobs, `POST /jobs` rejects with `429 QUEUE_FULL`.
- Running jobs do not count toward the cap; only queued ones.

#### API responses

| Scenario | Status | Body |
|----------|--------|------|
| Agent allocated immediately | `201` | `{job, status: "DISPATCHED", agent_id, ...}` |
| Job queued (no idle agent) | `202` | `{job, status: "QUEUED", queue_position: N, estimated_wait_seconds: M, ...}` |
| User at queue cap | `429` | `{error:{code:"QUEUE_FULL", message:"Max 10 queued jobs"}}` |
| Queue TTL expired | (on GET) | `status:"FAILED", error_code:"QUEUE_TIMEOUT"` |

### Admin pool ops

| Action | API |
|--------|-----|
| Set pool size per device | `POST /admin/devices/{id}/pool` `{size:N}` |
| List pool (all agents + status) | `GET /admin/agents` |
| Drain an agent (finish current job, then remove) | `POST /admin/agents/{id}/drain` |
| Force-release a stuck agent | `POST /admin/agents/{id}/release` |

### Pool state machine

```
CREATING → IDLE ──(allocate)──→ BUSY ──(job done)──→ IDLE
              │                    │
              └──→ UNHEALTHY ──→ FAILED → REMOVED
                                    ↑
              BUSY ──→ UNHEALTHY ───┘ (job FAILED, agent recycled)
```

## 11. Channel Layer

- `channel.Adapter` interface (see `07-api.md §10`). Web adapter implemented; Feishu/QQ registered as no-op stubs returning `NOT_IMPLEMENTED`.
- Inbound from any channel funnels into the same internal `SubmitJob(userID, agentID, cmd, params, fileIDs, skillID)` use-case, where `skillID` is **at most one** enabled skill (validated against `agent_skills`).

## 12. Configuration (env)

| Var | Default | Purpose |
|-----|---------|---------|
| `IAGENT_HTTP_ADDR` | `:8080` | API/WS listen |
| `IAGENT_TLS_CERT` / `IAGENT_TLS_KEY` | — | TLS (or terminate at LB) |
| `IAGENT_DB_URL` | — | PostgreSQL DSN |
| `IAGENT_JWT_SECRET` | — | HS256 signing key (or RS256 keypair) |
| `IAGENT_ACCESS_TTL` | `15m` | |
| `IAGENT_REFRESH_TTL` | `720h` | |
| `IAGENT_FILE_STORE` | `local:/var/iagent/files` | staging backend |
| `IAGENT_MAX_UPLOAD_MB` | `100` | |
| `IAGENT_HEARTBEAT_S` | `15` | tunnel heartbeat |
| `IAGENT_AGENTS_PER_USER` | `1` | default cap |
| `IAGENT_QUEUE_TTL` | `1h` | max time a job waits in queue before QUEUE_TIMEOUT |
| `IAGENT_MAX_QUEUED_PER_USER` | `10` | per-user cap on QUEUED jobs |

## 13. Observability

- Structured logs (`slog`) with `request_id`, `user_id`, `device_id`.
- Metrics: tunnel count, frames in/out, ack retransmits, job state transitions, API latency.
- Tracing: OpenTelemetry spans across submit → route → result (optional v1).

## 14. Failure Handling

| Scenario | Behavior |
|----------|----------|
| Device offline at submit | `409 DEVICE_OFFLINE` (UI shows actionable error) |
| Queue TTL expired | job auto-transitions QUEUED → FAILED with `QUEUE_TIMEOUT` on next dequeue check |
| Queue cap exceeded | `429 QUEUE_FULL` returned to caller (UI shows "too many queued jobs") |
| Tunnel drops mid-job | keep job RUNNING; reconcile on reconnect via STATE_SYNC |
| Duplicate tunnel frame | idempotent handler (by `job_id`+`event_seq`), ACK |
| DB unavailable | `/readyz` fails; reject writes with `503` |
| Gateway shutdown | drain WS with `1001`, devices reconnect |

## 15. Testing

- Unit: auth, tenant scoping, frame codec, ack/retransmit, job state transitions (including queue → dispatch, queue → timeout), allocator dequeue ordering (tier + FIFO), skill dispatch + reconcile.
- Integration: ephemeral PostgreSQL (testcontainers), fake device WS client simulating tunnel, queue with mock pool (all agents busy → queued → release → dequeue).
- E2E: with real device + agent (see `10-deployment.md`).
