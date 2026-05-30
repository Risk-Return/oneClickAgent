# 04 — Agent Container (Generic Agent + HTTP API)

Each agent is one Docker container running a Python runtime that exposes a **fixed HTTP API**. The spec defines the *contract and lifecycle*; the concrete LLM/agent framework inside is swappable and **not bound** here.

## 1. Goals & Non-Goals

**Goals**
- Stable, minimal HTTP API the device can rely on regardless of the agent implementation.
- Execute exactly one job at a time, emit progress, return a progress-level result.
- Manage its own skills. Wipe all user data after a job ends.

**Non-Goals**
- Talking to the gateway directly (only the device does).
- Persisting user data across jobs.
- Exposing terminals/raw internals to users.

## 2. Image Layout

```
agent/
├── Dockerfile
├── pyproject.toml
└── iagent_agent/
    ├── server.py          # FastAPI app, routes
    ├── runtime/           # job executor, progress emitter, cancellation
    ├── skills/            # skill loader from manifests
    ├── workspace.py       # /work dir mgmt + cleanup
    └── adapter/           # pluggable "brain": maps command → actions (impl-specific)
```

`adapter/` is the only part that changes per agent type; it implements:

```python
class AgentBrain(Protocol):
    async def run(self, ctx: JobContext, emit: ProgressEmitter) -> JobResult: ...
    async def cancel(self, job_id: str) -> None: ...
```

`JobContext` = `{ job_id, command, params, inputs_dir, output_dir, skill? }` (at most one selected skill).

## 3. HTTP API

Bound to `0.0.0.0:<container_port>` (mapped to a fixed host port by the device). Only the device calls it; not internet-exposed.

| Method | Path | Body / Result |
|--------|------|---------------|
| GET | `/healthz` | `200 {status:"ok", busy:bool}` (liveness/readiness) |
| GET | `/status` | `{ current_job, usage:{cpu_pct, mem_mb, disk_mb}, skills:[...] }` |
| POST | `/jobs` | `{job_id, command, params, inputs_dir, output_dir, skill_id?, callback_url}` → `202 {job_id}` or `409 {code:"BUSY"}`. **At most one** `skill_id` |
| GET | `/jobs/{id}` | `{ job_id, status, percent, message, result?, error? }` |
| POST | `/jobs/{id}/cancel` | `{reason?}` → `202`; transitions to CANCELLED |
| GET | `/skills` | `[{skill_id, name, version, status}]` (`status` ∈ enabled/disabled) |
| POST | `/skills` | `{skill_id, name, version, manifest, artifact_path}` install or update (idempotent) |
| POST | `/skills/{id}/disable` | disable without removing (skill kept, not loaded) |
| POST | `/skills/{id}/enable` | re-enable a disabled skill |
| DELETE | `/skills/{id}` | remove the skill entirely |

### Job execution flow

```
POST /jobs (busy? → 409)
  status=RUNNING; spawn executor task
  executor calls brain.run(ctx, emit)
  emit(percent, message) → push to device callback (below) + update in-memory state
  on done → status SUCCEEDED + result; on raise → FAILED + error
  finally → wipe workspace (inputs/scratch/output)
```

### Progress callback (preferred over polling)

Agent posts events to the device:

```
POST {callback_url}/jobs/{job_id}/events
  { event_seq, status, percent, message, ts }
  terminal: { event_seq, status:"SUCCEEDED"|"FAILED", result?, error?, finished_at }
```

If the callback fails, the device polls `GET /jobs/{id}`. `event_seq` is monotonic per job for idempotent ordering.

> **Progress-level only:** `message` and `result` must be user-presentable summaries. No raw logs, no chain-of-thought, no terminal/stdout streaming through this contract.

## 4. Workspace & Data Hygiene

```
/work/inputs/    # read-only, mounted by device (staged user files)
/work/scratch/   # agent temp
/work/output/    # produced artifacts (referenced in result, then wiped)
```

- On every job terminal state (success/fail/cancel), the runtime deletes the contents of `inputs`, `scratch`, `output`.
- No persistence between jobs; container restart starts clean.
- Large output artifacts: returned via device→gateway file-pull flow (reserved, see `05-tunnel-protocol §5`); otherwise summarized in `result`.

## 5. Skills

- A skill is a declarative `manifest` (capability name, entrypoint ref, params schema, resource hints) plus an optional packaged artifact, installed into the agent by the device.
- The device pushes skills it received from the **cloud skill vault**; the agent never talks to the vault or gateway directly.
- `skills/` loads installed manifests/artifacts and exposes **enabled** skills to the `AgentBrain`.
- Lifecycle the agent must support: **install/update** (idempotent, by `skill_id`+`version`), **disable** (kept but not loaded/usable), **enable** (reload a disabled skill), **delete** (fully removed).
- Only `enabled` skills are usable in a job; a job carries **at most one** `skill_id`, which must be a currently `enabled` skill (else the job fails with `SKILL_NOT_ENABLED`).
- Skill state is reported via `/status` and `/skills`; device relays to gateway (`SKILL_STATE`).

## 6. Resource Self-Limits

- Container limits applied by the device (cpu/mem/disk). The agent also self-reports `usage` in `/status`.
- Default limits: `cpu=2, mem=4GB, disk=10GB`.
- Agent should fail fast with a clear `error` if a job exceeds workspace disk quota.

## 7. Configuration (env)

| Var | Default | Purpose |
|-----|---------|---------|
| `IAGENT_AGENT_PORT` | `8090` | container HTTP port |
| `IAGENT_AGENT_ID` | injected | identity (label-matched) |
| `IAGENT_WORK_DIR` | `/work` | workspace root |
| `IAGENT_BRAIN` | impl-specific | selects the AgentBrain adapter |
| (LLM creds) | impl-specific | provided as secrets at create time, never logged |

## 8. Dockerfile Requirements

- Slim Python base (`python:3.11-slim`), non-root user, `HEALTHCHECK` hitting `/healthz`.
- `read_only` root fs compatible; writable tmpfs + `/work` volume.
- No SSH, no extra daemons. Single process (uvicorn) + executor tasks.
- Multi-arch build (`linux/amd64`, `linux/arm64`) so it runs under Docker Desktop on both Intel and Apple Silicon.

## 9. Security

- Never internet-facing; only the device on loopback/bridge network.
- Drops capabilities, non-root, no host network, pids limit.
- Secrets passed via env/secret mount at create time; excluded from `/status` and logs.
- Detail in `08-auth-security.md`.

## 10. Testing

- Contract tests against the HTTP API with a **mock brain** (deterministic progress + result).
- Verify workspace wipe on success/fail/cancel.
- Verify `409 BUSY` when a second job is dispatched.
- Verify callback + polling fallback behavior.
