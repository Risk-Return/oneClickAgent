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

All frames on the **main control tunnel** are **UTF-8 JSON text** WebSocket messages. (Binary, low-latency byte streams — e.g. interactive VNC — use a **separate session-relay socket**, §9, not the control tunnel.) Envelope:

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
| `JOB_DISPATCH` | `{ job_id, agent_id, command, params, file_ids:[], skill_id?, credential_ids:[]?, submitted_at }` (at most one `skill_id`; `credential_ids` = saved logins to inject — the decrypted storage-state is pushed separately via `CRED_PUSH`, §10) |
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
| `SKILL_STATE` | `{ device_skills:[{skill_id, version, status}], agent_skills:[{agent_id, skill_id, status, error?}] }` |

- `device_skills` reports overall device-level status (`installing`/`installed`/`disabled`/`error`).
- `agent_skills` reports per-agent status, including an optional `error` message when an individual agent's install fails. Only agents with non-`installed` status need to be reported; the gateway treats absent agents as `installed` when the device-level status is `installed`.
- The device MUST send `SKILL_STATE` after each agent install attempt (success or failure), so the gateway has per-agent granularity.

**Retry (G→D)**:

| Type | Payload |
|------|---------|
| `SKILL_RETRY` | `{ skill_id, agent_ids?:[], version? }` — retry install on specific agents that failed. If `agent_ids` is omitted or empty, retry all agents on the device that are not `installed`. |

- `SKILL_RETRY` is sent by the admin from the Fleet Rollout UI when individual agent installs fail. The device re-runs `POST agent /skills` for each specified agent.
- The device responds with `SKILL_STATE` per agent as usual.
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

### 4.8 Interactive VNC session control (§9)

Control frames that set up / tear down an interactive browser (VNC) session. The actual RFB byte stream travels on a **separate session-relay socket** (§9), not here.

| Type | Dir | Payload |
|------|-----|---------|
| `VNC_OPEN` | G→D | `{ session_id, agent_id, job_id, relay_url, session_token, ttl_s }` — open a bridge: dial `relay_url`, start the agent VNC stack |
| `VNC_OPENED` | D→G | `{ session_id, status:"ready"|"error", rfb_password?, message? }` — `rfb_password` is the agent's one-time RFB secret, relayed to the browser by the gateway |
| `VNC_CLOSE` | both | `{ session_id, reason }` — either side may close (user closed, job terminal, timeout) |

### 4.9 Credential transfer (§10)

Login storage-state (cookies + localStorage) moved between the encrypted cloud vault and the container's ephemeral browser profile.

| Type | Dir | Payload |
|------|-----|---------|
| `CRED_PUSH` | G→D | `{ job_id, credential_id, origin, storage_state, sha256 }` — decrypted storage-state to inject for a job; chunked like files if > frame cap |
| `CRED_PUSH_ACK` | D→G | `{ job_id, credential_id, status:"INJECTED"|"ERROR", message? }` |
| `CRED_CAPTURE` | D→G | `{ session_id, job_id, label, origin, storage_state, sha256 }` — a login captured from a VNC session, to be encrypted + stored |
| `CRED_CAPTURE_ACK` | G→D | `{ credential_id, status:"STORED"|"ERROR", message? }` |

### 4.10 Output file relay (§11)

Job result files (generated in `/work/output` on the agent container) flow **device → gateway** using `FILE_PULL_*` frames. Symmetrical to `FILE_PUSH_*` (§4.7) but in the reverse direction. Triggered when a job completes with files in the output directory.

| Type | Dir | Payload |
|------|-----|---------|
| `FILE_PULL_BEGIN` | D→G | `{ file_id, job_id, name, size, sha256, chunks }` |
| `FILE_PULL_CHUNK` | D→G | `{ file_id, index, data_b64 }` |
| `FILE_PULL_END` | D→G | `{ file_id }` |
| `FILE_PULL_ACK` | G→D | `{ file_id, status:"RECEIVED"|"ERROR", message? }` |

- Device reads files from the agent container's `/work/output` directory after job completion.
- Chunk size and backpressure mirror §5 (256 KiB, ≤ 8 in-flight).
- Gateway stores received files in the file store under `jobs/{job_id}/output/` and records metadata in `files` table.
- Files are available for download via `GET /jobs/{job_id}/output/{file_id}`.

## 5. File Transfer

- Inputs flow **gateway → device** (user uploads, `FILE_PUSH_*`).
- Outputs flow **device → gateway** (agent results, `FILE_PULL_*`).
- Chunk size: **256 KiB** base64-encoded payload (keeps frame < 1 MiB).
- Integrity: `sha256` verified on `FILE_*_END`; mismatch → `FILE_*_ACK status=ERROR` and the sender retries the whole file.
- Backpressure: receiver ACKs each `FILE_*_CHUNK`; sender keeps ≤ 8 chunks in flight per file.

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
- The session-relay subprotocol `iagent.session.v1` (§9) is versioned independently of the control tunnel.

## 9. Interactive Session Relay (VNC)

Interactive VNC is **binary, low-latency, and high-bandwidth** — unsuitable for the JSON/base64 control tunnel (1 MiB frame cap, ACK-per-frame). It uses a **separate, on-demand WebSocket** the device dials out per session, so the device still needs no inbound ports.

### Establishment

```
1. Browser → POST /jobs/{id}/vnc (07-api §5.1) → Gateway creates vnc_sessions row + session_token (short TTL)
2. Gateway → VNC_OPEN {session_id, relay_url, session_token, ttl_s} over the MAIN control tunnel → Device
3. Device → POST agent /vnc/start → agent brings up Xvfb+x11vnc, returns rfb_port + rfb_password
4. Device → VNC_OPENED {session_id, status:"ready", rfb_password} on the main tunnel
5. Device → dials a SECOND socket out:  wss://<gateway>/session/<session_id>
        Subprotocol: iagent.session.v1 ;  Authorization: Bearer <session_token>
6. Gateway pairs the device session socket with the browser noVNC socket
        (wss://<gateway>/ws/vnc/<session_id>, 07-api §9.1) by session_id
7. Gateway hands rfb_password to the browser (POST response / noVNC config); RFB auth proceeds end-to-end
```

### Data plane

```
browser noVNC ⇄ [Gateway /ws/vnc/{sid}] ⇄ raw RFB bytes ⇄ [Gateway /session/{sid}] ⇄ device ⇄ 127.0.0.1:rfb_port (x11vnc)
```

- The session socket carries **binary** WebSocket messages = raw RFB bytes, **no JSON envelope, no per-message ACK**. The device is the `websockify`-equivalent (TCP↔WS bridge to the container's loopback RFB port); the gateway is a transparent byte relay and **does not parse RFB**.
- **Auth**: `session_token` is single-use, bound to `(session_id, device_id, user_id)`, TTL `ttl_s` (default 60s to connect; the established socket lives for the session). The browser side is authorized by the user's JWT + `session_id` ownership.
- **Lifecycle / teardown**: any of {user closes, job terminal, idle > `IAGENT_VNC_IDLE_TTL`, max > `IAGENT_VNC_MAX_TTL`} closes the pair; the gateway sends `VNC_CLOSE` on the main tunnel and the device calls agent `POST /vnc/stop`. A closed/half-open peer closes the other side.
- **Backpressure**: standard WS flow control; the gateway caps per-session in-flight bytes and closes the session on sustained overflow (`4290`-style).
- **Concurrency**: multiple sessions per device are allowed (at most one per active job that opens VNC); each is its own socket keyed by `session_id`.
- **Close codes** reuse §6 (`4001` bad/expired session_token, `4002` session superseded, `4290` overloaded).

## 10. Credential Transfer (Login Cookies)

Moves browser login storage-state between the **encrypted cloud vault** (`06-data-model §1.16`) and a container's ephemeral `/work/profile`. The gateway is the **only** holder of the encryption key: it decrypts just-in-time before `CRED_PUSH` and encrypts on `CRED_CAPTURE`. Storage-state is never persisted on the device.

### Inject (gateway → device, per job)

```
On JOB_DISPATCH carrying credential_ids:
  for each credential_id: Gateway decrypts storage_state from the vault
    → CRED_PUSH {job_id, credential_id, origin, storage_state, sha256}
  Device → POST agent /browser/state {storage_state}  (verify sha256 first)
    → CRED_PUSH_ACK {status:"INJECTED"|"ERROR"}
  Agent writes into /work/profile before brain.run; wiped on job terminal
```

### Capture (device → gateway, save login from a VNC session)

```
User clicks "save login" in the web UI during a VNC session:
  Gateway → (control) instructs device to capture for the session's origin
  Device → GET agent /browser/state?origin=<site>  → storage_state
  Device → CRED_CAPTURE {session_id, job_id, label, origin, storage_state, sha256}
  Gateway encrypts (AES-256-GCM, envelope key) + stores in browser_credentials
    → CRED_CAPTURE_ACK {credential_id, status:"STORED"}
```

- **Chunking**: storage-state is usually < 1 MiB and fits one frame; if larger, chunk exactly like files (§5: 256 KiB base64 chunks, `sha256` verified on completion).
- **Idempotency**: `CRED_PUSH`/`CRED_CAPTURE` handlers are idempotent by `(job_id, credential_id)` / `(session_id, origin)`.
- **Hygiene**: device never writes storage-state to SQLite or disk; it streams straight to/from the agent. The agent confines it to `/work/profile` and wipes on terminal state. Never logged on any hop. See `08-auth-security §13`.
