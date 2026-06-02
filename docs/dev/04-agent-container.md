# 04-agent-container — Dev Progress

| Field | Value |
|-------|-------|
| **Spec** | `docs/spec/04-agent-container.md` |
| **Status** | Implemented (audited + gaps fixed 2026-06-02) |
| **Last Updated** | 2026-06-02 |
| **Imports** | `python -c "import iagent_agent"` passes |
| **Audit** | `docs/audit/04-agent-container.md` (to be created) |

## Packages Implemented

| Package | Path | Status |
|---------|------|--------|
| Entry Point | `agent/iagent_agent/__main__.py` | Done |
| FastAPI Server | `agent/iagent_agent/server.py` | Done |
| AgentBrain Protocol | `agent/iagent_agent/adapter/protocol.py` | Done |
| Stub Brain | `agent/iagent_agent/adapter/brain_stub.py` | Done |
| Job Executor | `agent/iagent_agent/runtime/executor.py` | Done |
| Job Context & Records | `agent/iagent_agent/runtime/context.py` | Done |
| Workspace | `agent/iagent_agent/workspace.py` | Done |
| Skill Manager | `agent/iagent_agent/skills/loader.py` | Done |
| Browser Manager | `agent/iagent_agent/browser/manager.py` | Done |
| VNC Stack | `agent/iagent_agent/browser/manager.py` | Done |
| Dockerfile | `agent/Dockerfile` | Done |
| Deps Manifests | `agent/deps/` | Done |
| VNC Scripts | `agent/vnc/entrypoint.sh` | Done |
| Contract Tests | `agent/tests/test_server.py` | Done |

## HTTP API (15 routes, aligning with spec §3)

| Method | Path | Status |
|--------|------|--------|
| GET | `/healthz` | Done |
| GET | `/status` | Done |
| POST | `/jobs` | Done |
| GET | `/jobs/{id}` | Done |
| POST | `/jobs/{id}/cancel` | Done |
| GET | `/skills` | Done |
| POST | `/skills` | Done |
| POST | `/skills/{id}/disable` | Done |
| POST | `/skills/{id}/enable` | Done |
| DELETE | `/skills/{id}` | Done |
| GET | `/vnc` | Done |
| POST | `/vnc/start` | Done |
| POST | `/vnc/stop` | Done |
| POST | `/browser/state` | Done |
| GET | `/browser/state` | Done |

## Env Config (9 vars, aligning with spec §9)

- `IAGENT_AGENT_PORT` (8090), `IAGENT_AGENT_ID` (consumed, exposed in `/status`)
- `IAGENT_WORK_DIR` (/work), `IAGENT_BRAIN` (opencode, falls back to stub)
- `IAGENT_VNC_ENABLED` (true), `IAGENT_VNC_PORT` (5901)
- `IAGENT_VNC_DISPLAY` (:99), `IAGENT_BROWSER_CMD` (camoufox), `IAGENT_BROWSER_PROFILE` (/work/profile)

## Dockerfile (multi-stage, spec §10)

- **Base:** ubuntu:24.04
- **Stages:** base → node → python → golang → rust → java → cache → tools → final
- **Bundled:** opencode (npm global), camoufox (npx), Node 20, Python 3.12, Go 1.22, Rust stable, Temurin 21 JDK, Xvfb + x11vnc
- **Dependency caches:** warmed from `agent/deps/` (npm ci, pip install, go mod download, cargo fetch, mvn dependency:go-offline)
- **Non-root user:** `app`; writable areas confined to `/work` (volume) + `/tmp` (tmpfs)
- **Security hardening:** runtime flags documented (`--cap-drop=ALL`, `--security-opt=no-new-privileges`, `--pids-limit=256`)
- **HEALTHCHECK:** `curl -sf http://localhost:8090/healthz`
- **Multi-arch:** linux/amd64 + linux/arm64
- **RFB port 5901:** loopback only, never host-published

## Tests (21 contract tests, all passing)

### Core API
- Healthz (idle + busy reflection)
- Status (usage, skills, current job, agent_id)
- Job submit (202) + BUSY (409) + polling + completion
- Job not found (404)
- Job cancel (202) + workspace wipe after cancel

### Skills
- Skills CRUD (install, list, disable, enable, delete)
- Skill update idempotent
- Skill not enabled for job (422)

### VNC + Browser
- VNC info
- VNC start without active job (409)
- VNC start with active job (skipped if Xvfb unavailable)
- Browser state inject + export round-trip
- Browser state export with origin filter

### Workspace + Credentials
- Workspace wipe after success (inputs, scratch, output, profile)
- Workspace wipe after cancel
- Disk quota exceeded detection
- Credentials injected flag wired through executor

### Callbacks
- Callback receives events (RUNNING → SUCCEEDED) with `ts` + `finished_at`

## Key Design Decisions

- **State via `app.state`** — executor, workspace, skills, browser, VNC all stored on FastAPI `request.app.state`, set during lifespan
- **Single-job concurrency** — `JobExecutor.busy` gate, 409 BUSY on second submit
- **Task lifecycle** — `asyncio.create_task` for background jobs; `cancel()` handles both started and never-started tasks (cleanup ensures workspace wipe + `_current = None`)
- **VNC + Browser teardown** — `_teardown()` in executor `finally` block kills browser, stops VNC stack, wipes workspace on all terminal states
- **Workspace hygiene** — `/work/{inputs,scratch,output,profile}` wiped on any terminal state (success/fail/cancel)
- **Disk quota** — `Workspace.check_quota()` walks workspace dirs and raises `RuntimeError` if usage exceeds limit (default 10 GB)
- **Inputs mount check** — `Workspace.inputs` warns if directory is missing (device mount may be absent)
- **Skill registry** — JSON file on disk under `/work/skills/`, per-skill artifact directories
- **Progress callback** — posts events to device `callback_url` via `CallbackClient`; polling fallback via `GET /jobs/{id}`
- **Stub brain** — 4-step deterministic progress (20/40/60/80%) for testing; configurable delay via `IAGENT_STUB_DELAY`
- **VNC + Browser** — subprocess-managed; VNC stack starts Xvfb + x11vnc on demand, per-session random RFB password, loopback-only binding
- **Credentials injection** — `POST /browser/state` calls `executor.mark_credentials_injected()`; `credentials_injected` flag propagates to `JobContext`
- **Origin filtering** — `GET /browser/state?origin=` filters cookies + localStorage by site origin before export
- **Agent identity** — `IAGENT_AGENT_ID` exposed in `/status` response, logged for traceability

## Known Gaps / TODOs

- [ ] Real opencode brain adapter (stub only; `open code` in `IAGENT_BRAIN` falls back to stub with warning)
- [ ] VNC integration test with actual RFB handshake (requires Xvfb+x11vnc installed; currently skipped)
- [ ] Docker image build + smoke test (opencode/camoufox/runtime presence checks)
- [ ] Per-arch camoufox availability verification
- [ ] Auth/security hardening flags (`--cap-drop=ALL`, pids limit) applied at `docker run` time by device manager (not in Dockerfile itself)
- [ ] Resource self-limits beyond disk quota (memory cap enforcement, not just reporting)
- [ ] Callback event retry + durable outbox (currently fire-and-forget with warning log)
