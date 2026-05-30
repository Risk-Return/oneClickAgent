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

## 10. Agent Scheduling & Placement

Customers own agents but never choose a device. On `POST /agents` the gateway's **scheduler** assigns the agent to an admin-managed device with available capacity (resource fit, tags, online status), records `agents.device_id`, and provisions the container via `AGENT_CREATE`. Admins may reassign via `PATCH /agents/{id}`. Placement is invisible to the customer.

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

## 13. Observability

- Structured logs (`slog`) with `request_id`, `user_id`, `device_id`.
- Metrics: tunnel count, frames in/out, ack retransmits, job state transitions, API latency.
- Tracing: OpenTelemetry spans across submit → route → result (optional v1).

## 14. Failure Handling

| Scenario | Behavior |
|----------|----------|
| Device offline at submit | `409 DEVICE_OFFLINE` (UI shows actionable error) |
| Tunnel drops mid-job | keep job RUNNING; reconcile on reconnect via STATE_SYNC |
| Duplicate tunnel frame | idempotent handler (by `job_id`+`event_seq`), ACK |
| DB unavailable | `/readyz` fails; reject writes with `503` |
| Gateway shutdown | drain WS with `1001`, devices reconnect |

## 15. Testing

- Unit: auth, tenant scoping, frame codec, ack/retransmit, job state transitions, skill dispatch + reconcile.
- Integration: ephemeral PostgreSQL (testcontainers), fake device WS client simulating tunnel.
- E2E: with real device + agent (see `10-deployment.md`).
