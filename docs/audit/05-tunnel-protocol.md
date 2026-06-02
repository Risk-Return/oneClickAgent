# 05-tunnel-protocol — Implementation Audit

> Audited against: `docs/spec/05-tunnel-protocol.md`
> Dev record: `docs/dev/05-tunnel-protocol.md`
> Date: 2026-06-02

## Summary

| Category | Count |
|----------|-------|
| Critical gaps | 5 |
| Significant gaps | 8 |
| Minor gaps | 4 |

---

## 1. Critical Gaps

### 1.1 `CRED_PUSH` field name mismatch — credentials never reach the agent

- **File:** `gateway/internal/model/types.go:858-864` vs `device/iagent_device/creds/relay.py:24`
- **Severity:** Critical
- **What's wrong:** The Go `CredPushPayload` struct uses JSON tag `json:"data"` for the storage-state field, but the device's `handle_cred_push` reads `payload.get("storage_state", "")`. The spec (§4.9, §10) explicitly names this field `storage_state`. When the gateway sends `CRED_PUSH`, the JSON key is `data` and the device implicitly gets an empty string — the SHA-256 check silently passes (empty string hash known), the agent receives no actual credentials, and the device reports `INJECTED` status. Credential injection is completely non-functional.
- **Spec ref:** §4.9 `CRED_PUSH` payload: `{ job_id, credential_id, origin, storage_state, sha256 }`; §10: "Gateway decrypts storage_state from the vault → CRED_PUSH {storage_state}"

### 1.2 `CRED_CAPTURE` handler type mismatch — credential capture flow broken

- **File:** `gateway/internal/tunnel/device_conn.go:367-375` and `gateway/internal/tunnel/router.go:124-130`
- **Severity:** Critical
- **What's wrong:** Both `DeviceConn.handleFrame()` and `Router.RegisterAll()` handle `FrameCredCaptureAck` (G→D) on the gateway's **read pump** path. But per spec §4.9, `CRED_CAPTURE_ACK` is sent **from gateway to device**, while the device sends `CRED_CAPTURE` (with actual storage-state data) **to the gateway**. The gateway should handle the inbound `FrameCredCapture` (D→G) and reply with `FrameCredCaptureAck`. Instead, the gateway handles the wrong type — so the device's `CRED_CAPTURE` frame (sent from `creds/relay.py:110`) is silently ignored by the gateway read pump (it falls through to the `default` case at `device_conn.go:377-378`). The save-login flow is completely broken.
- **Spec ref:** §4.9: `CRED_CAPTURE` is D→G; `CRED_CAPTURE_ACK` is G→D; §10: "Device → CRED_CAPTURE {session_id, job_id, label, origin, storage_state, sha256} / Gateway ... → CRED_CAPTURE_ACK {credential_id, status:"STORED"}"

### 1.3 Gateway→device frames never tracked for ACK/retransmission

- **File:** `gateway/internal/tunnel/hub.go:200-216` and `gateway/internal/tunnel/device_conn.go:509-579`
- **Severity:** Critical
- **What's wrong:** `Hub.SendFrame()` enqueues frames directly to `conn.outbound` without ever calling `conn.acks.Track(frame)`. The `AckTracker` is fully implemented (exponential backoff 1s/2s/4s, max 3 retries, background retransmitter via `StartRetransmitter`), but it is **never fed any frames** from the gateway→device direction. Every critical outbound frame — `JOB_DISPATCH`, `JOB_CANCEL`, `VNC_OPEN`, `VNC_CLOSE`, `CRED_PUSH`, `CRED_CAPTURE` (instruction), `FILE_PUSH_BEGIN/CHUNK/END`, `SKILL_*`, `AGENT_CREATE`, `AGENT_ACTION` — is sent fire-and-forget with no retransmission guarantee. If a frame is lost (network drop before ACK), it is gone forever. The `Track` method is only called in tests (`tunnel_test.go:117,150`), never in production code.
- **Spec ref:** §2: "Senders retain unacked frames and retransmit with backoff (1s, 2s, 4s, max 3 retries) before surfacing an error."

### 1.4 VNC session relay auth mismatch — binary relay socket never connects

- **File:** `device/iagent_device/vncbridge/bridge.py:76-81` vs `gateway/internal/httpapi/vnc_handler.go:223-250`
- **Severity:** Critical
- **What's wrong:** The device sends the `session_token` as `Authorization: Bearer <session_token>` header on the session relay WebSocket connect (`bridge.py:78`). But the gateway's `handleVNCDeviceSocket` only checks `X-Session-Token` header (line 225) or URL query param `token` (line 227-228). It never reads the `Authorization` header. The `relay_url` sent in `VNC_OPEN` (line 53) also contains no query parameter. Result: the device's session relay socket is rejected with `"token required"` (HTTP 401). The binary VNC RFB relay is completely non-functional.
- **Spec ref:** §9: "Device → dials a SECOND socket out: wss://<gateway>/session/<session_id> / Subprotocol: iagent.session.v1 ; Authorization: Bearer <session_token>"

### 1.5 `CRED_CAPTURE` instruction from gateway missing `agent_id`

- **File:** `gateway/internal/httpapi/vnc_handler.go:110-116` and `device/iagent_device/creds/relay.py:87-123`
- **Severity:** Critical
- **What's wrong:** When the user clicks "save login", the gateway sends a `CRED_CAPTURE` frame to instruct the device, but the payload only contains `SessionID` and `Label`. The device's `handle_cred_capture` reads `agent_id` from the payload (line 89) — which is empty — then calls `self.docker.get_client("")` which returns `None`, falling into the error path that sends `CRED_CAPTURE_ACK` with status `"error"` (lines 95-101). This gap compounds with gap 1.2 (the gateway can't even process the error ACK because it handles the wrong frame type). Even if gap 1.2 were fixed, the save-login flow would still fail because the gateway doesn't provide `agent_id` to the device.
- **Spec ref:** §10: "Gateway → (control) instructs device to capture for the session's origin" — the device needs to know which agent's container to query for browser state.

---

## 2. Significant Gaps

### 2.1 `JOB_CANCEL` frame never sent over tunnel

- **File:** `gateway/internal/httpapi/jobs_handler.go:191-226`
- **Severity:** Significant
- **What's wrong:** `handleCancelJob` updates the DB status to `CANCELLED` and releases the agent, but never sends a `JOB_CANCEL` frame over the tunnel to the device. Per spec §3.3, cancel flows as: "Gateway → JOB_CANCEL frame → Device → agent /cancel". Without this, the device and agent continue running a job that the user has cancelled. The device handler `handle_job_cancel` is registered in `__main__.py:136`, but it will never be called.
- **Spec ref:** §3.3: "Cancel: Web → POST /jobs/{job_id}/cancel → Gateway → JOB_CANCEL frame → Device → agent /cancel"; §4.2: `JOB_CANCEL` type table entry.

### 2.2 `JOB_DISPATCH` payload missing required fields

- **File:** `gateway/internal/pool/allocator.go:179-196`
- **Severity:** Significant
- **What's wrong:** The `dispatchJob` function constructs a `JobDispatchPayload` without `FileIDs`, `CredentialIDs`, `SubmittAt`, or `Params` — all are required or conditionally useful per the spec. The `Params` field (`json.RawMessage`) could carry structured parameters the agent needs. `SubmittAt` is explicitly listed in the spec payload. Even though `FileIDs` and `CredentialIDs` are sent separately (`PushFilesToDevice` and implicit credential push in `jobs_handler.go`), the spec says they should be carried on the `JOB_DISPATCH` frame itself so the device can correlate.
- **Spec ref:** §4.2: `JOB_DISPATCH: { job_id, agent_id, command, params, file_ids:[], skill_id?, credential_ids:[]?, submitted_at }`

### 2.3 `NewFrame()` does not set `ts` — all G→D frames have timestamp 0

- **File:** `gateway/internal/tunnel/codec.go:57-68`
- **Severity:** Significant
- **What's wrong:** `NewFrame()` creates frames without setting the `TS` field (defaults to 0). This function is used for every outbound gateway frame: `JOB_DISPATCH`, `VNC_OPEN`, `VNC_CLOSE`, `CRED_PUSH`, `CRED_CAPTURE`, `FILE_PUSH_BEGIN`, `FILE_CHUNK`, `FILE_PUSH_END`, `SKILL_DISPATCH_BEGIN`, `SKILL_CHUNK`, `SKILL_DISPATCH_END`, `SKILL_ACTION`, `SKILL_SYNC`, `AGENT_CREATE`. All these frames arrive at the device with `"ts": 0`. The spec envelope (§2) defines `ts` as "epoch millis (sender clock)". A `NewFrameWithTS` variant exists but is unused in production code.
- **Spec ref:** §2 envelope: `"ts": 1730000000123, // epoch millis (sender clock)`

### 2.4 HELLO frame not ACKed by gateway

- **File:** `gateway/internal/tunnel/device_conn.go:153-165,238-381`
- **Severity:** Significant
- **What's wrong:** The gateway handles HELLO in a special path in `StartReadPump` (lines 153-159), but then passes the frame to `handleFrame()` (line 162) where it falls through to the `default` case (line 377-378) — there is no `case model.FrameHello:` in the switch. Per spec §2, "Every non-ACK frame MUST be ACKed by the receiver." The gateway never sends an ACK for the HELLO frame, so the device's `_pending_acks` entry for its HELLO is never cleared. After the 1s/2s/4s backoff cycle (max 3 retries), the HELLO send future resolves as `TimeoutError`. The device's `send_with_ack` is not used for HELLO (it's sent via `_send`), but the principle violation means any future use of tracked HELLO sends would break.
- **Spec ref:** §2: "Every non-ACK frame MUST be ACKed by the receiver."

### 2.5 `AGENT_CREATE` payload missing required fields

- **File:** `gateway/internal/pool/allocator.go:225-228`
- **Severity:** Significant
- **What's wrong:** The `AGENT_CREATE` frame payload only includes `AgentID` and `AgentName` (the latter is not even a spec field). The spec requires `{ agent_id, image, tags, limits:{cpu, mem_mb, disk_mb}, env? }`. Missing `image` means the device doesn't know which Docker image to pull; missing `limits` means no resource constraints are specified for the container. The device handler (`_handle_agent_create` in `__main__.py:187-193`) ignores the spec payload entirely and just calls `docker_mgr.ensure_pool(count)`.
- **Spec ref:** §4.4: `AGENT_CREATE: { agent_id, image, tags, limits:{cpu, mem_mb, disk_mb}, env? }`

### 2.6 `SKILL_DISPATCH_ACK` and `FILE_PURGED` frame types never handled

- **File:** `gateway/internal/tunnel/device_conn.go:238-381` and `gateway/internal/tunnel/router.go:43-131`
- **Severity:** Significant
- **What's wrong:** Both frame type constants are defined in `types.go:563-564`, but neither appears in the gateway's `handleFrame` switch statement nor in `Router.RegisterAll`. When the device sends `SKILL_DISPATCH_ACK` (per spec §4.6: device acknowledges skill package receipt) or `FILE_PURGED` (per spec §4.7: device confirms file cleanup), the gateway silently ignores these frames. This means the gateway has no visibility into skill package delivery status or file cleanup completion.
- **Spec ref:** §4.6: `SKILL_DISPATCH_ACK (D→G): { skill_id, version, status:"CACHED"|"ERROR", message? }`; §4.7: `FILE_PURGED (D→G): { file_id, job_id }`

### 2.7 `VNC_CLOSE` from device not handled by gateway read pump

- **File:** `gateway/internal/tunnel/device_conn.go:238-381`
- **Severity:** Significant
- **What's wrong:** The spec marks `VNC_CLOSE` as "both" directions. The device sends `VNC_CLOSE` when the VNC bridge ends (e.g., TTL expiry, bridge error — see `bridge.py:121-124`). But the gateway's `handleFrame()` has no `case model.FrameVNCClose:`, so the gateway never learns that the device closed a VNC session. The gateway's VNC relay (`vncrelay/relay.go`) has its own `CloseSession` but never gets called with the device-side close notification.
- **Spec ref:** §4.8: `VNC_CLOSE | both | { session_id, reason } — either side may close`

### 2.8 `VNC_OPEN` payload missing `agent_id` and `job_id`

- **File:** `gateway/internal/httpapi/vnc_handler.go:51-56`
- **Severity:** Significant
- **What's wrong:** The `VNCOpenPayload` struct in `types.go:834-841` defines `AgentID` and `JobID` fields (matching spec §4.8), but the handler at `vnc_handler.go:51-56` only populates `SessionID`, `RelayURL`, `SessionToken`, and `TTLSecs` — omitting `AgentID` and `JobID`. The device's `handle_vnc_open` reads `agent_id` from payload (`bridge.py:38`) and uses it to route to the correct Docker container. Without it, the device can't start VNC for the right agent.
- **Spec ref:** §4.8: `VNC_OPEN: { session_id, agent_id, job_id, relay_url, session_token, ttl_s }`

---

## 3. Minor Gaps

### 3.1 `HELLO` payload missing `agent_count` field

- **File:** `device/iagent_device/tunnel/client.py:112-118`
- **Severity:** Minor
- **What's wrong:** The Go `HelloPayload` struct (`types.go:571-578`) has `AgentCount int` field (no `omitempty`), but the device's `_send_hello_and_sync` never sets it. The field will be 0, which the gateway logs (`device_conn.go:175: "agents", payload.AgentCount`). This is cosmetic but the field exists in the model for a reason.
- **Spec ref:** §4.1: HELLO payload includes agent count implicitly via `agents:[...]` array; the Go struct adds an explicit count.

### 3.2 `STATE_SYNC` sends empty jobs list on reconnect

- **File:** `device/iagent_device/tunnel/client.py:130`
- **Severity:** Minor
- **What's wrong:** On reconnect, the device sends `STATE_SYNC` with hardcoded `"jobs": []`. Real in-flight jobs persisted in the outbox are flushed separately via `outbox.flush()` (line 103-105), but the `STATE_SYNC` snapshot should reflect actual in-flight job state per the spec. The Go model `StateSyncJob` has `JobID`, `AgentID`, `Status` fields that are never populated by the device.
- **Spec ref:** §4.1: `STATE_SYNC: { jobs:[{job_id, status, percent}], agents:[{agent_id, status}] }`; §7: "re-send HELLO, then STATE_SYNC, then flush buffered JOB_PROGRESS/JOB_RESULT frames"

### 3.3 `HELLO_ACK` does not reference HELLO `msg_id` via `ack_id`

- **File:** `gateway/internal/tunnel/device_conn.go:177-198`
- **Severity:** Minor
- **What's wrong:** The HELLO_ACK frame is constructed without setting `AckID` to reference the HELLO's `msg_id`. While there's no hard requirement that HELLO_ACK carry an `ack_id` (the spec only says "Every non-ACK frame MUST be ACKed", which means a separate ACK frame should be sent), the convention is that the HELLO is acknowledged. Currently the device ACKs the HELLO_ACK (because `_handle_frame` ACKs all non-ACK frames), but the HELLO itself is never acknowledged (see gap 2.4).
- **Spec ref:** §2: "Every non-ACK frame MUST be ACKed by the receiver (`type: "ACK"`, `ack_id` = original `msg_id`)."

### 3.4 Outbound frame `ts` is 0 for all gateway→device frames using `NewFrame()`

- **File:** `gateway/internal/tunnel/codec.go:57-68`
- **Severity:** Minor *(already counted in 2.3, this is a restatement for consolidation)*
- *(This gap is identical to 2.3 above — consolidated for completeness in the source-level audit.)*

---

## 4. What's Solidly Implemented

| Feature | Verification | Files |
|---------|-------------|-------|
| **Frame envelope** (v, type, msg_id, ack_id, ts, payload) | Correct JSON structure, 1 MiB max, version validation | `codec.go:13-54`, `codec.py:78-102`, `types.go:517-524` |
| **WebSocket subprotocol** (`iagent.tunnel.v1`) | Negotiated on both sides | `tunnel_handler.go:22`, `client.py:97` |
| **WebSocket ping/pong** (secondary liveness) | WS `PingMessage` every 15s + `SetPongHandler` | `device_conn.go:92-95,206-217` |
| **HELLO timeout** | Gateway closes with `4003` after 10s | `device_conn.go:97-114` |
| **Supersede** | New connection for same device closes old with `4002` | `hub.go:151-167` |
| **All close codes** | 4001 (heartbeat), 4002 (supersede), 4003 (HELLO timeout), 4004 (protocol violation), 4005 (token revoked), 4290 (rate limited) | `device_conn.go:107,130,137,143,149,502`, `hub.go:166,341` |
| **Heartbeat timeout threshold** | 45s (3×15s) via `HeartbeatInterval * 3` | `config.go:62`, `hub.go:327-343` |
| **Application PING/PONG** | Device sends PING every 15s (configurable from HELLO_ACK); gateway replies PONG | `client.py:191-197`, `device_conn.go:383-398` |
| **ACK retransmission (device→gateway side)** | Exponential backoff 1s/2s/4s, max 3 retries, device retransmit loop | `client.py:231-260` |
| **ACK retransmission (gateway→device side)** | `AckTracker` fully implemented — but never fed frames (see gap 1.3) | `device_conn.go:509-632` |
| **Idempotency** | `sync.Map` on gateway, `set` on device; ACK/PING/PONG exempt | `device_conn.go:239-246`, `client.py:148-152` |
| **Rate limiting** | Per-connection 100 fps cap, close with 4290 | `device_conn.go:466-480` |
| **Token revocation watcher** | Periodic 30s check, close with 4005 | `device_conn.go:485-507` |
| **Reconnect with backoff** | `min(30s, 1s * 2^attempt) + jitter ±20%`, `max_reconnect_attempts` | `client.py:88-91` |
| **Outbox durability** | SQLite-backed, flushed on reconnect, JOB_RESULT persisted until ACK | `outbox.py`, `client.py:103-105` |
| **HELLO_ACK config application** | Device reads `heartbeat_s` from HELLO_ACK `config` | `client.py:171-178` |
| **File relay (gateway side)** | `FILE_PUSH_BEGIN/CHUNK/END` with 256 KiB chunks, base64, ≤8 in flight, SHA-256 integrity | `relay/relay.go:103-203` |
| **Skill dispatch (gateway side)** | `SKILL_DISPATCH_BEGIN/CHUNK/END` with 256 KiB chunks, base64, SHA-256 | `skillvault/dispatch.go:48-181` |
| **VNC relay manager** | Session lifecycle, binary byte pump, TTL reaper, concurrency cap | `vncrelay/relay.go` |
| **All 36 frame type constants** | Defined in both Go (`types.go`) and Python (`codec.py`) | `types.go:528-565,820-830`, `codec.py:16-71` |
| **Tunnel WS endpoint** | `GET /tunnel` with device token auth, SHA-256 token hash | `tunnel_handler.go:25-85` |
| **Device-side frame handlers** | All 16 inbound types registered in `__main__.py:134-153` | `__main__.py` |
| **Device VNC bridge** | Second socket dial, TCP↔WS relay, TTL enforcement | `vncbridge/bridge.py` |
| **Device credential relay** | SHA-256 verification, agent browser state pass-through | `creds/relay.py` |
| **Device file stager** | Receives chunks, verifies SHA-256, stages in workspace | `files/stager.py` (not in-scope per dev record) |

---

## 5. Fix Priority

| Priority | Gap | Impact |
|----------|-----|--------|
| **P0** | 1.1 — `CRED_PUSH` field name (`data` vs `storage_state`) | Credential injection completely non-functional. Fix: change Go `json:"data"` to `json:"storage_state"` in `CredPushPayload`. |
| **P0** | 1.4 — VNC session relay auth mismatch (`Authorization` vs `X-Session-Token`) | VNC binary relay completely non-functional. Fix: add `Authorization: Bearer <token>` check in `handleVNCDeviceSocket`, or send token as query param in `relay_url`. |
| **P0** | 1.3 — `Hub.SendFrame` never tracks ACKs | All G→D frames (JOB_DISPATCH, VNC_OPEN, CRED_PUSH, etc.) are fire-and-forget with no retransmission. Fix: call `conn.acks.Track(frame)` inside `Hub.SendFrame` for non-ACK frames. |
| **P0** | 1.2 — Gateway handles `CRED_CAPTURE_ACK` instead of `CRED_CAPTURE` in read pump | Credential capture flow completely broken. Fix: swap `FrameCredCaptureAck` → `FrameCredCapture` in `device_conn.go:367` and `router.go:124`. Add `FrameCredCaptureAck` handling on the device side (or send from a write path, not read pump). |
| **P0** | 1.5 — `CRED_CAPTURE` instruction missing `agent_id` | Even with fix for 1.2, device can't route capture to correct agent. Fix: include `AgentID` (from session) in `CredCapturePayload` when sending from `vnc_handler.go`. |
| **P1** | 2.8 — `VNC_OPEN` missing `agent_id`, `job_id` | Device's VNC handler can't route to the right container. Fix: populate `AgentID` and `JobID` in `vnc_handler.go:51-56`. |
| **P1** | 2.1 — `JOB_CANCEL` never sent over tunnel | Running jobs not cancelled on device. Fix: send `JOB_CANCEL` frame in `handleCancelJob` before releasing agent. |
| **P1** | 2.3 — `NewFrame()` doesn't set `ts` | All outbound frames have timestamp 0. Fix: add `TS: time.Now().UnixMilli()` to `NewFrame()`. |
| **P1** | 2.4 — HELLO not ACKed by gateway | Device retransmits HELLO unnecessarily. Fix: add `case model.FrameHello:` to `handleFrame()` switch to send ACK. |
| **P1** | 2.7 — `VNC_CLOSE` from device not handled | Gateway never learns of device-side VNC session closures. Fix: add `case model.FrameVNCClose:` to `handleFrame()` and wire to `VNCRelay.CloseSession`. |
| **P2** | 2.2 — `JOB_DISPATCH` missing fields | `params`, `submitted_at`, `file_ids`, `credential_ids` not populated. Fix: populate all fields from job model in `dispatchJob`. |
| **P2** | 2.5 — `AGENT_CREATE` missing fields | Missing `image`, `tags`, `limits`. Fix: populate from agent model / config. |
| **P2** | 2.6 — `SKILL_DISPATCH_ACK` / `FILE_PURGED` not handled | Gateway has no visibility into skill delivery or file cleanup. Fix: add handlers in `device_conn.go` and `router.go`. |

