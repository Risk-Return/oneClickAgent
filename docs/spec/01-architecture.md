# 01 — Architecture

## 1. Logical Layers

```
┌──────────────────────────────────────────────────────────────────────┐
│ Channel Layer        web (built)  │  feishu / qq / … (adapter stubs)   │
├──────────────────────────────────────────────────────────────────────┤
│ Cloud Gateway (Go)                                                     │
│   ├─ Channel Adapters      normalize inbound → canonical Command       │
│   ├─ Auth & Session        JWT issue/verify, RBAC, tenant scoping      │
│   ├─ Web API (REST/WS)     users, devices, agents, jobs, files, skills │
│   ├─ Tunnel Hub            registry of connected devices + routing     │
│   ├─ File Relay/Staging    accept uploads, stage, push to device       │
│   ├─ Skill Vault           catalog + artifacts; dispatch to devices    │
│   ├─ VNC Relay             pair browser↔device sockets; relay RFB bytes │
│   ├─ Credential Vault      encrypted login cookies; inject per job     │
│   └─ Store (PostgreSQL)    source of truth                             │
├──────────────────────────────────────────────────────────────────────┤
│ Reverse Tunnel (WSS, JSON frames)                                      │
├──────────────────────────────────────────────────────────────────────┤
│ Local Device (Python)                                                  │
│   ├─ Tunnel Client         dial-out, reconnect, frame de/mux           │
│   ├─ Job Dispatcher        queue, route job → agent container          │
│   ├─ Docker Manager        create/start/stop/health/recover containers │
│   ├─ File Stager           receive files, mount into agent, cleanup    │
│   ├─ Skill Manager         cache vault skills; apply to all agents     │
│   ├─ VNC Bridge            TCP↔WS bridge to container RFB (no inbound)  │
│   ├─ Cred Relay            stream login cookies G↔agent (no persist)   │
│   ├─ Monitor               resource & status sampling                  │
│   └─ Store (SQLite)        local device/agent/job/file state           │
├──────────────────────────────────────────────────────────────────────┤
│ Agent Container (Ubuntu image, HTTP API) — one per agent               │
│   ├─ Job Executor          run one job, emit progress, return result   │
│   ├─ Skill Manager         install/update/enable/disable/delete skills │
│   ├─ Browser + VNC         headless camoufox on Xvfb; x11vnc (loopback) │
│   ├─ Toolchain             opencode + node/python/go/rust/java (warmed) │
│   └─ Workspace             ephemeral data + browser profile, wiped      │
└──────────────────────────────────────────────────────────────────────┘
```

## 2. Identity & Addressing

Every routable entity has a stable ID:

- `user_id` — a user. `role=admin` (operator) manages **devices + agent pool**; `role=user` (customer) owns **jobs/files**.
- `org_id` — optional **organization/group** a customer belongs to (null = single user); used by admins to grant skill visibility to a whole group.
- `device_id` — an admin-managed local device; carries a `device_token` for tunnel auth. Customers never see devices.
- `agent_id` — a **pooled** agent container on a device; exists independently of any customer. Temporarily **allocated** to a customer's job and **released** back to the pool on job completion.
- `job_id` — globally unique (UUIDv7 recommended) so it can be correlated across gateway/device/agent.
- `file_id` — globally unique; tracks lifecycle and storage location.
- `session_id` — an interactive **VNC session** bound to a running job's agent; its RFB stream is relayed over a dedicated, on-demand socket (`05-tunnel-protocol §9`).
- `credential_id` — a customer's **encrypted saved login** in the credential vault, optionally injected into a job's agent browser (`06-data-model §1.16`).

Routing key for a command: `(user_id, agent_id, job_id)`. The customer never supplies a `device_id` or `agent_id` directly; on job submission the gateway's **allocator** picks an idle agent from the pool, binds `agent_id` to the job, and internally resolves `agent_id → device_id` for tunnel routing. Tenant validation asserts the job belongs to the authenticated customer (`job.user_id`); the agent itself is pool-scoped, not customer-scoped.

## 3. End-to-End Sequences

### 3.1 Device registration & tunnel bring-up

```
Device boot
  └─ POST /api/v1/devices/enroll  (enrollment_code) ──────────► Gateway
       Gateway: validate code → create device row → return device_id + device_token
  └─ Open WSS /tunnel  (Authorization: Bearer <device_token>) ─► Gateway Tunnel Hub
       Hub: verify token → mark device ONLINE → register in routing table
  └─ Device sends HELLO frame (agents, capabilities, resources)
  └─ Heartbeats every 15s; Hub marks OFFLINE after miss threshold
```

### 3.2 Submit a job (web)

```
(Prereq: admin maintains a pool of idle agent containers on devices.)
User → Web UI → POST /api/v1/jobs {command, file_ids[], skill_id?}
Gateway:
  1. Persist job (status=PENDING)
  2. **Allocate**: pick an idle agent from the pool (agent.user_id = NULL, status=idle)
     → set agent.user_id = job.user_id, agent.status = busy
     → resolve host device from agent.device_id
  3. If files referenced: ensure staged on device (see 3.4)
  4. Route JOB_DISPATCH frame over tunnel → device
Device:
  5. Persist local job (status=QUEUED) → dispatch to the allocated agent container
Agent:
  6. status RUNNING → emits PROGRESS events
Device → Gateway: relays JOB_PROGRESS frames (status, percent, message)
Gateway: updates job row + fan-out to subscribed Web UI (WS)
Agent: returns RESULT → Device → Gateway → job status SUCCEEDED/FAILED
Web UI: shows result; Device triggers file cleanup for that job
**Release**: Gateway sets agent.user_id = NULL, agent.status = idle
  — agent returns to pool, ready for the next customer's job
```

### 3.3 Cancel / status

```
Cancel: Web → POST /jobs/{job_id}/cancel → Gateway → JOB_CANCEL frame → Device → agent /cancel
Status: pull via GET /jobs/{job_id}; live via WS subscription channel job:{job_id}
```

### 3.4 File upload & lifecycle

```
Upload: Web → POST /files (multipart) → Gateway stages in object/temp store → file_id (status=STAGED_CLOUD)
On job submit referencing file_ids:
  Gateway → FILE_PUSH frames (chunked) over tunnel → Device writes to job workspace (status=STAGED_DEVICE)
  Device mounts workspace into agent container (read-only inputs dir)
On job terminal state:
  Device deletes job workspace files; reports FILE_PURGED → Gateway marks file PURGED
  Gateway removes its staged copy per retention policy
```

### 3.5 Skill lifecycle (admin) & selection (customer)

```
Admin fleet-wide install (ALL devices → ALL agents):
  Admin → POST /admin/skills/{id}/install {version} → Gateway  (AuthZ role=admin)
  Gateway: for each device → record device_skills (status=installing)
         → SKILL_DISPATCH_* (chunked package from vault) over tunnel → Device caches + verifies sha256
         → SKILL_ACTION scope=device action=install → Device installs on every agent (POST agent /skills)
  Device → SKILL_STATE (per-agent: installed or error+message) → Gateway updates device_skills + agent_skills
         → skill.status rollout to admins (WS) with per-agent granularity
  Admin disable/update/delete: same fan-out with action=disable|update|delete.

Retry failed agents (per device, per agent):
  Admin → POST /admin/skills/{id}/retry {device_id, agent_ids?:[]} → Gateway
         → SKILL_RETRY {skill_id, agent_ids} over tunnel → Device retries install on specified agents
  Device → SKILL_STATE per agent → Gateway updates agent_skills

Admin visibility (per customer OR per organization/group):
  Admin → PATCH /admin/skills/{id}/visibility {public|restricted}
        + POST/DELETE /admin/skills/{id}/grants {principal_type:user|org, principal_id}
  Admin → POST /admin/orgs, /admin/orgs/{id}/members → group customers
  → A skill is visible to a customer if public, granted to them, or granted to their org. (No device traffic.)

Customer selection (visible + installed only):
  Customer → GET /skills → only skills visible to them (public ∪ user grants ∪ org grants)
  Customer → POST /agents/{id}/skills {skill_id} → Gateway verifies: owns agent + skill visible + installed on host device
           → SKILL_ACTION scope=agent action=enable → Device → POST agent /skills/{id}/enable   (DELETE → disable)

Run a job (one skill max):
  Customer → POST /agents/{id}/jobs {command, skill_id?}  // AT MOST ONE skill
  Gateway verifies skill_id (if any) is ENABLED on the agent → JOB_DISPATCH {…, skill_id?} → agent runs with that single skill.

Reconnect: Gateway sends SKILL_SYNC (desired device_skills + agent_skills) so the device converges.
New agent: device applies all 'installed' fleet skills before the agent goes RUNNING.
```

### 3.6 Interactive browser (VNC) & login cookies

```
Inject saved logins on dispatch (customer attached credential_ids):
  Gateway decrypts each credential (AES-256-GCM) in memory
    → CRED_PUSH {storage_state} over tunnel → Device → POST agent /browser/state
  Agent loads cookies into /work/profile before brain.run; wiped on job terminal.

Open a live VNC session (job RUNNING):
  Customer → POST /jobs/{id}/vnc → Gateway: vnc_sessions row + single-use session_token
           → VNC_OPEN {relay_url, session_token} over tunnel → Device
  Device → POST agent /vnc/start (Xvfb+x11vnc+camoufox) → VNC_OPENED {rfb_password}
  Device → dials a SECOND socket out: wss://gateway/session/{id} (binary RFB)
  Browser noVNC → wss://gateway/ws/vnc/{id} → Gateway pairs the two sockets → live control

Save a login from the session:
  Customer → POST /vnc/{id}/save-login {label}
  Device → GET agent /browser/state?origin → CRED_CAPTURE → Gateway encrypts + stores

Close: job terminal / idle / max-TTL / user → VNC_CLOSE; device POST agent /vnc/stop.
```

- The VNC RFB stream uses a **separate binary socket** (the device dials out per session); it never touches the JSON control tunnel and needs no inbound device port.
- Login cookies are **encrypted at rest** in the gateway, decrypted only in memory at dispatch, and **never persisted** on the device or agent (wiped with `/work/profile`).

## 4. Data Flow & Source of Truth

- **Cloud PostgreSQL** is authoritative for users, devices, the agent pool (with allocation status), jobs (canonical status), files (metadata), and the **skill vault** (catalog, versions, desired `device_skills`/`agent_skills`).
- **Local SQLite** is authoritative for *device-local execution detail* (container ids, pool state, workspace paths, local queue, cached skill packages) and is a cache of jobs currently in-flight.
- Reconciliation: on tunnel (re)connect, device sends a `STATE_SYNC` snapshot of in-flight jobs/agents (pool state); gateway reconciles canonical status (e.g., mark orphaned RUNNING jobs as FAILED, release their agents back to the pool, if the device reports them unknown). Skill desired-state is reconciled via `SKILL_SYNC`.

## 5. State Machines

### Job

```
PENDING ──► QUEUED ──► DISPATCHED ──► RUNNING ──► SUCCEEDED
   │           │            │            │     └─► FAILED
   └───────────┴────────────┴────────────┴────────► CANCELLED
```

- `PENDING`: accepted at gateway, allocator attempting immediate allocation.
- `QUEUED`: no idle agent in pool — waiting at gateway allocator. Ordered by user tier (enterprise > pro > free) then FIFO. Subject to `IAGENT_QUEUE_TTL`.
- `DISPATCHED`: agent allocated, job frame routed to device over tunnel.
- `RUNNING`: agent confirmed start.
- Terminal: `SUCCEEDED`, `FAILED`, `CANCELLED`. Terminal triggers file cleanup and agent release (wake-up allocator → dequeue next job).

### Device

```
ENROLLED → ONLINE ⇄ OFFLINE   (ONLINE requires live tunnel + heartbeats)
```

### Agent (pooled)

```
CREATING → IDLE ⇄ BUSY     (IDLE = available for allocation; BUSY = assigned to a customer job)
IDLE → UNHEALTHY → (restart) → IDLE
UNHEALTHY → (max retries exceeded) → FAILED → REMOVED
BUSY → (job done) → IDLE   (released back to pool)
BUSY → UNHEALTHY → (job FAILED) → IDLE  (agent recycled after failure)
```

### Device-wide skill (`device_skills`)

```
INSTALLING → INSTALLED ⇄ DISABLED
INSTALLED → UPDATING → INSTALLED
(any) → DELETING → (removed)
INSTALLING / UPDATING → ERROR → (retry via SKILL_RETRY) → INSTALLED
```

- A device-level `ERROR` status means one or more agents failed to install. The admin can inspect per-agent failures and trigger `SKILL_RETRY` for specific agents.
- Per-agent status tracked in `agent_skills`: `installed`, `error {message}`, `disabled`. Absent agents at device-level `INSTALLED` are assumed `installed`.

### VNC session (`vnc_sessions`)

```
PENDING ──(VNC_OPENED ready)──► READY ──(both sockets paired)──► ACTIVE ──► CLOSED
   │                              │                               │
   └──────────────────────────────┴───────────────────────────────┴──► ERROR
```

- `PENDING`: session row created, `VNC_OPEN` routed to device, awaiting `VNC_OPENED`.
- `READY`: agent VNC stack up, device session socket dialed; awaiting the browser noVNC socket.
- `ACTIVE`: both sockets paired; raw RFB relayed live.
- Terminal `CLOSED`: job terminal, user close, idle (`IAGENT_VNC_IDLE_TTL`) or max (`IAGENT_VNC_MAX_TTL`). `ERROR`: agent/relay failure. Always tears down the agent VNC stack.

## 6. Reliability & Recovery

- **Tunnel loss**: device retries with exponential backoff + jitter; jobs continue locally; results buffered in SQLite and flushed on reconnect.
- **Gateway restart**: stateless except DB; devices reconnect automatically; in-flight job status restored from DB + device `STATE_SYNC`.
- **Device restart**: on boot, reconcile Docker (adopt or recreate containers per SQLite records), resume health checks, re-establish tunnel.
- **Agent crash**: health check fails → device restarts container up to `max_restarts`; affected job → `FAILED` with reason.
- **At-least-once frames**: every tunnel frame carries a `msg_id`; receivers ACK; senders retry unacked frames; handlers are idempotent by `(job_id, event_seq)`.

## 7. Concurrency Model

- **Gateway**: one goroutine per device tunnel (read pump) + per-connection write pump with an outbound queue; web WS subscriptions fanned out via an in-memory pub/sub keyed by `user_id`/`job_id`.
- **Device**: asyncio event loop; one task per agent dispatch; agents are single-job — the allocator never dispatches two jobs to the same agent while it is `BUSY`.
- **Agent**: single active job; returns `409 Busy` if a second job is dispatched (safety net).

## 8. Security Posture (detail in `08-auth-security.md`)

- Gateway is the only public service; TLS everywhere (WSS/HTTPS).
- Devices authenticate with rotating `device_token`; users with JWT.
- Strict tenant scoping on every route and every tunnel frame.
- Agent containers run with resource limits, no host network, dropped capabilities, and ephemeral per-job data.
