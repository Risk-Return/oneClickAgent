# 03 — Local Device (Python)

A single installation on a private machine that manages multiple agents (Docker containers), holds the reverse tunnel to the gateway, stages files, and persists local state in SQLite. No public IP, no inbound ports.

> **Operated by an admin, not customers.** A device is admin-managed infrastructure enrolled with an admin-issued code. It may host agents belonging to **different customers** (placed by the gateway scheduler); the device itself is agnostic to customer identity and just runs the agents/jobs it is told to. Fleet-wide skill installs from the admin apply to **every** agent it hosts.

## 1. Goals & Non-Goals

**Goals**
- Dial-out reverse WebSocket tunnel with robust reconnect.
- Full Docker lifecycle for agents: create, start, stop, remove, health, recover.
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
    ├── jobs/                  # dispatcher, queue, progress relay
    ├── docker/                # docker-py wrapper: lifecycle, health, recovery
    ├── files/                 # staging, mount, cleanup
    ├── agentclient/           # HTTP client to agent containers
    ├── skills/                # device-wide skill cache, dispatch receive, apply to agents
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
   1. load config + reconcile Docker (adopt/recreate containers from SQLite)
   2. open tunnel → send HELLO (agents, capabilities, resources)
   3. start: heartbeat task, job dispatcher, monitor, outbox flusher
   4. serve frames until shutdown; on disconnect → reconnect loop
```

Graceful shutdown: stop accepting new jobs, finish/flush in-flight results to outbox, close tunnel with `1000`.

## 5. Tunnel Client

- Implements device side of `05-tunnel-protocol.md`.
- Reconnect with exponential backoff + jitter; on reconnect send `HELLO` → `STATE_SYNC` → flush `outbox`.
- All outbound progress/results written to `outbox` first (durable), removed on cloud ACK → guarantees at-least-once delivery across restarts.
- Inbound dispatch table maps frame `type` → handler (job dispatch/cancel, agent create/action, skill sync, file push).

## 6. Job Dispatcher

```
on JOB_DISPATCH:
  persist local job (QUEUED); send JOB_ACCEPTED
  ensure referenced files staged (await FILE_PUSH_* completion)
  POST agent /jobs {job_id, command, params, inputs_dir, skill_id?}   # at most one skill
  on accept: status DISPATCHED→RUNNING; relay JOB_PROGRESS as events arrive
  on agent callback result: write result to outbox as JOB_RESULT; mark terminal
  always: trigger file cleanup for the job
on JOB_CANCEL:
  POST agent /jobs/{id}/cancel; on confirm emit terminal CANCELLED
```

- One active job per agent (agents are single-job); extra dispatch → queue depth 1 or `JOB_REJECTED` if busy (policy configurable).
- Progress receipt: prefer agent → device callback `POST /jobs/{id}/events`; fallback to polling `GET /jobs/{id}`.

## 7. Docker Manager

Responsibilities (via docker-py):

| Action | Detail |
|--------|--------|
| create | pull image, create container with limits, fixed host port→container port, labels `iagent.agent_id`, mounts for workspace |
| start/stop/restart/remove | standard lifecycle; update SQLite + emit AGENT_STATUS |
| health check | periodic `GET /healthz`; container HEALTHCHECK as backup |
| recovery | on failure restart up to `max_restarts` (default 3) with backoff; exceed → status FAILED, fail active job |
| reconcile | on device boot, list containers by label; adopt matching SQLite records, recreate missing, remove orphans |

Resource limits (default `cpu=2, mem=4GB, disk=10GB`):
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

## 10. Monitor

- Samples per-agent `usage` (cpu%, mem, disk) via Docker stats; throttled (e.g., every 10s).
- Emits `AGENT_STATUS` on change or interval.
- Surfaces device-level resources in `HELLO` and refreshes periodically.

## 11. Local Store (SQLite)

Schema in `06-data-model.md §2`. Single writer connection, WAL mode. `outbox` table is the durability backbone for tunnel delivery.

## 12. Configuration (env / file)

| Var | Default | Purpose |
|-----|---------|---------|
| `IAGENT_GATEWAY_URL` | — | gateway base (https) |
| `IAGENT_DEVICE_DATA_DIR` | platform data dir | SQLite + workspaces |
| `IAGENT_DOCKER_HOST` | platform default | docker socket/npipe |
| `IAGENT_MAX_RESTARTS` | `3` | agent restart cap |
| `IAGENT_HEARTBEAT_S` | `15` | tunnel heartbeat |
| `IAGENT_AGENT_IMAGE` | `iagent/agent:latest` | default agent image |
| `IAGENT_PORT_RANGE` | `42000-42999` | host port allocation for agents |

## 13. CLI

```
iagent-device enroll --gateway URL --code CODE
iagent-device run
iagent-device status            # local agents + tunnel state
iagent-device agents            # list local agents
iagent-device logs --agent ID   # local diagnostic logs (NOT exposed to users via gateway)
```

## 14. Failure Handling

| Scenario | Behavior |
|----------|----------|
| Tunnel down | jobs continue; results buffered in outbox; reconnect loop |
| Agent unhealthy | restart up to cap; then fail active job + AGENT_STATUS=failed |
| Device reboot | reconcile Docker from SQLite; recreate/adopt; reconnect tunnel |
| Disk pressure | reject new jobs; emit warning status |
| Corrupt file transfer | FILE_ACK ERROR → gateway retransmits whole file |

## 15. Testing

- Unit: framing/ack, dispatcher state, file stager (sha256, chunk order), docker wrapper (mock client).
- Integration: real Docker with a stub agent image; simulate tunnel against a fake gateway WS server.
- Cross-platform CI: run suite on Windows + macOS runners.
