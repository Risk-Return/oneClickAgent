# 05-tunnel-protocol — Dev Progress

| Field | Value |
|-------|-------|
| **Spec** | `docs/spec/05-tunnel-protocol.md` |
| **Status** | Implemented (audit gaps fixed 2026-06-02; output relay + skill retry added 2026-06-05) |
| **Last Updated** | 2026-06-05 |
| **Audit** | `docs/audit/05-tunnel-protocol.md` (5 critical, 8 significant, 4 minor — all resolved) |
| **Build (Go)** | `go build ./...` passes |
| **Vet (Go)** | `go vet ./...` passes |
| **Tests (Go)** | `go test ./internal/tunnel/...` — 15/15 pass |
| **Lint (Python)** | `ruff check iagent_device/tunnel/` — all checks passed |
| **Typecheck (Python)** | `mypy iagent_device/tunnel/` — no issues |
| **Tests (Python)** | `pytest tests/test_tunnel_client.py tests/test_codec.py tests/test_outbox.py` — 27/27 pass |

## Packages Implemented

| Package | Path | Status |
|---------|------|--------|
| Tunnel Codec (Go) | `gateway/internal/tunnel/codec.go` | Done |
| Device Connection | `gateway/internal/tunnel/device_conn.go` | Done |
| Tunnel Hub | `gateway/internal/tunnel/hub.go` | Done |
| Device Registry | `gateway/internal/tunnel/registry.go` | Done |
| Frame Router | `gateway/internal/tunnel/router.go` | Done |
| Frame Types & Payloads | `gateway/internal/model/types.go` | Done |
| Tunnel WS Handler | `gateway/internal/httpapi/tunnel_handler.go` | Done |
| Route Registration | `gateway/internal/httpapi/router.go` (GET /tunnel) | Done |
| Device Store (token lookup) | `gateway/internal/store/devices.go` | Done |
| Tunnel Codec (Python) | `device/iagent_device/tunnel/codec.py` | Done |
| Tunnel Client | `device/iagent_device/tunnel/client.py` | Done |
| Durable Outbox | `device/iagent_device/tunnel/outbox.py` | Done |

## Key Design Decisions Matched to Spec

### Connection & Framing (§1–2)
- **Subprotocol**: `iagent.tunnel.v1` (negotiated during WS upgrade)
- **Auth**: `Authorization: Bearer <device_token>`; gateway hashes with SHA-256 and looks up via `GetByTokenHash`
- **One tunnel per device**: new connection with same `device_id` supersedes old one with close code `4002`
- **JSON envelope**: `{v, type, msg_id, ack_id?, ts, payload}`
- **Max frame size**: 1 MiB (enforced on both encode and decode)
- **ACK rule**: every non-ACK frame is ACKed by the receiver

### HELLO Lifecycle (§1)
- **HELLO timeout**: gateway closes with `4003 HELLO_TIMEOUT` if HELLO not received within 10s of upgrade (goroutine-based timer in `DeviceConn.StartReadPump`)
- **HELLO payload** (D→G): `{device_id, agent_version, platform, agents:[{agent_id, status, port, tags}], resources:{cpu_count, memory_mb, disk_mb}}`
- **HELLO_ACK payload** (G→D): `{server_time, session_id, config:{heartbeat_s, max_frame_bytes}}`
- Device applies `config.heartbeat_s` from HELLO_ACK to its own heartbeat interval

### Idempotency (§2)
- **Gateway** (`DeviceConn`): `processed` sync.Map tracks msg_ids; duplicates are ACKed but handlers are skipped
- **Device** (`TunnelClient`): `_processed_msg_ids` set tracks msg_ids; duplicates are logged and dropped
- Non-stateful frames (ACK, PING, PONG) are exempt from dedup

### ACK Retransmit (§2)
- **Gateway** (`AckTracker.RetransmitReady`): exponential backoff — 1s, 2s, 4s, max 3 retries
- **Retransmitter**: per-connection background goroutine (`StartRetransmitter`) ticks every 1s, retransmits ready frames, drops frames that exceed max retries
- **Device** (`TunnelClient._retransmit_loop`): retransmits unacked frames with 1s/2s/4s backoff, max 3 retries; marks future as `TimeoutError` on failure

### Heartbeats & Liveness (§3)
- Device sends `PING` every 15s (or configurable from HELLO_ACK)
- Gateway replies `PONG`
- Gateway `LivenessChecker` marks device OFFLINE after 45s (3 missed) and closes with code `4001`
- **WS-level ping/pong**: gateway sends WebSocket `PingMessage` every 15s as secondary liveness check (`StartWritePump`); `SetPongHandler` in `StartReadPump`

### Message Types (§4)
- **32 frame types** implemented across Go model (`Frame*` constants) and Python codec (`FrameType` StrEnum)
- All 9 categories: control, job control, job events, agent management, agent telemetry, skills, files, VNC, credentials
- Frame routing: `DeviceConn.handleFrame()` switch (15 types), `Router.RegisterAll()` (11 types)

### Close Codes (§6)
| Code | Meaning | Implemented |
|------|---------|-------------|
| `1000` | Normal closure | Yes |
| `4001` | Auth failed / heartbeat timeout | Yes |
| `4002` | Superseded by newer connection | Yes |
| `4003` | HELLO timeout | Yes |
| `4004` | Protocol violation (bad frame, oversized, wrong version) | Yes |
| `4005` | Token revoked (periodic check via `StartTokenWatcher`) | Yes |
| `4290` | Rate limited (per-connection frame rate cap, 100 fps) | Yes |

### Reconnect Policy (§7)
- **Device**: exponential backoff `min(30s, 1s * 2^attempt)` + ±20% jitter
- On reconnect: re-send HELLO → STATE_SYNC → flush outbox
- `max_reconnect_attempts` configurable (0 = infinite)
- `JOB_RESULT` persisted in outbox until ACKed

### Tunnel Endpoint (new)
- **HTTP route**: `GET /tunnel` (registered with public access, auth via `device_token` header)
- **Upgrader**: subprotocol `iagent.tunnel.v1`, same `CheckOrigin` policy as other WS endpoints

## File Transfer (§5)

File push (G→D) with 256 KiB base64 chunks + SHA-256 verification is handled by `gateway/internal/relay/` and `device/iagent_device/files/stager.py` — not in-scope for the tunnel transport layer itself.

## VNC Session Relay (§9)

Separate binary WS socket (`/session/{sessionID}`, subprotocol `iagent.session.v1`) handled by `gateway/internal/vncrelay/` and `device/iagent_device/vncbridge/`. Tunnel control frames (`VNC_OPEN`, `VNC_OPENED`, `VNC_CLOSE`) ride on the main control tunnel and are handled in `device_conn.go` / `router.go`.

## Known Gaps / TODOs

- [x] Subprotocol name aligned to spec (`iagent.tunnel.v1` — was `oneClickAgent.tunnel.v1`)
- [x] HELLO timeout: gateway closes with `4003` after 10s
- [x] HELLO_ACK carries `server_time`, `session_id`, `config.{heartbeat_s, max_frame_bytes}`
- [x] ACK retransmit: exponential backoff (1s/2s/4s, max 3x) in both gateway and device
- [x] Idempotency: msg_id dedup on both gateway (`sync.Map`) and device (`set`)
- [x] HELLO payload: `agent_version`, `platform`, `port`, `tags` fields
- [x] JOB_DISPATCH payload: `params`, `credential_ids`, `submitted_at` fields
- [x] JOB_PROGRESS payload: `event_seq` field
- [x] VNC_OPEN payload: `agent_id`, `job_id` fields
- [x] Missing frame type constants: `SKILL_DISPATCH_ACK`, `FILE_PURGED`
- [x] Router.RegisterAll wired for VNC and Cred handlers
- [x] Device applies `heartbeat_s` from HELLO_ACK config
- [x] Tunnel WS endpoint (`/tunnel`) wired in HTTP router with device token auth
- [x] `GetByTokenHash` device store method + `ErrNotFound` sentinel
- [x] Gateway connection supersede test (`TestHubSupersede`)
- [x] Gateway ACK retransmit test (`TestAckTrackerRetransmit` — verifies 1s/2s/4s backoff + max 3 retries)
- [x] Gateway idempotency test (`TestIdempotency` — duplicate msg_ids ACKed but processed once)
- [x] Gateway rate limit test (`TestRateLimit` — 100 fps cap)
- [x] Gateway HELLO payload handling test (`TestHandleHelloAckPayload`)
- [x] Close code `4004` (protocol violation): closes on bad frame, oversized frame, wrong version
- [x] Close code `4005` (token revoked): `StartTokenWatcher` periodic check via `TokenVerifier` callback
- [x] Close code `4290` (rate limited): per-connection `checkRateLimit` with 100 fps cap
- [x] WebSocket-level ping/pong: WS `PingMessage` every 15s + `SetPongHandler` for secondary liveness
- [x] Device ACK retransmit with backoff (`_retransmit_loop` — 1s/2s/4s, max 3x)
- [x] Deadlock fix: `Hub.Register` releases lock before calling `conn.Close`
- [x] Identity-safe `Unregister`: only unregisters matching connection, not superseded replacement
- [x] (Audit 1.3) `Hub.SendFrame` now calls `acks.Track()` — all G→D frames retransmitted with backoff
- [x] (Audit 1.1) `CredPushPayload.Data` → `StorageState` JSON field name — device can read storage_state
- [x] (Audit 1.2) Gateway handles `FrameCredCapture` (D→G) in read pump + `HandleCredCapture` on Hub
- [x] (Audit 1.4) `handleVNCDeviceSocket` reads `Authorization: Bearer <token>` per spec §9
- [x] (Audit 1.5) CRED_CAPTURE instruction includes `AgentID`, `JobID` from VNC session
- [x] (Audit 2.1) `handleCancelJob` sends `JOB_CANCEL` frame over tunnel before releasing agent
- [x] (Audit 2.3) `NewFrame()` sets `TS: time.Now().UnixMilli()` — all outbound frames have timestamps
- [x] (Audit 2.4) Gateway `handleFrame` sends ACK for HELLO frame
- [x] (Audit 2.6) Handlers added for `SKILL_DISPATCH_ACK`, `FILE_PURGED` in device_conn + router + Hub
- [x] (Audit 2.7) `VNC_CLOSE` from device handled via `HandleVNCClose` + `FrameVNCClose` case
- [x] (Audit 2.8) `VNC_OPEN` payload includes `AgentID`, `JobID`
- [x] (Audit 2.2) `JobDispatchPayload` populated with `Params`, `SubmittedAt`
- [x] (Audit 2.5) `AgentCreatePayload` includes `Image`, `Tags`, `Limits`; allocator sets defaults
- [x] (Audit 3.1) Device HELLO payload includes `agent_count`
- [x] (Audit 3.3) **Rejected**: HELLO_ACK is a response frame, not an ACK. Spec §4.1 lists them separately. ACK for HELLO is sent via separate `sendAck()` call (Audit 2.4 fix).

### Audit fixes — cross-module touch points

## Git History

- 2026-06-05 — feat: FILE_PULL frames for output file relay (D→G) with chunk buffer, SHA256 verify, file store; SKILL_RETRY spec for per-agent retry
- (pending commit) — fix(tunnel): resolve all 16 audit gaps — ACK tracking, credential field names, VNC auth, cancel dispatch, G→D timestamps, HELLO/CAPTURE/CLOSE/DISPATCH/ACK/PURGED handlers, allocator payloads
- (previous) — feat(tunnel): full spec compliance — HELLO timeout, ACK retransmit (both sides), idempotency, close codes 4004/4005/4290, WS-level ping/pong, device retransmit, tunnel endpoint, tests
