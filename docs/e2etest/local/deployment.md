# Local Device E2E Testing

Testing the local device + agent containers against a mock cloud gateway on Ubuntu 26.04.

## Stack

| Component | Port | Tech |
|-----------|------|------|
| Mock Gateway | dynamic | `websockets` 16.0, speaks `iagent.tunnel.v1` |
| Device | — | Python 3.14, in-process `TunnelClient` |
| Agent Container | 42200-42202 | `iagent/agent:dev`, Ubuntu 24.04, stub brain |
| SQLite | `:memory:` / temp dir | WAL mode, full schema |

## Test Harness

**MockGateway** (`device/tests/e2e/mock_gateway.py`) — WS-only server, no dependencies beyond `websockets` + stdlib:
- Registers device tokens for auth
- Sends HELLO_ACK on HELLO, PONG on PING, ACK on every non-ACK frame
- Captures all other frames in per-device queues for test assertions
- Supports disconnect() for reconnect testing

**Fixtures** (`device/tests/e2e/conftest.py`):
- `mock_gateway` — starts/stops the mock gateway on a random port
- `device_enrolled` — injects device_id + token into local SQLite, registers with mock gateway
- `device_connected` — starts `TunnelClient`, `Monitor`, `DockerManager` in-process, waits for connection
- `agent_image` — checks `iagent/agent:dev` exists, skips if not built
- `docker_required` — skips tests if Docker socket not accessible

## Test Results — 21/21 passing (84s)

### Tunnel Protocol (4 tests)

| Test | What It Validates |
|------|-------------------|
| `test_tunnel_connect_hello` | Device dials mock gateway, sends HELLO with platform/agents/capabilities, receives HELLO_ACK |
| `test_tunnel_reconnect` | Disconnect mock gateway, device reconnects within 20s with exponential backoff |
| `test_state_sync_on_reconnect` | STATE_SYNC frame received after each reconnect with jobs[] and agents[] |
| `test_ping_pong_heartbeat` | Gateway PING → device PONG, connection stays alive |

### Frame Handling (7 tests)

| Test | What It Validates |
|------|-------------------|
| `test_job_dispatch_lifecycle` | JOB_DISPATCH frame received by device, payload contains job_id + command |
| `test_job_cancel` | JOB_CANCEL frame received, job_id matches |
| `test_agent_status_reporting` | Monitor._sample() emits AGENT_STATUS with agent_id + status |
| `test_file_push_begin_chunk_end` | FILE_PUSH_BEGIN→CHUNK→END flow, FILE_PUSH_END handler fires |
| `test_skill_dispatch_flow` | SKILL_DISPATCH_BEGIN→CHUNK→END flow, SKILL_DISPATCH_END handler fires |
| `test_vnc_open_close` | VNC_OPEN frame received by VNC bridge handler |
| `test_credential_push` | CRED_PUSH frame with valid storage_state + sha256 received |

### Config (1 test)

| Test | What It Validates |
|------|-------------------|
| `test_config_env_vars` | IAGENT_GATEWAY_URL, IAGENT_POOL_SIZE read from environment |

### Real Docker Agent Containers (3 tests)

| Test | What It Validates |
|------|-------------------|
| `test_real_agent_container_health` | `docker run iagent/agent:dev` → GET /healthz (200 ok), GET /status (skills, usage, current_job) |
| `test_real_agent_job_execution` | POST /jobs (202 accepted), stub brain executes → completes (succeeded/failed or 404 after cleanup), agent returns to idle |
| `test_real_agent_skill_install` | POST /skills (204), verify in /status skills list, POST disable (204), POST enable (204), DELETE (204), verify removed |

### Robustness / Gap Tests (7 tests)

| Test | What It Validates |
|------|-------------------|
| `test_outbox_durability` | Frame enqueued before disconnect, flushed on reconnect, received by gateway |
| `test_device_resilience_state_recovery` | Pre-populated SQLite state (agents+jobs) recovered after tunnel restart |
| `test_concurrent_frames` | 10 rapid JOB_DISPATCH frames, all dispatched, no duplicates |
| `test_file_staging_full_lifecycle` | FILE_PUSH_BEGIN→CHUNK→END stages file to workspace, verify content on disk, cleanup removes dir |
| `test_agent_failure_recovery` | Docker kill agent container, verify unreachable, restart, verify healthy again |
| `test_skill_dispatch_to_agent` | Install skill on container, execute job referencing skill, verify terminal state |

## Bugs Found During E2E Development

| # | File | Bug |
|---|------|-----|
| 1 | `conftest.py` | `DockerManager()` parameter name `repo` → should be `agent_repo` |
| 2 | `conftest.py` | `Monitor()` parameter name `repo` → should be `agent_repo` |
| 3 | `test_e2e_device.py` | Job execution poll: 404 response (job cleaned up) treated as success, not error |

## Running the Tests

```bash
cd device
source venv/bin/activate

# All tests
python -m pytest tests/e2e/ -v --timeout=60

# Tunnel protocol only (no Docker required)
python -m pytest tests/e2e/ -v --timeout=30 -k "not real_agent"

# Docker container tests only
python -m pytest tests/e2e/ -v --timeout=60 -k "real_agent"
```
