# Audit 03 — Local Device Implementation vs Spec

**Date:** 2026-06-02
**Scope:** Line-by-line audit of all 18 Python source files in `device/iagent_device/` against `docs/spec/03-local-device.md` and cross-referenced protocol spec `docs/spec/05-tunnel-protocol.md`. Identifies spec-aligned implementations, partial/deviant implementations, and outright gaps.

---

## Summary

| Category | Count |
|----------|-------|
| Spec-Aligned (Passing) | 19 |
| Partial / Minor Deviation | 7 |
| Gaps / Missing | 33 |

Overall: solid scaffolding across all 17 packages, correct frame protocol and outbox pattern. However, the implementation is approximately **~40% complete** — job dispatch uses fake simulated progress, Docker pool lifecycle is rudimentary, skill manager has table-update omissions and format mismatches, monitor uses host-level metrics instead of per-container stats, and zero tests exist.

---

## 1. Spec-Aligned (Passing)

| # | Requirement | Spec § | Evidence |
|---|-------------|--------|----------|
| 1 | Module layout — all 17 packages exist | §2 | 17 `.py` files matching the spec tree |
| 2 | Dial-out WSS tunnel with `Authorization: Bearer` auth header | §5 | `tunnel/client.py:57-59` |
| 3 | Tunnel subprotocol `oneClickAgent.tunnel.v1` | `05-tunnel §1` | `tunnel/client.py:60` |
| 4 | Frame codec — `v`, `type`, `msg_id`, `ts`, `ack_id`, `payload` envelope | `05-tunnel §2` | `tunnel/codec.py:71-82` |
| 5 | `FRAME_MAX_SIZE = 1 MiB` | `05-tunnel §2` | `tunnel/codec.py:13` |
| 6 | `FrameType` StrEnum with all protocol message types | `05-tunnel §4` | `tunnel/codec.py:16-64` |
| 7 | HELLO and STATE_SYNC sent on connect | §5 | `tunnel/client.py:70-76` |
| 8 | Heartbeat PING every `heartbeat_s` seconds | §5 | `tunnel/client.py:117-122` |
| 9 | ACK handling — every non-ACK frame gets ACKed | `05-tunnel §2` | `tunnel/client.py:91-92` |
| 10 | Durable outbox pattern — write to SQLite before send, remove on cloud ACK | §5 | `tunnel/outbox.py:21-39` |
| 11 | Config — all env vars from §14 parsed | §14 | `config.py:59-76` |
| 12 | Agent pool — `ensure_pool()` creates idle agents, removes surplus | §7 | `docker/manager.py:39-58` |
| 13 | Docker labels `iagent.agent_id`, `iagent.pool=true` | §7 | `docker/manager.py:70-72` |
| 14 | Container resource limits — `mem_limit="4g"`, `nano_cpus=2_000_000_000` | §7 | `docker/manager.py:74-75` |
| 15 | Security flags — `cap_drop=["ALL"]`, `read_only=True` | §7 | `docker/manager.py:77-78` |
| 16 | Reconcile on boot — recycle orphaned BUSY → IDLE | §7 | `docker/reconcile.py:19-22` |
| 17 | Agent HTTP client — full API surface (healthz, jobs, skills, VNC, browser/state) | §6 | `agentclient/client.py` all endpoints |
| 18 | File stager — BEGIN/CHUNK/END with sha256 verification | §8 | `files/stager.py:24-78` |
| 19 | Credential pass-through — never persists to SQLite or disk, in-memory only | §11 | `creds/relay.py:31-66` |
| 20 | VNC bridge — dial session WS, connect TCP to RFB, bridge bytes both ways | §10 | `vncbridge/bridge.py:68-96` |
| 21 | SQLite — WAL mode, FK enforcement, `busy_timeout=5000`, 8 tables | §13 | `store/connection.py:10-14`, `store/connection.py:34-129` |
| 22 | Schema migration with `schema_version` tracking | §13 | `store/connection.py:18-31` |
| 23 | `pyproject.toml` — all required dependencies present | §3 | `pyproject.toml:6-16` |
| 24 | Cross-platform — `platformdirs`, `pathlib.Path`, OS-aware Docker socket | §3 | `config.py:14-21` |

---

## 2. Partial / Minor Deviation

| # | Issue | Location | Spec Reference |
|---|-------|----------|----------------|
| P1 | **HELLO payload incomplete** — sends `{device_id, agent_count:0, agents:[]}` but spec requires `platform, resources:{cpu, mem_mb, disk_mb}, agent_version, agents:[{agent_id, status, port, tags}]` | `tunnel/client.py:71-75` | `05-tunnel §4.1` |
| P2 | **Reconnect delay uses `uniform(1, 15)` flat random** — spec §7 requires exponential backoff `min(30s, 1s*2^attempt) + jitter ±20%` | `tunnel/client.py:52` | `05-tunnel §7` |
| P3 | **`Outbox.send_fn` signature mismatch** — `Outbox.enqueue_and_send` calls `send_fn(msg_id, frame_type, payload)`, but `TunnelClient._send` expects `_send(frame_type, payload, ack_id)`. This will crash at runtime when the tunnel is connected. | `tunnel/outbox.py:24` called with 3 args; `tunnel/client.py:124` accepts 3 args but `msg_id` is passed as `frame_type` (str enum expected, gets UUID string); `__main__.py:118` passes `tunnel._send` | §5 |
| P4 | **`send_with_ack` re-parses its own JSON** — calls `encode_frame()` (which returns a JSON string), then `json.loads()` the string back to extract `msg_id`. Should pass the dict directly. | `tunnel/client.py:130-139` | N/A (code smell) |
| P5 | **Outbox never flushed on reconnect** — `outbox` ref is stored on the client but `.flush()` is never called after successful reconnection | `tunnel/client.py:46-54` | §5 |
| P6 | **No `HELLO_ACK` handler** — spec says gateway responds with `HELLO_ACK` after `HELLO`, but no handler is registered for it | `tunnel/client.py:94-101` | `05-tunnel §1`, `05-tunnel §4.1` |
| P7 | **VNC session WS auth header mismatch** — code uses `X-Session-Token` header, but `05-tunnel §9` step 5 says `Authorization: Bearer <session_token>` | `vncbridge/bridge.py:72` | `05-tunnel §9` |

---

## 3. Gaps / Missing

### 3.1 CLI (`__main__.py`)

| # | Gap | Spec § |
|---|-----|--------|
| G1 | **Missing `pull` command** — `iagent-device pull` to force re-pull the agent image | §15 |
| G2 | **`status` command is a stub** — prints `"Status: see logs..."`; should show real tunnel state, Docker pool status | §15 |
| G3 | **`agents` command is minimal** — lists from SQLite but doesn't show container health, uptime | §15 |
| G4 | **`logs` command is a stub** — prints `"Logs: check device log output"` | §15 |

### 3.2 Lifecycle / Tunnel (`tunnel/client.py`, `__main__.py`)

| # | Gap | Spec § |
|---|-----|--------|
| G5 | **No image pre-pull** — `IAGENT_PREPULL_IMAGE` is parsed in config but `docker pull` is never called on enroll or run | §7 |
| G6 | **No graceful shutdown** — spec says "stop accepting new jobs, finish/flush in-flight results to outbox, close tunnel with code 1000" | §4 |
| G7 | **No reconnect retry cap or max attempts** — infinite reconnect loop | §16 |
| G8 | **No `AGENT_CREATE`, `AGENT_ACTION`, `AGENT_STATUS_REQ` handlers** — these frame types are in the codec but no handlers registered in the dispatch table | `05-tunnel §4.4` |
| G9 | **No `JOB_QUERY` handler** | `05-tunnel §4.2` |
| G10 | **No `SKILL_SYNC` handler** — spec says to reconcile full skill state on (re)connect | `05-tunnel §4.6` |
| G11 | **No `ERROR` frame handler** | `05-tunnel §4.1` |
| G12 | **No disk pressure detection** — spec says "reject new jobs, emit warning status" | §16 |

### 3.3 Docker Manager (`docker/manager.py`, `docker/reconcile.py`)

| # | Gap | Spec § |
|---|-----|--------|
| G13 | **No restart-on-failure flow** — `health_check()` increments restarts counter and updates status, but does not actually restart the Docker container or create a replacement when `max_restarts` is exceeded | §7 |
| G14 | **No pool reaper** — spec requires clearing workspace + emitting `AGENT_STATUS=IDLE` after job completion; `release()` sets DB status but no workspace cleanup | §7 |
| G15 | **No workspace/inputs volume mount** — containers created without bind-mount for workspace directories | §7, §8 |
| G16 | **No tmpfs mount, no `pids_limit`, no non-root user** — security hardening incomplete | §7, `08-auth-security` |
| G17 | **Container `remove=True`** — spec says agents are reusable pool members; auto-remove contradicts pool lifecycle and would prevent health-checking a stopped container | §7 |
| G18 | **No Docker container adoption on reconcile** — spec says "list containers by `iagent.pool=true` label, adopt matching SQLite records"; `reconcile()` only calls `ensure_pool()` | §7 |
| G19 | **RFB port isolation unclear** — spec says RFB port is loopback-bound inside container and NOT published; `_create_container` only publishes port 8090 (HTTP), but there is no explicit RFB port isolation configuration or doc comment | §7 |
| G20 | **No per-agent disk quota enforcement** — spec says "enforce via image/storage-driver quota where supported; otherwise monitor workspace size and fail job on overflow" | §7 |

### 3.4 Job Dispatcher (`jobs/dispatcher.py`)

| # | Gap | Spec § |
|---|-----|--------|
| G21 | **Fake/simulated progress** — uses hardcoded `asyncio.sleep(1)` + hardcoded percent values (0%, 50%), not actual agent progress events | §6 |
| G22 | **No file staging wait** — spec requires "ensure referenced files staged (await FILE_PUSH_* completion)" before dispatching to agent | §6 |
| G23 | **No credential injection before dispatch** — spec requires "if credential_ids present: await CRED_PUSH per credential → POST agent /browser/state" before the job starts | §6 |
| G24 | **No real progress relay from agent** — spec says prefer agent callback `POST /jobs/{id}/events`; fallback to polling `GET /jobs/{id}`. Neither is implemented. | §6 |
| G25 | **No file cleanup on job terminal** — `FileStager.cleanup()` is defined but never called from the dispatcher's `finally` block | §6, §8 |
| G26 | **No `FILE_PURGED` frame** — spec §8 says "on job terminal → delete workspace dir → FILE_PURGED" | §8 |

### 3.5 Skill Manager (`skills/manager.py`)

| # | Gap | Spec § |
|---|-----|--------|
| G27 | **`SKILL_DISPATCH_ACK` never sent** — spec requires `SKILL_DISPATCH_ACK status=CACHED` on successful dispatch; code sends `SKILL_STATE` instead, which is a different message type with different semantics | `05-tunnel §4.6` |
| G28 | **No `device_skills` table updates** — `SkillRepo.upsert_device_skill()` is defined but never called during install/update/disable/delete actions | §9 |
| G29 | **No `agent_skills` table updates** — per-agent skill state is never tracked in SQLite | §9 |
| G30 | **`SKILL_STATE` format wrong** — spec says single frame with `{device_skills:[{skill_id, version, status}], agent_skills:[{agent_id, skill_id, status}]}` but code sends one `SKILL_STATE` per individual skill with `scope` field | `05-tunnel §4.6` |
| G31 | **New agents don't inherit device-wide skills** — spec says "new agents created later automatically receive all installed device-wide skills before going RUNNING" | §9 |
| G32 | **No idempotent retry on partial failure** — spec says "partial failures are reported per agent and retried" | §9 |
| G33 | **`install_skill()` called with empty strings** — when installing/updating, `client.install_skill(skill_id, "", "", "")` passes empty version, manifest, and artifact_path | `skills/manager.py:117` |
| G34 | **No skill cache pruning** — spec says "cache is pruned when a skill is deleted device-wide" | §9 |

### 3.6 Monitor (`monitor/monitor.py`)

| # | Gap | Spec § |
|---|-----|--------|
| G35 | **Uses host-level psutil, not per-container metrics** — `psutil.cpu_percent()` and `psutil.virtual_memory()` report host metrics, not the individual agent container metrics. Spec requires "Samples per-agent usage (cpu%, mem, disk) via Docker stats" | §12 |
| G36 | **HELLO doesn't include device resources or capabilities** — spec says "surfaces device-level resources + agent capabilities (incl. vnc_enabled) in HELLO" | §12 |
| G37 | **AGENT_STATUS payload doesn't include `health`, `restarts`, or `usage` structure** — spec §4.5 requires `{agent_id, status, health, restarts, usage:{cpu_pct, mem_mb, disk_mb}, ts}` but code sends flat `cpu_percent` and `memory_mb` | `05-tunnel §4.5` |

### 3.7 Credential Relay (`creds/relay.py`)

| # | Gap | Spec § |
|---|-----|--------|
| G38 | **`CRED_PUSH` payload field mismatch** — code expects `data` field (base64); spec says `storage_state` field. Also expects `origin` which spec doesn't list in CRED_PUSH payload | `05-tunnel §4.9` |
| G39 | **`CRED_PUSH_ACK` status values wrong** — spec says `status:"INJECTED"` but code sends `status:"ok"` | `05-tunnel §4.9` |

### 3.8 VNC Bridge (`vncbridge/bridge.py`)

| # | Gap | Spec § |
|---|-----|--------|
| G40 | **No `ttl_s` handling** — spec §10 says "TTL expires the session" but the timeout field from payload is ignored | §10 |
| G41 | **No backpressure handling** — spec says "Backpressure follows the slower side; on overflow or ttl expiry the device tears the bridge down" | §10 |
| G42 | **No `VNC_CLOSE` sent on bridge drop** — when the bridge task ends, the device should notify gateway via `VNC_CLOSE`, but only the local DB is updated | §10 |

### 3.9 Storage (`store/`)

| # | Gap | Spec § |
|---|-----|--------|
| G43 | **Outbox never pruned** — `OutboxRepo.delete_acked()` is defined in repositories but never called anywhere; outbox will grow unbounded | N/A |
| G44 | **`JobState` Pydantic model unused** — `jobs/models.py` defines `JobState` but all code uses raw dicts via `JobRepo` | N/A |

### 3.10 Testing

| # | Gap | Spec § |
|---|-----|--------|
| G45 | **Zero unit tests** — no test files exist in `device/` despite `pytest` and `pytest-asyncio` in dev dependencies | §17 |
| G46 | **Zero integration tests** — no Docker daemon tests, no tunnel simulation, no VNC round-trip tests | §17 |
| G47 | **No CI configuration** — spec calls for cross-platform CI (Windows + macOS + Linux runners) | §17 |

---

## 4. Bug Findings

| # | Bug | Severity | Location |
|---|-----|----------|----------|
| B1 | **Runtime crash on outbox send** — `Outbox.send_fn` is called with signature `(msg_id, frame_type, payload)` but `TunnelClient._send` expects `(frame_type, payload, ack_id)`. The outbox path will crash when the tunnel is active. | **High** | `tunnel/outbox.py:24`, `__main__.py:118` |
| B2 | **Monitor reports host CPU/memory as agent metrics** — every agent gets the same host-level `cpu_percent` and `memory_mb` values because `psutil` samples system-wide, not per-container. Gateway would see inflated/identical numbers for every agent. | **High** | `monitor/monitor.py:30-31` |
| B3 | **`HELLO` agent_count always 0** — hardcoded to `agent_count: 0` and `agents: []`, so the gateway never knows the actual pool state on connect. | **Medium** | `tunnel/client.py:73` |

---

## 5. Recommendations

### Critical (blocking for production)
1. **Fix outbox signature mismatch (B1)** — align `send_fn` interface between `Outbox` and `TunnelClient`
2. **Replace fake job progress (G21)** — implement real agent→device progress relay via POST `/jobs/{id}/events` callback or polling `GET /jobs/{id}`
3. **Fix monitor metrics (B3, G35)** — use `docker stats` API (available via docker-py) instead of `psutil` for per-container metrics

### High Priority
4. **Implement image pre-pull (G5)** — call `docker pull` on `enroll` and `run` before pool creation
5. **Implement restart/recovery flow (G13)** — `health_check()` must restart the container, not just update SQLite
6. **Implement pool reaper (G14)** — clear workspace + emit IDLE status after job completion
7. **Add missing handlers (G8-G11)** — `AGENT_CREATE`, `AGENT_ACTION`, `SKILL_SYNC`, `ERROR`
8. **Wire credential injection into dispatcher (G23)** — inject creds before dispatching job to agent

### Medium Priority
9. **Fix HELLO payload (P1, B3)** — include real agent pool state, device platform, resources, capabilities
10. **Add exponential backoff reconnect (P2)** — replace flat random with jittered exponential backoff
11. **Complete skill manager (G27-G34)** — send correct frame types, update tables, inherit skills to new agents
12. **Add container security hardening (G15-G16)** — tmpfs, pids_limit, non-root user, workspace mounts

### Low Priority
13. **Wire FileStager cleanup into dispatcher (G25-G26)** — call cleanup + send FILE_PURGED on job terminal
14. **Implement graceful shutdown (G6)** — drain jobs, flush outbox, close tunnel with code 1000
15. **Prune outbox (G43)** — periodically call `delete_acked()`
16. **Write tests (G45-G47)** — start with unit tests for codec, dispatcher state machine, file stager sha256

---

## 6. Dev Progress Doc Accuracy

The dev progress at `docs/dev/03-local-device.md` marks all 16 packages as **"Done"** and lists 6 known gaps. This audit finds that while packages are structurally complete, the **"Done"** assessment overstates readiness:

- Several packages have **stub/prototype-level** implementations (job dispatcher with fake progress, monitor with wrong metrics source)
- The 6 known gaps are accurate but incomplete — this audit found 33 gaps vs 6 documented
- The dev doc should be updated with the full gap list from this audit
