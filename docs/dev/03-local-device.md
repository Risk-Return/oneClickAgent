# 03-local-device — Dev Progress

| Field | Value |
|-------|-------|
| **Spec** | `docs/spec/03-local-device.md` |
| **Status** | Implemented |
| **Last Updated** | 2026-06-05 |
| **Imports** | `python -c "import iagent_device"` passes |

## Packages Implemented

| Package | Path | Status |
|---------|------|--------|
| Entry Point / CLI | `device/iagent_device/__main__.py` | Done |
| Config | `device/iagent_device/config.py` | Done |
| Tunnel Codec | `device/iagent_device/tunnel/codec.py` | Done |
| Tunnel Client | `device/iagent_device/tunnel/client.py` | Done |
| Durable Outbox | `device/iagent_device/tunnel/outbox.py` | Done |
| Store Connection | `device/iagent_device/store/connection.py` | Done |
| Store Repositories | `device/iagent_device/store/repositories.py` | Done |
| Docker Manager | `device/iagent_device/docker/manager.py` | Done |
| Docker Reconcile | `device/iagent_device/docker/reconcile.py` | Done |
| Agent HTTP Client | `device/iagent_device/agentclient/client.py` | Done |
| Job Dispatcher | `device/iagent_device/jobs/dispatcher.py` | Done |
| Job Models | `device/iagent_device/jobs/models.py` | Done |
| File Stager | `device/iagent_device/files/stager.py` | Done |
| Skill Manager | `device/iagent_device/skills/manager.py` | Done |
| VNC Bridge | `device/iagent_device/vncbridge/bridge.py` | Done |
| Credential Relay | `device/iagent_device/creds/relay.py` | Done |
| Monitor | `device/iagent_device/monitor/monitor.py` | Done |

## SQLite Tables (8)

- `device_info`, `agents`, `jobs`, `files`, `outbox`, `device_skills`, `agent_skills`, `vnc_sessions`

## CLI Commands

- `iagent-device enroll --gateway URL --code CODE`
- `iagent-device run`
- `iagent-device status`
- `iagent-device agents`
- `iagent-device logs`
- `iagent-device pull`

## Key Design Decisions

- WAL mode SQLite with busy_timeout=5000ms
- Outbox pattern for at-least-once tunnel delivery
- Pool management: create N idle agents, allocate on JOB_DISPATCH, release on completion
- VNC bridge: TCP loopback → WSS byte-relay (websockify-equivalent)
- Credential relay: pass-through only, zero disk persistence
- All Docker operations async via docker-py
- Cross-platform: `platformdirs` for data dirs, `pathlib` everywhere

## Known Gaps / TODOs

- [x] Per-agent skill state: `_emit_skill_state` includes per-agent `error` messages; `_handle_skill_action` tracks `agent_results` with error details (2026-06-05)
- [x] SKILL_RETRY handling: device receives SKILL_RETRY frame, re-runs install on specified agents (2026-06-05)
- [ ] Unit tests for all packages
- [ ] Integration test with real Docker daemon
- [ ] Mock Docker client for CI testing
- [ ] `docker` library might need fallback for environments without Docker
- [ ] VNC bridge: TCP connection currently uses loopback only (container RFB)
- [ ] Monitor: psutil gives host metrics, not per-container metrics
