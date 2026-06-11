# Diagnosis Runbook: Job stuck at "dispatched" + Agent stuck "busy"

Date: 2026-06-11
Status: RESOLVED — root cause identified, cloud-side fixes applied. Device-side investigation pending.
Audience: on-server agent (keep steps literal; copy-paste commands; do not refactor code)

---

## 1. Symptom (what the user reports)

- A job submitted from the cloud UI stays at status `dispatched` forever.
- The agent that ran it stays `busy` forever (never returns to `idle`), so the pool
  looks full even though nothing is running.
- On the **local device**, the same job has already **finished** (the agent container
  reached a terminal state and the device released the agent locally).
- Net effect: cloud DB and device DB disagree. Cloud is stale.

---

## 2. How the status is SUPPOSED to flow (happy path)

```
[cloud] dispatch job  -> jobs.status = 'dispatched', agents.status = 'busy'
        | JOB_DISPATCH frame (cloud -> device)
        v
[device] handle_job_dispatch():
        1. send JOB_ACCEPTED        -> cloud sets jobs.status = 'running'
        2. send JOB_PROGRESS running
        3. poll agent until terminal
        4. send JOB_RESULT          -> cloud sets jobs.status = 'succeeded'/'failed'
                                       AND releases agent (agents.status = 'idle')
        5. send AGENT_STATUS idle    -> cloud sets agents.status = 'idle' (belt + braces)
```

If the cloud is stuck at `dispatched`, then **JOB_ACCEPTED never took effect** — i.e.
NONE of the device->cloud status frames after JOB_DISPATCH were applied.

---

## 3. What has already been examined (code is correct on paper)

All file:line references are in this repo.

| Area | File:line | Verdict |
|------|-----------|---------|
| Device emits JOB_ACCEPTED | `device/iagent_device/jobs/dispatcher.py:74` | OK |
| Device emits JOB_PROGRESS running | `dispatcher.py:92` | OK |
| Device emits JOB_RESULT (success/fail) | `dispatcher.py:198`, `dispatcher.py:204` | OK |
| Device releases agent + emits AGENT_STATUS idle | `dispatcher.py:119-127` (in `finally`) | OK |
| Outbox send path | `device/iagent_device/tunnel/outbox.py:21-26` | OK (durable, sends via `send_fn`) |
| Outbox wired to tunnel send | `device/iagent_device/__main__.py:186` (`outbox.send_fn = _tunnel._send`) | OK |
| Tunnel `_send` actually transmits | `device/iagent_device/tunnel/client.py:206-210` (`await self._ws.send`) | OK |
| Device handles inbound ACK -> `outbox.ack` | `client.py:161-169` | OK |
| Gateway read loop -> handleFrame | `gateway/internal/tunnel/device_conn.go:159` | OK |
| Gateway routes JOB_ACCEPTED | `device_conn.go:305-313` -> `hub.HandleJobAccepted` | OK |
| Gateway routes JOB_RESULT | `device_conn.go:295-303` -> `hub.HandleJobResult` | OK |
| Gateway routes AGENT_STATUS | `device_conn.go:325-333` -> `hub.HandleAgentStatus` | OK |
| Callback: JOB_ACCEPTED -> running | `gateway/cmd/gateway/main.go:188-190` | OK |
| Callback: JOB_RESULT -> terminal + release agent | `main.go:171-187` | OK |
| Callback: AGENT_STATUS -> agents.UpdateStatus | `main.go:219-221` | OK |
| Store writes (no state-machine guard) | `gateway/internal/store/jobs.go:49-78`, `gateway/internal/store/agents.go:72-86` | OK (unconditional UPDATE) |
| Frame type names match | device `codec.py:30-39` vs gateway `types.go:591-599` | OK (identical strings) |

Conclusion: the bug is most likely **runtime / environmental**, not a logic typo.

---

## 4. Leading hypotheses (check in this order)

- **H1 — Device never received JOB_DISPATCH for THIS job.**
  "Local finished" may refer to a *different/earlier* job. If JOB_DISPATCH never
  arrived, the device never sends JOB_ACCEPTED, so cloud stays `dispatched`.

- **H2 — Gateway handler returns an error but the frame is still ACKed.**
  In `device_conn.go:259` the ACK is sent via `defer c.sendAck(frame.MsgID)`, which
  runs EVEN IF the handler returns an error (e.g. DB write failed, payload unmarshal
  failed). The device then marks the frame delivered (`outbox.ack`) and never retries.
  Result: status update silently lost. **Look for gateway error logs around the
  handler.**

- **H3 — Agent never reached a terminal status on the device.**
  `_poll_progress` (`dispatcher.py:129`) only sends JOB_RESULT when the agent reports
  `succeeded/failed/cancelled`. If the agent stays `running`, JOB_RESULT is never sent
  (until the 3600s timeout). Check what the agent actually reported.

- **H4 — Tunnel reconnect dropped frames / device offline when it sent them.**
  If the WSS tunnel was down when the device emitted frames, delivery depends on
  outbox `flush` on reconnect. Check device disconnect/reconnect logs and whether
  `outbox` rows for this job were ever acked.

- **H5 — job_id / agent_id mismatch.**
  `model.UUID = uuid.UUID` (`types.go:13`). If the device sends a job_id/agent_id that
  is not a valid UUID, the gateway `json.Unmarshal` fails -> handler returns error ->
  ACK still sent (see H2) -> update lost. Check the actual id strings in frames.

---

## 5. Step-by-step checklist (run on the cloud server)

> Fill in the real values where you see `<JOB_ID>`, `<AGENT_ID>`, `<DEVICE_ID>`.
> Get `<JOB_ID>` from the stuck job in the UI (or from Step 5.1).

### Step 5.1 — Inspect the stuck job + agent in PostgreSQL

Production DB is `iagent`. Connect:

```bash
psql "postgresql://iagent:<PASSWORD>@localhost:5432/iagent?sslmode=disable"
```

Then run:

```sql
-- A) Find recently stuck jobs
SELECT id, status, agent_id, device_id, created_at, started_at, finished_at, updated_at
FROM jobs
WHERE status = 'dispatched'
ORDER BY updated_at DESC
LIMIT 20;

-- B) Inspect the specific job (use the id from A)
SELECT id, status, agent_id, device_id, percent, progress_message,
       created_at, started_at, finished_at, updated_at, result
FROM jobs
WHERE id = '<JOB_ID>';

-- C) Inspect the agent the job was on
SELECT id, status, user_id, job_id, allocated_at, updated_at
FROM agents
WHERE id = '<AGENT_ID>';
```

Record:
- `jobs.status`, `jobs.updated_at`, `jobs.started_at`, `jobs.finished_at`
- `agents.status`, `agents.allocated_at`, `agents.updated_at`
- Note whether `updated_at` advanced AT ALL after dispatch (if it never moved past
  `started_at`, the cloud received zero device frames for this job -> points to H1/H4).

### Step 5.2 — Search the GATEWAY logs for this job

Locate the gateway log (systemd or docker). Examples:

```bash
# if systemd
journalctl -u iagent-gateway --since "1 hour ago" --no-pager > /tmp/gw.log
# if docker compose
docker compose logs gateway --since 1h > /tmp/gw.log
```

Then grep (use ripgrep `rg`, or `grep` if rg missing):

```bash
# 1. Did the gateway DISPATCH this job to the device?
rg -n "<JOB_ID>" /tmp/gw.log

# 2. Any frame handler errors? (H2 / H5) — these are the smoking gun
rg -n "read error|decode error|frame too large|handle|error" /tmp/gw.log | rg -i "job|agent|frame|handler"

# 3. Did the device disconnect/reconnect around that time? (H4)
rg -n "HELLO|disconnect|read error|write error|close|4003|4004" /tmp/gw.log
```

What to record:
- Was JOB_DISPATCH for `<JOB_ID>` actually sent? (line present or not)
- Any error line mentioning the job/agent id or "handler"/"UpdateStatus"/"UpdateResult"?
- Any disconnect/reconnect within the job's lifetime window?

### Step 5.3 — Search the DEVICE logs for this job

On the local device machine, locate the device log (the user can run this part).

```bash
# adjust path to wherever the device logs go
rg -n "<JOB_ID>" device.log
```

Confirm, in order, that the device logged:
1. Received JOB_DISPATCH for `<JOB_ID>`            (H1)
2. "Job started" / created job on the agent        (`dispatcher.py:90-98`)
3. Polled the agent to a terminal status           (H3 — what status? succeeded/failed?)
4. Sent JOB_RESULT                                  (`dispatcher.py:198/204`)
5. Released the agent + sent AGENT_STATUS idle      (`dispatcher.py:119-127`)

Also grep for outbox/send failures:

```bash
rg -n "outbox flush failed|frame handling error|poll error|pull_outputs" device.log
```

### Step 5.4 — Check the device outbox (SQLite) for un-acked frames (H2/H4)

On the device, open the device SQLite DB (WAL mode). Find the outbox table:

```bash
sqlite3 <device_db_path>.sqlite
```

```sql
.tables
-- find the outbox table name, then:
SELECT msg_id, type, acked, created_at
FROM outbox
ORDER BY created_at DESC
LIMIT 50;
```

What to record:
- Are there `JOB_ACCEPTED` / `JOB_RESULT` / `AGENT_STATUS` rows that are NOT acked?
  - NOT acked  => cloud never ACKed => frame likely never delivered (H4) OR cloud
    errored before ACK. Cross-check with Step 5.2.
  - acked but cloud status still `dispatched` => cloud ACKed but the DB write failed
    (H2/H5). This is the most important finding — it means the handler errored.

---

## 6. Decision table (map findings -> root cause)

| Finding | Root cause | Fix direction |
|---------|-----------|---------------|
| No JOB_DISPATCH for `<JOB_ID>` in gateway log | H1: job not actually dispatched / different job finished | Re-check which job the device ran; verify allocator dispatched this one |
| Device log has NO "received JOB_DISPATCH" | H1/H4: frame never arrived | Inspect tunnel connectivity at dispatch time |
| Device reached terminal + outbox rows ACKED, but cloud still `dispatched` | H2/H5: gateway handler errored AFTER frame received, ACK sent anyway | Fix gateway handler error; STOP ACKing on handler error (see §7) |
| Outbox rows NOT acked + device saw a disconnect | H4: frames lost on a dead socket, flush didn't re-deliver | Inspect reconnect/flush; verify `outbox.flush()` runs on reconnect |
| Agent status on device never went terminal | H3: agent stuck running | Inspect agent container / opencode run |
| job_id / agent_id in frames is not a valid UUID | H5: unmarshal fails on gateway | Fix id propagation |

---

## 7. Strong candidate code fix (only after evidence confirms H2/H5)

In `gateway/internal/tunnel/device_conn.go:256-260` the ACK is deferred and fires
regardless of handler outcome:

```go
if frame.Type != model.FrameAck && frame.Type != model.FramePing &&
    frame.Type != model.FramePong && frame.Type != model.FrameHello {
    defer c.sendAck(frame.MsgID)   // <-- runs even if handleFrame returns an error
}
```

This means a transient DB error (or any handler error) causes the device to believe
the frame was delivered, drop it from the outbox, and NEVER retry — leaving the cloud
permanently stale. The intended at-least-once contract is: **ACK only after the handler
succeeds**. Candidate change: send the ACK explicitly at the END of successful handling
(not via `defer`), or skip the ACK when the handler returns an error so the device
retransmits.

> Do NOT apply this yet. First confirm via Step 5.2 (gateway error logs) + Step 5.4
> (acked-but-stale outbox rows) that a handler error actually occurred. If the evidence
> points to H1/H3/H4 instead, this change is irrelevant.

---

## 8. What to report back

Please send back:
1. Step 5.1 query results (jobs row + agents row).
2. Step 5.2 grep hits for `<JOB_ID>` and any error/disconnect lines.
3. Step 5.3 device log lines (which of the 5 stages were logged).
4. Step 5.4 outbox rows for this job (type + acked flag).

With those four pieces of evidence the root cause maps directly via the §6 table.

---

## 9. Investigation Results (2026-06-11)

### 9.1 Cloud-side evidence (Steps 5.1 + 5.2)

**Database (Step 5.1):**

| Entity | ID | Status | Key detail |
|--------|-----|--------|------------|
| Job | `019eb662-d20b-7e4e-9eb1-8e73ee63aabd` | `dispatched` | created_at = updated_at = 19:13:10 (never moved) |
| Agent | `019eb249-cb8b-7a8b-85dc-18194efb2525` | `busy` | **job_id = NULL**, allocated_at = 19:13:10 |
| Device | `019ea7ff-4976-76e6-8086-638dbebbde4b` | `online` | last_seen_at = **11:49:47** (7.5 hours before dispatch) |

**Gateway log (Step 5.2):**
- Zero tunnel frame activity since 11:49:47. No JOB_DISPATCH, JOB_ACCEPTED, JOB_RESULT, AGENT_STATUS, JOB_PROGRESS.
- No "read error", "write error", "device connection closed", or "device unregistered" entries.
- Device registered at 11:49:47 (HELLO with agents=0). Gateway was restarted at ~11:49:51 (old instance died). No subsequent device reconnection.
- Job ID only appears in HTTP GET polls from the UI.

### 9.2 Root cause

**H1 confirmed: Device never received JOB_DISPATCH.**

The device's WSS connection died after 11:49:47 (likely due to a gateway restart). The device never reconnected. However:

1. The gorilla WebSocket `ReadMessage` and `WriteMessage` calls have **no timeouts** configured. Dead TCP connections were never detected — the read pump stayed blocked on `ReadMessage`, and the write pump stayed blocked on `WriteMessage`.

2. The hub's `h.devices` map still held a stale `DeviceConn` entry (no "device unregistered" log confirms it was never removed).

3. When the job was submitted at 19:13:10, `SendFrame` found the stale device entry, accepted the JOB_DISPATCH frame into the outbound channel (which had spare capacity), and returned nil. The frame was consumed by the write pump but never delivered because the WSS was dead.

4. The liveness checker only touches an in-memory registry — it does not remove stale `DeviceConn` objects from `h.devices`, nor does it update the PostgreSQL `devices.status` to `offline`.

**Secondary bug found:** `agent_store.Allocate()` accepted a `jobID` parameter but never set it in the SQL UPDATE. Agents were left with `job_id=NULL` after allocation.

### 9.3 Cloud-side fixes applied

**Fix A — `gateway/internal/store/agents.go:63-70`**: Added missing `job_id=$4` to the `Allocate` SQL UPDATE. The `jobID` parameter was accepted but never written.

**Fix B — `gateway/internal/tunnel/device_conn.go`**: Added WebSocket read/write timeouts:
- Read pump: `SetReadDeadline(30s)` before each `ReadMessage()`. Dead connections are detected within 30s instead of hanging until TCP keepalive (2+ hours).
- Write pump: `SetWriteDeadline(10s)` before each `WriteMessage()`. Failed writes return within 10s.
- Ping failure path now logs `"write error on ping"` instead of silently returning.

**Fix C — `gateway/internal/httpapi/jobs_handler.go:293`**: Nil guard on `PushFilesToDevice` for tests that don't wire up a `FileRelay`.

---

## 10. Device-side recommendations

Steps 5.3 and 5.4 were **not executed** (requires access to the device machine). Recommended actions on the device:

### 10.1 Verify reconnect behavior

The device lost its tunnel connection when the gateway was restarted at ~11:49:51 and apparently never reconnected. Check:

```bash
# On the device machine, search for reconnect attempts in device logs
grep -n "reconnect\|disconnect\|closed\|connection" device.log
```

The reconnect loop should have triggered within the backoff window. If there are no reconnect attempts, the device may have crashed or the reconnect logic may have a bug.

### 10.2 Check outbox state

If the device DID reconnect at some point (to a later gateway instance), the outbox flush should have re-sent pending frames. But the job was submitted at 19:13:10 — long after the last known connection. Check:

```bash
sqlite3 <device_db>.sqlite "SELECT msg_id, type, acked, created_at FROM outbox ORDER BY created_at DESC LIMIT 50;"
```

Look for un-acked `JOB_ACCEPTED` or `JOB_RESULT` frames that might correspond to a DIFFERENT job the device ran between 11:49 and 19:13.

### 10.3 Device reconnect on gateway restart

When the gateway restarts, all device connections are closed via `CloseAll()` (close code 1001). The device should detect this, enter its reconnect backoff, and eventually reconnect. Verify:

1. The device's `TunnelClient` properly handles WS close codes 1001/4001/4002 and triggers a reconnect.
2. On reconnect, `send_hello` re-registers the device's agent pool via `AGENT_STATUS` frames.
3. If the reconnect loop is stuck or has an unrecoverable error path, add logging and recovery.

