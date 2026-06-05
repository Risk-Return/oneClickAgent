# Local Device Deployment Progress

Deploying the IAgent local device (Python 3.14) + agent Docker containers (Ubuntu 24.04)
against a Go cloud gateway (local test instance) on Ubuntu 26.04.

## Completed Work

### Infrastructure Setup

| Task | Detail |
|------|--------|
| Docker installed | `docker.io` v28.x, user added to `docker` group |
| Go installed | `golang-go` for gateway binary build |
| PostgreSQL installed | v18 (Ubuntu 26.04 ships PG 18, not PG 15 as spec states) |
| Python venv created | `device/venv/` with all dependencies |
| Agent Docker image built | `iagent/agent:dev` (lightweight dev version, stub brain) |
| Gateway binary built | `gateway/bin/gateway` with Chinese Go proxy |
| Database created | `iagent` DB with all 4 migration sets applied |
| Admin user registered | `admin@iagent.local` with admin role |

### Bugs Fixed (10 fixes, 4 commits)

| # | File | Bug | Commit |
|---|------|-----|--------|
| 1 | `device/__main__.py:276` | Python 3.14 removed `asyncio.get_event_loop()` | `e96872c` |
| 2 | `agent/Dockerfile:74` | `$(...)` in ENV not supported by legacy Docker builder | `e96872c` |
| 3 | `gateway/tunnel_handler.go:63` | Duplicate `Sec-WebSocket-Protocol` header (gateway) | `e96872c` |
| 4 | `device/monitor.py:79` | HELLO `capabilities` dict → should be `[]string` | `e96872c` |
| 5 | `device/docker/manager.py:97` | Hardcoded `user="1000:1000"` doesn't match `app` UID 1001 | `e96872c` |
| 6 | `device/__main__.py:192` | AGENT_CREATE handler ignored `agent_id` from payload | `e96872c` |
| 7 | `device/tunnel/client.py:94` | `websockets` v16 internal ping caused keepalive timeout | `c2228d4` |
| 8 | `gateway/store/agents.go:18` | `Create()` overrode pre-set agent ID with new UUID | `a2d2c8c` |
| 9 | `gateway/pool/allocator.go:247` | Port=0 for all agents violated `(device_id, port)` unique constraint | `a2d2c8c` |
| 10 | `device/docker/manager.py` | Container name collision from UUIDv7 shared prefix (8 → 16 chars) | `a2d2c8c` |

### Verified Working Flows

| Flow | Status |
|------|--------|
| Gateway startup + PostgreSQL connection | Verified |
| Admin user registration + login (JWT) | Verified |
| Device enrollment via REST API | Verified |
| Device tunnel connection (WSS/WS) | Verified |
| HELLO / HELLO_ACK handshake | Verified |
| Agent pool creation on gateway (EnsurePoolSize) | Verified |
| AGENT_CREATE → Docker container creation with matching IDs | Verified |
| Agent container health check (GET /healthz) | Verified |
| Agent status sync (AGENT_STATUS → gateway "idle") | Verified |
| Agent job execution via stub brain | Verified |
| Agent skill install/disable/enable/delete | Verified |
| Agent status reporting via monitor | Verified |
| Tunnel reconnect with exponential backoff | Verified |
| Device enrollment in dev Dockerfile | Created |

### E2E Test Suite (21 tests, 4 files)

| File | Purpose | Tests |
|------|---------|-------|
| `device/tests/e2e/mock_gateway.py` | WebSocket gateway simulator (no deps beyond `websockets`) | — |
| `device/tests/e2e/conftest.py` | Fixtures: mock gateway, device lifecycle, Docker | — |
| `device/tests/e2e/test_e2e_device.py` | All e2e tests | 21 |

**Test coverage by category:**

| Category | Count | Tests |
|----------|-------|-------|
| Tunnel protocol | 4 | HELLO, reconnect, STATE_SYNC, heartbeat |
| Frame handling | 7 | JOB_DISPATCH/CANCEL, AGENT_STATUS, FILE_PUSH, SKILL_DISPATCH, VNC_OPEN, CRED_PUSH |
| Config | 1 | Env var loading |
| Docker containers | 3 | Health check, job execution (stub brain), skill CRUD |
| Robustness gaps | 6 | Outbox durability, state recovery, concurrent frames, file staging, agent failure recovery, skill dispatch to agent |

### Environment Adaptations

| Item | Detail |
|------|--------|
| Ubuntu 26.04 | System Python 3.14, PostgreSQL 18 (spec says 15) |
| Docker socket | Requires `sg docker` wrapper (user not in docker group for subprocesses) |
| Go module proxy | `GOPROXY=https://goproxy.cn,direct` (IPv6 timeout on default proxy) |
| PyPI mirror | `https://pypi.tuna.tsinghua.edu.cn/simple` |
| Docker mirror | `https://docker.m.daocloud.io` |
| venv required | Ubuntu 26.04 Python is externally managed (PEP 668) |

## Remaining Work

### A — Critical Bug (1 item)

| # | File | Issue |
|---|------|-------|
| A1 | `device/files/puller.py:54-62` | **FilePuller only sends last chunk.** The for-loop body is empty; the `await` for FILE_PULL_CHUNK and FILE_PULL_END is outside the loop. All chunks except the last are silently dropped. |

### B — Significant Gaps (4 items)

| # | File | Issue |
|---|------|-------|
| B1 | `device/jobs/dispatcher.py:58-68` | Dispatcher doesn't wait for FILE_PUSH_* completion before dispatching to agent. Agent may start with incomplete inputs. |
| B2 | `device/jobs/dispatcher.py:77-149` | Only polling for progress. Agent sends callbacks (`POST /jobs/{id}/events`) but device has no HTTP server to receive them. Callback path is dead code. |
| B3 | `agent/workspace.py:49-54` | `check_quota()` exists but is never called. Jobs can silently exceed disk limits. |
| B4 | `agent/Dockerfile` | No multi-arch build support (`linux/arm64`). Dockerfile is `linux/amd64` only. |

### C — Need Testing (4 items)

| # | What | Why |
|---|------|-----|
| C1 | OpenCode brain adapter | `agent/adapter/brain_opencode.py` has zero tests. This is the production brain. |
| C2 | Credential push → agent injection full flow | CRED_PUSH → POST /browser/state → verify storage_state written to agent profile |
| C3 | VNC bridge full round-trip | VNC_OPEN → agent VNC start → RFB byte relay → VNC_CLOSE |
| C4 | Real Go gateway integration | Device + agent pool against the actual Go gateway binary (not mock) |

### D — Minor Improvements (9 items)

| # | File | Issue |
|---|------|-------|
| D1 | `device/docker/manager.py:89` | Single global `/workspaces` mount instead of per-job mounts with read-only inputs |
| D2 | `device/monitor/monitor.py:73-76` | Device resources in HELLO hardcoded to zero (`cpu=0, mem_mb=0, disk_mb=0`) |
| D3 | `device/docker/manager.py:188` | `get_container_stats()` returns `disk_mb: 0` hardcoded — no per-container disk tracking |
| D4 | `device/__main__.py:284-305` | Graceful shutdown doesn't drain in-flight jobs or flush outbox before closing tunnel |
| D5 | `agent/server.py:104-108` | `/status` uses `psutil` which reports host-level metrics inside container, not cgroup |
| D6 | `device/vncbridge/bridge.py` | `BACKPRESSURE_BUFFER` defined but never used for throttling |
| D7 | `device/creds/relay.py:105-108` | CRED_CAPTURE assumes storage_state is string; agent returns dict |
| D8 | `agent/vnc/entrypoint.sh` | Dead code — VNC/Browser modules launch Xvfb/x11vnc directly |
| D9 | `agent/Dockerfile:127` | No tmpfs `/tmp` in Dockerfile; relies on device runtime mount |

### E — Deferred (1 item)

| # | What | Why |
|---|------|-----|
| E1 | Real Go gateway e2e test | `sg docker` subprocess management unreliable in pytest; test infrastructure needs redesign for proper device daemon lifecycle |

## Summary

| Metric | Count |
|--------|-------|
| Infrastructure items completed | 10 |
| Bugs fixed + committed | 10 |
| E2E tests created | 21 (all passing) |
| Verified working flows | 13 |
| Remaining: Critical bug | 1 |
| Remaining: Significant gaps | 4 |
| Remaining: Needs testing | 4 |
| Remaining: Minor improvements | 9 |
| **Total remaining** | **18** |

**The local device is functional for the core flow** (enroll → tunnel → pool → agent → job execution).
The blocker for connecting to a real cloud gateway is the **real Go gateway integration test (E1)** — once the
device can reliably connect + sync agents against the actual gateway, the remaining items are bug fixes
and completeness improvements.
