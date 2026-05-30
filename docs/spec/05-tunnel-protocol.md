# 05 — Tunnel Protocol (Reverse WebSocket)

Defines the persistent **device → gateway** reverse WebSocket used to carry all command/result traffic. The device always dials out; the gateway never initiates a connection to the device.

## 1. Connection

- **Endpoint**: `wss://<gateway-host>/tunnel`
- **Subprotocol**: `iagent.tunnel.v1`
- **Auth**: HTTP header `Authorization: Bearer <device_token>` on the WS upgrade request.
- **TLS**: required (WSS). Self-signed allowed only in dev with explicit opt-in.
- **One tunnel per device.** A new successful connection for a `device_id` supersedes the old one; the gateway closes the stale socket with code `4002 SUPERSEDED`.

### Lifecycle

```
dial → HTTP 101 upgrade → device sends HELLO → gateway sends HELLO_ACK
→ steady state (frames + heartbeats) → close
```

If `HELLO` is not received within `10s` of upgrade, the gateway closes with `4003 HELLO_TIMEOUT`.

## 2. Framing

All frames are **UTF-8 JSON text** WebSocket messages (binary frames reserved for future chunk optimization). Envelope:

```jsonc
{
  "v": 1,                 // protocol version
  "type": "JOB_DISPATCH", // message type (see §4)
  "msg_id": "01J...",     // UUIDv7, unique per sender per connection
  "ack_id": "01J...",     // optional: msg_id being acknowledged
  "ts": 1730000000123,    // epoch millis (sender clock)
  "payload": { }          // type-specific body
}
```

Rules:

- Every non-ACK frame MUST be ACKed by the receiver (`type: "ACK"`, `ack_id` = original `msg_id`).
- Senders retain unacked frames and retransmit with backoff (`1s, 2s, 4s`, max 3 retries) before surfacing an error.
- Receivers MUST treat handlers as **idempotent**; duplicates (same `msg_id`) are ACKed but processed once.
- Max frame size: **1 MiB**. Larger payloads (files) MUST be chunked (§5).

## 3. Heartbeats & Liveness

- Device sends `PING` every **15s**; gateway replies `PONG`.
- Gateway marks device `OFFLINE` if no frame (incl. PING) for **45s** (3 missed) and closes the socket.
- WebSocket-level ping/pong MAY also be used; application `PING/PONG` is authoritative for status.

## 4. Message Types

### 4.1 Control (both directions unless noted)

| Type | Dir | Payload |
|------|-----|---------|
| `HELLO` | D→G | `{ device_id, agent_version, platform, agents:[{agent_id, status, port, tags}], resources:{cpu, mem_mb, disk_mb} }` |
| `HELLO_ACK` | G→D | `{ server_time, session_id, config:{heartbeat_s, max_frame_bytes} }` |
| `PING` / `PONG` | both | `{}` |
| `ACK` | both | `{}` (uses `ack_id`) |
| `ERROR` | both | `{ code, message, ref_msg_id? }` |
| `STATE_SYNC` | D→G | `{ jobs:[{job_id, status, percent}], agents:[{agent_id, status}] }` |

### 4.2 Job control (gateway → device)

| Type | Payload |
|------|---------|
| `JOB_DISPATCH` | `{ job_id, agent_id, command, params, file_ids:[], skill_id?, submitted_at }` (at most one `skill_id`) |
| `JOB_CANCEL` | `{ job_id, reason }` |
| `JOB_QUERY` | `{ job_id }` |

### 4.3 Job events (device → gateway)

| Type | Payload |
|------|---------|
| `JOB_ACCEPTED` | `{ job_id, agent_id }` (→ QUEUED) |
| `JOB_PROGRESS` | `{ job_id, event_seq, status, percent, message }` |
| `JOB_RESULT` | `{ job_id, status:"SUCCEEDED"|"FAILED", result:{...}, error?:{code,message}, finished_at }` |
| `JOB_REJECTED` | `{ job_id, code, message }` |

> **Progress-only contract:** `message` is a human-readable status string. Raw logs, terminal output, stack traces, and internal chain-of-thought MUST NOT be sent over the tunnel to the user-facing path (see `09-web-ui.md`).

### 4.4 Agent management (gateway → device)

| Type | Payload |
|------|---------|
| `AGENT_CREATE` | `{ agent_id, image, tags, limits:{cpu, mem_mb, disk_mb}, env? }` |
| `AGENT_ACTION` | `{ agent_id, action:"start"|"stop"|"restart"|"remove" }` |
| `AGENT_STATUS_REQ` | `{ agent_id? }` |

### 4.5 Agent telemetry (device → gateway)

| Type | Payload |
|------|---------|
| `AGENT_STATUS` | `{ agent_id, status, health, restarts, usage:{cpu_pct, mem_mb, disk_mb}, ts }` |

### 4.6 Skills (gateway ⇄ device ⇄ agent)

Two scopes:
- **`device`** — admin operation: install/disable/update/delete a skill for **all agents** on a device.
- **`agent`** — user selection: enable/disable an already-installed skill on a specific agent.

Skill packages are dispatched from the **cloud skill vault** to the device, chunked like files.

**Package dispatch (G→D)** — sent before an `install`/`update` that needs the artifact:

| Type | Payload |
|------|---------|
| `SKILL_DISPATCH_BEGIN` | `{ skill_id, key, name, version, manifest, size, sha256, chunks }` |
| `SKILL_CHUNK` | `{ skill_id, version, index, data_b64 }` |
| `SKILL_DISPATCH_END` | `{ skill_id, version }` |
| `SKILL_DISPATCH_ACK` (D→G) | `{ skill_id, version, status:"CACHED"|"ERROR", message? }` |

**Actions (G→D)**:

| Type | Payload |
|------|---------|
| `SKILL_ACTION` | `{ skill_id, scope:"device"|"agent", agent_id?, action:"install"|"disable"|"enable"|"update"|"delete", version? }` |
| `SKILL_SYNC` | `{ device_skills:[{skill_id, version, status}], agent_skills:[{agent_id, skill_id, status}] }` — full desired state, sent on (re)connect to reconcile |

`scope:"device"` actions (`install`/`disable`/`update`/`delete`) are **admin-issued** and applied by the device to every agent it hosts. `scope:"agent"` actions (`enable`/`disable`) are user-issued and target one `agent_id`.

**State report (D→G)**:

| Type | Payload |
|------|---------|
| `SKILL_STATE` | `{ device_skills:[{skill_id, version, status}], agent_skills:[{agent_id, skill_id, status}] }` |

- Chunk size and integrity follow the file rules (§5): 256 KiB chunks, `sha256` verified on `SKILL_DISPATCH_END`.
- The device caches the skill package and applies/reapplies it to agents (incl. agents created later) per the desired state.

### 4.7 Files (§5)

| Type | Dir | Payload |
|------|-----|---------|
| `FILE_PUSH_BEGIN` | G→D | `{ file_id, job_id, name, size, sha256, chunks }` |
| `FILE_CHUNK` | G→D | `{ file_id, index, data_b64 }` |
| `FILE_PUSH_END` | G→D | `{ file_id }` |
| `FILE_ACK` | D→G | `{ file_id, status:"STAGED_DEVICE"|"ERROR", message? }` |
| `FILE_PURGED` | D→G | `{ file_id, job_id }` |

## 5. File Transfer

- Inputs flow **gateway → device** only (user uploads). Results are returned in `JOB_RESULT` (small) or, for large artifacts, via a result-file flow mirroring §4.7 in the device→gateway direction (`FILE_PULL_*`, reserved).
- Chunk size: **256 KiB** base64-encoded payload (keeps frame < 1 MiB).
- Integrity: `sha256` verified on `FILE_PUSH_END`; mismatch → `FILE_ACK status=ERROR` and the gateway retries the whole file.
- Backpressure: device ACKs each `FILE_CHUNK`; gateway keeps ≤ 8 chunks in flight per file.

## 6. Close Codes

| Code | Meaning |
|------|---------|
| `1000` | Normal closure |
| `4001` | Auth failed / invalid device_token |
| `4002` | Superseded by newer connection |
| `4003` | HELLO timeout |
| `4004` | Protocol violation (bad frame / oversized) |
| `4005` | Token revoked |
| `4290` | Rate limited / overloaded (retry with backoff) |

## 7. Reconnect Policy (device side)

- Exponential backoff: `min(30s, 1s * 2^attempt)` + jitter `±20%`.
- On reconnect: re-send `HELLO`, then `STATE_SYNC`, then flush buffered `JOB_PROGRESS`/`JOB_RESULT` frames persisted in SQLite.
- Never drop terminal results: `JOB_RESULT` is persisted until ACKed by the gateway.

## 8. Versioning

- `v` and subprotocol `iagent.tunnel.v1` are bumped together on breaking changes.
- Gateway MAY support multiple versions concurrently; `HELLO_ACK` echoes the negotiated version.
