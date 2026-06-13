# Tunnel Instability & Job Lifecycle Bugs — Investigation & Fixes

Date: 2026-06-13
Status: TUNNEL STABILIZED — connection survives >2min (was 30s cycle); job lifecycle resilience improved with re-dispatch on reconnect and pending-frame migration

---

## Phase 2 Findings & Fixes (17:27 CST)

After the Phase 1 fixes were deployed, the tunnel continued cycling every 30s (ping failures → close → reconnect in 4s → repeat). Three root-cause gaps were identified on the gateway side that compounded the device-side TCP instability:

### 9. `sendAck` writes without deadline — cascading write failures

**Symptom**: ACK write errors started at T+10s after every connection, flooding the log with `ack write error: i/o timeout`. The read pump kept processing frames from the device (read direction was alive) but all ACK responses were lost because the write direction was dead.

**Root cause**: `sendAck` in `device_conn.go` wrote to the WebSocket without setting a write deadline. It used whatever deadline the write pump had previously set. When the write pump was idle (outbound channel empty), the old expired deadline caused immediate "i/o timeout" failures. When no deadline was set at all, the write could block indefinitely on a half-dead TCP connection.

**Fix**: Set `c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))` in both `sendAck` and `sendPong` before writing. This ensures:
- ACK writes fail fast (5s) instead of blocking or cascading
- The read pump goroutine isn't held up by dead connections
- A new write deadline supersedes any expired deadline from the write pump

**Files**: `gateway/internal/tunnel/device_conn.go:522,501`

### 10. Dispatched jobs lost on tunnel drop — no re-dispatch on reconnect

**Symptom**: When a JOB_DISPATCH frame was sent just before a tunnel drop, the job stayed in "dispatched" status forever. The device never received it (frame lost in dead outbound channel), and on reconnect, nothing triggered a re-send. The job would only resolve after the 5-minute `ExpireDispatched` timeout → "failed".

**Root cause**: When the tunnel drops, the old connection's outbound channel has no consumer (write pump exited). The tracked frames on the old connection's `AckTracker` are orphaned. On reconnect:
- `ReconcilePool` triggers `dequeueNext` — but this only picks up "queued" jobs, not "dispatched" ones
- `STATE_SYNC` has a 2-minute grace period before failing dispatched jobs, but no mechanism to re-deliver them

**Fix**: Added `redispatchLostJobs` called from `ReconcilePool` on every device HELLO. It finds all jobs in "dispatched" status assigned to this device's agents that have been in that state for >15 seconds (enough time for a tunnel drop + reconnect cycle), and re-sends `JOB_DISPATCH` (with credentials) on the new connection.

**Files**: `gateway/internal/pool/allocator.go:372-417`

### 11. Pending tracked frames lost when connection superseded

**Symptom**: When the device reconnects, the old connection is closed (`Close(4002, "superseded")`) and a new connection is created. Any frames tracked on the old connection's `AckTracker` (including JOB_DISPATCH and CRED_PUSH) were silently discarded.

**Root cause**: `Hub.Register()` instantly closed the old connection on supersede without migrating its pending tracked frames. The retransmitter goroutine on the old connection exits when `done` is closed, and the frames are never delivered.

**Fix**: Before closing the old connection, `Hub.Register()` now drains pending tracked frames from the old connection's `AckTracker` and queues them to the new connection's outbound channel + `AckTracker`. Logs `"migrating pending frames to new connection"` with the frame count.

**Files**: `gateway/internal/tunnel/hub.go:203-223`

### Result

After these three fixes, the tunnel connection has been stable for over 2 minutes (previously cycled every 30 seconds). Zero `ack write error` or `write failed on ping` log entries since deployment.

This is likely because the `sendAck` deadline fix prevents the TCP write direction from entering a broken state. Previously, ACK writes without a deadline could exhaust the gorilla/websocket or OS-level write buffer, causing permanent write-direction death. Now, each write sets its own deadline and the connection state is properly managed.

---

## Overview

During this session, we investigated and fixed multiple cascading bugs in the cloud gateway's job dispatch, queuing, and tunnel connection handling. The root cause of most failures is an **unstable device-to-nginx TCP connection** that drops after 15-45 seconds on every connection. While the tunnel instability itself is a device-side network issue, the gateway had several bugs that made job processing fragile in the face of these disconnects.

---

## Issues Found & Fixed

### 1. Nginx tunnel location missing from HTTPS server block

**Symptom**: Device kept getting SPA index.html (200, 411 bytes) instead of 101 WebSocket upgrade.

**Root cause**: The `/aiproduct/tunnel` location block existed only in the HTTP (port 80) server block, but the device connects via WSS (port 443 / HTTPS). The HTTPS server block had no tunnel proxy — requests fell through to the SPA catch-all `location /aiproduct/`.

**Fix**: Added tunnel location block to the HTTPS server block in `/etc/nginx/conf.d/aibi.conf` with `proxy_buffering off`, `proxy_send_timeout 86400s`, `proxy_read_timeout 86400s`.

**Files**: `/etc/nginx/conf.d/aibi.conf` (server-side, not in git)

---

### 2. Tunnel zombie connections after write direction dies

**Symptom**: Write pump would warn "write failed on ping, continuing" every 15s forever without ever closing the connection. Connection appeared alive (read pump still receiving HELLO/PING) but writes were dead — all frames silently lost.

**Root cause**: Commit `2c46f14` changed the write pump to `Warn` and continue on WS ping write failures instead of returning (which triggers `Close()`). This prevented transient ping failures from killing connections, but also prevented permanent write failures from being detected.

**Fix**: Added consecutive failure counter. After 2 consecutive WS ping write failures, the write pump exits and triggers connection close. This recycles dead connections in ~30s instead of letting them zombie forever.

**Commits**: `44f41b6`, `a06c1b9`, `e74a1dc`

---

### 3. `SendFrame` returns success after write pump is dead

**Symptom**: JOB_DISPATCH frame queued to outbound channel, `SendFrame` returned nil (success), but write pump was already exiting — frame silently lost. Allocator thought dispatch succeeded, job stayed "dispatched" with stale agent_id.

**Root cause**: `SendFrame` only checks if the device connection exists in the hub's device map. It doesn't check whether the connection's write pump is still alive. When the write pump exits, the outbound channel has no consumer.

**Fix**: Added `conn.closed.Load()` check in `SendFrame` before queuing. Returns error if the write pump has already exited, allowing the allocator to requeue.

**Commit**: `1623791`

---

### 4. Immediate job failure on dispatch when device is offline

**Symptom**: Jobs went "queued" → "failed" immediately when the device was offline during dispatch.

**Root cause**: Two places in the code immediately set `model.JobFailed` when `SendFrame` or `dispatchJob` returned an error:
- `allocator.go:dequeueNext` — dequeue path
- `jobs_handler.go:handleSubmitJob` — submit path

**Fix**: Changed both to set `model.JobQueued` instead of `model.JobFailed`, and call `ClearAgent` to remove stale agent binding. The allocator retries naturally when the device reconnects.

**Commits**: `5086d56`, `853dcee`

---

### 5. `queue_expires_at` never set — queue permanently broken

**Symptom**: Job stuck in "queued" status forever, never dispatched. Agent was idle, device was online. The dequeue loop would silently return nil.

**Root cause**: `DequeueNext` SQL query requires `queue_expires_at > NOW()`. This field was **never set** for any job — always NULL. Previous jobs only succeeded because they were dispatched immediately via `handleSubmitJob` (agent available at submit time, bypassing the queue). Any job that fell back to the queue was permanently stuck.

Places where status was set to "queued" without setting `queue_expires_at`:
1. `jobs_handler.go:92` — initial queuing when no agent available
2. `jobs_handler.go:145` — requeue after dispatch failure
3. `allocator.go:176` — requeue after dequeue dispatch failure

**Fix**: Modified `UpdateStatus` in `store/jobs.go` to set `queue_expires_at = NOW() + interval '1 hour'` whenever status changes to "queued".

**Commit**: `91067a2`

---

### 6. No dequeue trigger when device comes online

**Symptom**: After fixing `queue_expires_at`, the job still wouldn't dequeue. Device reconnected, ReconcilePool ran, but no dequeue attempt was made.

**Root cause**: `dequeueNext` was only triggered by `Release()` (when an agent is freed). If a device came online with an idle agent and a queued job, nothing triggered the dequeue. The expiry ticker only ran `expireStale`, not `dequeueNext`.

**Fix**: Added `go a.dequeueNext(context.Background())` at the end of `ReconcilePool` so that every device reconnect (HELLO) triggers a dequeue attempt.

**Commit**: `91067a2`

---

### 7. STATE_SYNC reconciliation fails recently-dispatched jobs

**Symptom**: Job went "dispatched" → "failed" within 9 seconds. The dispatch frame was queued to the outbound channel but the write pump died before sending it. The device reconnected, STATE_SYNC ran, and the job (with stale agent_id) was not in the device's reported job list → immediately failed.

**Root cause**: `STATE_SYNC` reconciliation had no grace period. Any active job not in the device's job list was failed immediately, even if dispatched 1 second ago.

**Fix**: Added 2-minute grace period. Jobs dispatched within the last 2 minutes are skipped by STATE_SYNC reconciliation. If the agent genuinely crashed, the 5-minute `ExpireDispatched` timeout will handle it.

**Commit**: `d28bdef`

---

### 8. FILE_PULL_BEGIN duplicate file entries

**Symptom**: Every output file appeared 4 times in the UI. Device retransmitted `FILE_PULL_BEGIN` with different `fileID` values.

**Root cause**: Device retransmitted `FILE_PULL_BEGIN` frames through the outbox with different fileID each time. The gateway created a new file record and `job_files` entry for each retransmission. No deduplication by (job_id, file_name).

**Fix**: Added deduplication check in `OnFilePullBegin`. If a file with the same (job_id, name) already exists, skip the transfer and send "RECEIVED" ACK immediately.

**Commit**: `a06c1b9`

---

## Unresolved: Device-side TCP disconnection

**Symptom**: Device's TCP connection to nginx drops consistently at 15-45 seconds after every WebSocket upgrade. The device's `websockets` library reports `> EOF` within 4ms of sending its T+30 WS keepalive ping.

**Evidence**:
```
15:57:52.728  101 Switching Protocols (connection established)
15:57:52.731  PING sent (T+0, JSON data frame)
15:58:07.732  PING sent (T+15, JSON data frame)  
15:58:22.732  % sent keepalive ping  ← websockets library WS ping
15:58:22.736  > EOF                   ← remote side closes TCP (4ms!)
```

**Gateway log pattern**:
```
T+15s: write failed on ping, continuing (consecutive=1)
T+30s: write failed on ping, continuing (consecutive=2) → connection closed
```

**Analysis**: The device's TCP connection to nginx drops, nginx closes the upstream to the gateway, the gateway's writes fail with "i/o timeout". The read direction works (HELLO/PING frames received) but the write direction is dead within 15-30s.

This is a **device-side network issue** — possible causes:
- Aggressive NAT/firewall timeout on the device's network
- Mobile carrier connection management
- Device process or network stack issue

The cloud-side mitigations (zombie detection, requeue, grace periods) make the system resilient to these drops, but cannot prevent them.

---

## Deployment Notes

### Gateway restart command (production)

```bash
fuser -k 42080/tcp; sleep 1
nohup env \
  IAGENT_CORS_ORIGINS='*' \
  IAGENT_CRED_KEY='...' \
  IAGENT_DB_URL='postgres://...' \
  IAGENT_ENV='development' \
  IAGENT_FILE_STORE='local:/tmp/iagent-files' \
  IAGENT_HTTP_ADDR=':42080' \
  IAGENT_JWT_SECRET='...' \
  IAGENT_LOG_FORMAT='text' \
  IAGENT_LOG_LEVEL='debug' \
  IAGENT_PATH_PREFIX='/aiproduct' \
  IAGENT_WEB_DIST_DIR='/root/projects/oneClickAgent/web/dist' \
  /root/projects/oneClickAgent/gateway/bin/gateway \
  > /tmp/gateway.log 2>&1 &
```

### Nginx tunnel config (manual, not in git)

Added to both HTTP and HTTPS server blocks:
```nginx
location /aiproduct/tunnel {
    proxy_pass http://127.0.0.1:42080/tunnel;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_read_timeout 86400s;
    proxy_send_timeout 86400s;
    proxy_buffering off;
}
```

### Database fix for stuck jobs

```sql
UPDATE jobs SET queue_expires_at = NOW() + interval '1 hour' 
WHERE status = 'queued' AND queue_expires_at IS NULL;
```

---

## Commits in this session

### Phase 1 (earlier)
```
d28bdef fix(gateway): add 2min grace period to STATE_SYNC reconciliation
91067a2 fix(gateway): set queue_expires_at when status changes to queued; trigger dequeue on reconnect
1623791 fix(gateway): reject SendFrame when write pump already exited
e74a1dc fix(gateway): lower zombie ping failure threshold from 3 to 2
a06c1b9 fix(gateway): close zombie tunnel after 3 ping failures; deduplicate FILE_PULL_BEGIN
44f41b6 fix(gateway): close zombie tunnel after 3 consecutive ping write failures
853dcee fix(gateway): clear agent assignment when requeuing after dispatch failure
5086d56 fix(gateway): requeue jobs instead of failing when device is offline during dispatch
```

### Phase 2 (17:27 CST — tunnel stabilization)
```
??????? fix(gateway): set write deadline in sendAck + sendPong to prevent cascading write failures
??????? fix(gateway): re-dispatch lost jobs on device HELLO after tunnel reconnect
??????? fix(gateway): migrate pending tracked frames from old to new connection on supersede
```
