# 03 — Local Device (Python)

A single installation on a private machine that manages multiple agents (Docker containers), holds the reverse tunnel to the gateway, stages files, and persists local state in SQLite. No public IP, no inbound ports.

> **Operated by an admin, not customers.** A device is admin-managed infrastructure enrolled with an admin-issued code. It maintains a **pool of agent containers** that are temporarily allocated to customer jobs. When a job arrives, the device dispatches it to the allocated agent; when the job completes, the agent returns to the idle pool. The device itself is agnostic to customer identity — it just runs the agents/jobs it is told to. Fleet-wide skill installs from the admin apply to **every** agent in the pool.

## 1. Goals & Non-Goals

**Goals**
- Dial-out reverse WebSocket tunnel with robust reconnect.
- Full Docker lifecycle for agent **pool**: create (pre-provision idle agents), start, stop, remove, health, recover, recycle after job completion.
- Receive jobs/files, dispatch to agents, relay progress/results, clean up data.
- Survive restarts: reconcile Docker + resume from SQLite.

**Non-Goals**
- Authenticating end users (gateway does that).
- Exposing any public endpoint.
- Long-term storage of user files.

## 2. Module Layout

```
device/
├── pyproject.toml
└── iagent_device/
    ├── __main__.py            # entrypoint / CLI (enroll, run, status)
    ├── config.py              # env + config file, OS-aware paths
    ├── tunnel/                # ws client, framing, ack, reconnect, outbox flush
    ├── jobs/                  # dispatcher, queue, progress relay, deallocation
    ├── docker/                # docker-py wrapper: pool lifecycle, health, recovery, reaper
    ├── files/                 # staging, mount, cleanup
    ├── agentclient/           # HTTP client to agent containers (jobs, skills, vnc, browser-state)
    ├── skills/                # device-wide skill cache, dispatch receive, apply to agents
    ├── vncbridge/             # on VNC_OPEN: dial session WS out, bridge TCP↔WS to container RFB port
    ├── creds/                 # CRED_PUSH inject / CRED_CAPTURE export relay (no local persistence)
    ├── monitor/               # resource + status sampling
    └── store/                 # SQLite repositories (sqlite3/aiosqlite)
```

## 3. Key Libraries

- `websockets` (asyncio WS client) or `aiohttp`.
- `docker` (docker-py) for container control.
- `httpx` (async) for agent HTTP API.
- `aiosqlite` + WAL for local store.
- `pydantic` for config + frame models.
- `keyring` for `device_token` storage where available (fallback to file with `0600`).

> **Cross-platform:** use `pathlib.Path`, `platformdirs` for data dir, never hard-code `/`. Works on Docker Desktop (Windows/macOS) and Docker Engine (Linux). Bind-mount paths converted appropriately per OS.

## 4. Lifecycle

```
iagent-device enroll --gateway https://… --code <enrollment_code>
   → POST /devices/enroll → store device_id + device_token in SQLite/keystore

iagent-device run
    1. load config + reconcile Docker — ensure pool of N agent containers
       (create missing idle agents, remove surplus, recycle left-over BUSY from crash)
    2. open tunnel → send HELLO (pool_size, capabilities, resources)
    3. start: heartbeat task, job dispatcher + allocator, monitor, outbox flusher,
       pool reaper (recycle finished-agents)
    4. serve frames until shutdown; on disconnect → reconnect loop
```

Graceful shutdown: stop accepting new jobs, finish/flush in-flight results to outbox, close tunnel with `1000`.

## 5. Tunnel Client

- Implements device side of `05-tunnel-protocol.md`.
- Reconnect with exponential backoff + jitter; on reconnect send `HELLO` → `STATE_SYNC` → flush `outbox`.
- All outbound progress/results written to `outbox` first (durable), removed on cloud ACK → guarantees at-least-once delivery across restarts.
- Inbound dispatch table maps frame `type` → handler (job dispatch/cancel, agent create/action, skill sync, file push, **VNC open/close**, **credential push**).
- Interactive VNC uses a **second, on-demand socket** dialed out per session (`05-tunnel-protocol §9`); it is separate from this control tunnel and carries raw binary RFB.

## 6. Job Dispatcher

```
on JOB_DISPATCH:
  persist local job (QUEUED); send JOB_ACCEPTED
  ensure referenced files staged (await FILE_PUSH_* completion)
  if credential_ids present: await CRED_PUSH per credential → POST agent /browser/state {storage_state}
                            (verify sha256) → CRED_PUSH_ACK   # injected into /work/profile, never persisted
  POST agent /jobs {job_id, command, params, inputs_dir, skill_id?}   # at most one skill
  on accept: status DISPATCHED→RUNNING; relay JOB_PROGRESS as events arrive
  on agent callback result: write result to outbox as JOB_RESULT; mark terminal
  always: trigger file cleanup for the job
  signal pool reaper: this agent is done → return to IDLE
on JOB_CANCEL:
  POST agent /jobs/{id}/cancel; on confirm emit terminal CANCELLED
  signal pool reaper: release agent back to IDLE
```

- Agents are single-job; the gateway allocator never sends two jobs to the same `BUSY` agent.
- Progress receipt: prefer agent → device callback `POST /jobs/{id}/events`; fallback to polling `GET /jobs/{id}`.

## 7. Docker Manager (Pool Management)

Maintains a pool of agent containers. The pool size is configured per device (default e.g. 4).

### Pool operations

| Action | Detail |
|--------|--------|
| pre-pull image | on enroll/run, `docker pull` the agent image **before** creating containers; the image is large (Ubuntu + opencode + camoufox + node/python/go/rust/java + VNC), so pre-pull avoids cold-start latency (`04-agent-container §2`, `10-deployment §4`) |
| create (scale up) | create N idle containers with limits, fixed host port→container HTTP port, labels `iagent.agent_id`, `iagent.pool=true`, mounts for workspace. The container's **RFB port stays loopback-bound and is NOT published** (`-p`); the device reaches it via the container bridge IP on loopback |
| start / stop / restart / remove | standard lifecycle; update SQLite + emit AGENT_STATUS |
| health check | periodic `GET /healthz` per agent; container HEALTHCHECK as backup |
| recovery | on failure restart up to `max_restarts` (default 3) with backoff; exceed → agent FAILED, remove from pool, create replacement |
| reconcile (boot) | list containers by `iagent.pool=true` label; adopt matching SQLite records, create missing up to desired pool size, remove surplus, recycle any still-BUSY (orphaned after crash) → IDLE |
| reaper | after job completion: clear workspace, emit AGENT_STATUS=IDLE, agent ready for next allocation |

### Resource limits

Default `cpu=2, mem=4GB, disk=10GB` per agent:
- CPU: `nano_cpus = 2_000_000_000`.
- Memory: `mem_limit="4g"`.
- Disk: enforce via image/storage-driver quota where supported; otherwise monitor workspace size and fail job on overflow (documented limitation per platform).

Security flags: `network` restricted (no host net), `cap_drop=["ALL"]` + minimal adds, `read_only` root fs with tmpfs + workspace volume, `pids_limit`, non-root user. See `08-auth-security.md`.

## 8. File Stager

```
FILE_PUSH_BEGIN → create job workspace dir (per-job, ephemeral) → open file
FILE_CHUNK → append (verify order via index) → ACK each chunk
FILE_PUSH_END → verify sha256 → FILE_ACK staged_device (or ERROR)
mount workspace inputs (read-only) into the agent container
on job terminal → delete workspace dir → FILE_PURGED
```

Workspace path: `<data_dir>/workspaces/<job_id>/{inputs,output}`. Always removed on terminal state and on startup reconciliation for completed jobs.

## 9. Skill Manager

Receives skill packages from the **cloud skill vault** and applies them to agents. Handles two scopes (see `05-tunnel-protocol §4.6`):

- **Device-wide (admin)** — `install` / `disable` / `update` / `delete` applied to **all** agents on the device.
- **Per-agent (user)** — `enable` / `disable` an installed skill on a single agent.

```
on SKILL_DISPATCH_BEGIN/CHUNK/END:
  receive chunked package → verify sha256 → cache at <data_dir>/skills/<skill_id>/<version>/
  SKILL_DISPATCH_ACK status=CACHED (or ERROR)
on SKILL_ACTION (scope=device, action=install|update):
  upsert device_skills (status=installing/updating)
  for each agent on device: POST agent /skills {skill_id, version, manifest, artifact_path}
  on all-ok → status=installed; else status=error + error_message
on SKILL_ACTION (scope=device, action=disable|delete):
  for each agent: POST /skills/{id}/disable  OR  DELETE /skills/{id}
  update device_skills status (disabled/removed)
on SKILL_ACTION (scope=agent, action=enable|disable):
  POST agent /skills/{id}/enable|disable for the one agent_id; update agent_skills
emit SKILL_STATE after each change; reconcile against SKILL_SYNC on (re)connect
```

Rules:
- New agents created later automatically receive all `installed` device-wide skills before going RUNNING.
- A user can only `enable` a skill that is `installed` (not `disabled`/`deleting`) on the device.
- Skill packages are cached locally and reused; cache is pruned when a skill is deleted device-wide.
- Operations are idempotent and resumable; partial failures are reported per agent and retried.

## 10. VNC Session Bridge

When a customer opens an interactive browser view, the gateway sends `VNC_OPEN` over the control tunnel; the device sets up a bridge so RFB bytes flow gateway↔container without any inbound port (`05-tunnel-protocol §9`).

```
on VNC_OPEN {session_id, agent_id, job_id, relay_url, session_token, ttl_s}:
  1. POST agent /vnc/start            → {rfb_port, rfb_password}   # agent brings up Xvfb+x11vnc+browser
  2. open TCP to 127.0.0.1:rfb_port   (container loopback RFB)
  3. dial OUT a session WS:  relay_url  (Authorization: Bearer <session_token>, subproto iagent.session.v1)
  4. send VNC_OPENED {session_id, status:"ready", rfb_password} on the control tunnel
  5. bridge bytes both ways:  session WS (binary) ⇄ TCP RFB   # device == websockify-equivalent
on VNC_CLOSE / job terminal / socket error:
  close both sockets; POST agent /vnc/stop; persist vnc_sessions.status=closed
```

- The bridge is pure byte-shoveling; the device does **not** parse RFB.
- One bridge task per `session_id`; multiple concurrent sessions allowed (one per active job that opens VNC).
- Backpressure follows the slower side; on overflow or `ttl` expiry the device tears the bridge down.
- `rfb_password` (from the agent) is forwarded to the gateway only via the trusted control tunnel.

## 11. Credential Handling (Login Cookies)

The device is a **pass-through** for login storage-state; it **never** persists cookies to SQLite or disk (`06-data-model §2`).

```
Inject (job start, gateway → device):
  on CRED_PUSH {job_id, credential_id, origin, storage_state, sha256}:
    verify sha256 → POST agent /browser/state {storage_state} → CRED_PUSH_ACK {status:INJECTED}

Capture (save login, device → gateway):
  on gateway capture request for an active session's origin:
    GET agent /browser/state?origin=<site> → storage_state
    CRED_CAPTURE {session_id, job_id, label, origin, storage_state, sha256}   # gateway encrypts + stores
```

- Storage-state is streamed straight between the gateway and the agent; the device holds it only transiently in memory.
- Never logged. The device has no decryption key and no knowledge of which customer owns a credential.

## 12. Monitor

- Samples per-agent `usage` (cpu%, mem, disk) via Docker stats; throttled (e.g., every 10s).
- Emits `AGENT_STATUS` on change or interval.
- Surfaces device-level resources + agent capabilities (incl. `vnc_enabled`) in `HELLO` and refreshes periodically.

## 13. Local Store (SQLite)

Schema in `06-data-model.md §2`. Single writer connection, WAL mode. `outbox` table is the durability backbone for tunnel delivery. `vnc_sessions` tracks bridge state only; **no credential bytes are stored**.

## 14. Configuration (env / file)

| Var | Default | Purpose |
|-----|---------|---------|
| `IAGENT_GATEWAY_URL` | — | gateway base (https) |
| `IAGENT_DEVICE_DATA_DIR` | platform data dir | SQLite + workspaces |
| `IAGENT_DOCKER_HOST` | platform default | docker socket/npipe |
| `IAGENT_MAX_RESTARTS` | `3` | agent restart cap |
| `IAGENT_HEARTBEAT_S` | `15` | tunnel heartbeat |
| `IAGENT_AGENT_IMAGE` | `iagent/agent:latest` | default agent image (large; pre-pulled on run) |
| `IAGENT_PREPULL_IMAGE` | `true` | `docker pull` the agent image on enroll/run before creating the pool |
| `IAGENT_PORT_RANGE` | `42000-42999` | host port allocation for agent **HTTP** ports (RFB ports are loopback-only, never published) |
| `IAGENT_SESSION_DIAL_TIMEOUT_S` | `15` | timeout dialing the gateway session socket on `VNC_OPEN` |

## 15. CLI

```
iagent-device enroll --gateway URL --code CODE
iagent-device run               # pre-pulls the agent image, then runs the pool + tunnel
iagent-device status            # local agents + tunnel state
iagent-device agents            # list local agents
iagent-device pull              # force a re-pull of the agent image
iagent-device logs --agent ID   # local diagnostic logs (NOT exposed to users via gateway)
```

## 16. Failure Handling

| Scenario | Behavior |
|----------|----------|
| Tunnel down | jobs continue; results buffered in outbox; reconnect loop |
| Agent unhealthy | restart up to cap; then fail active job + AGENT_STATUS=failed |
| Device reboot | reconcile Docker from SQLite; recreate/adopt; reconnect tunnel |
| Disk pressure | reject new jobs; emit warning status |
| Corrupt file transfer | FILE_ACK ERROR → gateway retransmits whole file |
| Image pull fails | retry with backoff; surface device status; pool stays empty until image present |
| VNC session socket drops | tear down bridge + `POST agent /vnc/stop`; gateway marks session closed |
| CRED_PUSH sha256 mismatch | `CRED_PUSH_ACK status=ERROR`; gateway re-pushes or fails the job |

## 17. Testing

- Unit: framing/ack, dispatcher state, file stager (sha256, chunk order), docker wrapper (mock client), VNC bridge byte-pump (loopback TCP ⇄ fake WS), credential inject/capture relay (no disk writes).
- Integration: real Docker with a stub agent image; simulate tunnel against a fake gateway WS server; open a VNC session end-to-end against a stub RFB server; `CRED_PUSH`/`CRED_CAPTURE` round-trip.
- Cross-platform CI: run suite on Windows + macOS runners (note: the bundled agent image targets Linux containers under Docker Desktop).
