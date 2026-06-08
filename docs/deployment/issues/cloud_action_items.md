# Cloud-Side Action Items — Verified from Local Testing

**Date:** 2026-06-08
**Status:** 7 of 11 resolved — cloud deployed fixes
**Companion:** `cloud-side.md` (auditor's comprehensive gap list)

## Cloud Deployed Fixes (2026-06-08)

| # | Issue | Status |
|---|-------|--------|
| 1.1 | `Result *string` → `*json.RawMessage` | ✅ Resolved |
| 1.2 | JOB_PROGRESS forces running | ✅ Resolved (inferred from wired handler list) |
| 1.3 | FILE_PULL_* dropped | ✅ Resolved (wired handlers) |
| 2.1 | Cancel misses agent_id | ✅ Resolved (`agent_id` now populated) |
| 2.3 | Wrong Command text | ✅ Resolved (per cloud confirmation) |
| 3.2 | CredCapture unwired | ✅ Resolved (wired handler) |
| 4.1 | No ACK frames | ✅ Resolved (ACKs now sent for all non-control frames) |
| — | 5-min JOB_DISPATCH timeout | ✅ Device sends JOB_PROGRESS{running} immediately |
| — | Failed dispatch rollback | ✅ No device change needed |

## Remaining Items

| # | Issue | Status |
|---|-------|--------|
| 2.2 | FILE_PUSH misses job_id (C9) | Open |
| 3.1 | CRED_PUSH_ACK status match | Open (cloud checks "INJECTED" now ✅) |
| 3.3 | ttl_s vs ttl_secs (C10) | Open (device reads both ✅) |
| 4.2 | StateSync wired (C3) | ✅ Resolved |
| 4.3 | Cloud outbox for device-bound frames | Open (see `agent_drain_sync_disconnect.md`)

## Priority 1: Job Never Reaches Terminal Status

### 1.1 JOB_RESULT `result` type mismatch (C1)

**File:** `gateway/internal/model/types.go:685`

The current code has `Result *string`, but the device sends `result` as a JSON object.
`json.Unmarshal` of an object into `*string` fails, successful results are rejected,
and the job stays `running` forever.

```go
// Current (broken):
Result *string `json:"result,omitempty"`

// Fix:
Result *json.RawMessage `json:"result,omitempty"`
```

**Evidence:** Every job completes on the agent side (callback shows `terminal event status=succeeded`),
but the cloud web UI never transitions from "running"/"waiting". Outbox shows `JOB_RESULT` was sent.

### 1.2 JOB_PROGRESS forces `running` (C4)

**File:** `gateway/internal/store/jobs.go:58-65` (`UpdateProgress`)

The SQL hard-codes `status=CASE WHEN $4='running' THEN $4 ELSE status END` with `$4` always
set to `model.JobRunning`. Terminal `JOB_PROGRESS` events from the device (e.g.,
`status: "succeeded"`) are ignored and the job regresses to `running`.

```sql
-- Current (broken):
UPDATE jobs SET status=CASE WHEN $4='running' THEN $4 ELSE status END

-- Fix:
-- Pass the device's actual status, only advance (never regress terminal):
UPDATE jobs SET status = CASE
  WHEN status IN ('succeeded','failed','cancelled') THEN status  -- don't regress
  WHEN $4 = 'succeeded' OR $4 = 'failed' OR $4 = 'cancelled' THEN $4
  ELSE 'running'
END
```

**Evidence:** Device sends `JOB_PROGRESS{status:"succeeded"}` via callback → cloud rewrites to `running`.

### 1.3 FILE_PULL_* frames dropped

**File:** `gateway/internal/tunnel/device_conn.go:255-419` (`handleFrame` switch)

`FILE_PULL_BEGIN`, `FILE_PULL_CHUNK`, `FILE_PULL_END` cases are missing from the switch.
Handlers exist (`hub.go`) and are wired in `main.go`, but are never called.

**Evidence:** Documented in `agent_output_files_cleanup.md`.

## Priority 2: Job Flow Completeness

### 2.1 JOB_CANCEL omits `agent_id` (C8)

**File:** `gateway/internal/httpapi/jobs_handler.go:243-250`

Cancel frame sends `{job_id, reason}` without `agent_id`. Device-side already fixed (looks up
agent_id locally from job record), but cloud should include it for belt-and-suspenders.

### 2.2 FILE_PUSH_BEGIN omits `job_id` (C9)

**File:** `gateway/internal/model/types.go:764-770`

`FilePushBeginPayload` has no `JobID` field. Input files staged to wrong path on device.

### 2.3 JOB_DISPATCH `Command` field is wrong

**File:** gateway `handleSubmitJob()`

Command field contains file paths and wrapper text instead of user's raw input.
Documented in `job_dispatch_wrong_command.md`.

## Priority 3: Credential & VNC

### 3.1 CRED_PUSH_ACK status mismatch (C6)

**File:** `gateway/cmd/gateway/main.go:186-192`

Cloud checks `payload.Status == "ok"` but device sends `"INJECTED"` (spec-compliant).
Every successful credential injection is logged as failure.

### 3.2 OnCredCapture not wired (C7)

**File:** `gateway/cmd/gateway/main.go:124-208`

Saved logins from VNC are silently dropped — `CRED_CAPTURE` frames never processed.

### 3.3 VNC_OPEN ttl_secs vs ttl_s (C10)

**File:** `gateway/internal/model/types.go:938-945`

Cloud sends `ttl_secs`, device reads `ttl_s`. Device-side already fixed (reads both).

## Priority 4: Robustness

### 4.1 No ACK frames sent to device

**File:** `gateway/internal/tunnel/device_conn.go`

The gateway never sends ACK frames. Device-side mitigated by self-acking outbox entries,
but proper ACK flow would enable retransmit and reliable delivery.

### 4.2 StateSync not wired (C3)

**File:** `gateway/cmd/gateway/main.go`

`OnStateSync` handler never set. Jobs orphaned after device restart are never cleaned up.

### 4.3 Cloud outbox needed for device-bound frames

Documented in `agent_drain_sync_disconnect.md`. Frames sent to disconnected devices are
lost forever (no outbound buffer on cloud side).

## Summary

| # | Issue | Doc Ref | Blocks |
|---|-------|---------|--------|
| 1.1 | `Result *string` → `*json.RawMessage` | C1 | Job completion |
| 1.2 | JOB_PROGRESS forces running | C4 | Job completion |
| 1.3 | FILE_PULL_* dropped | cleanup.md | Output files |
| 2.1 | Cancel misses agent_id | C8 | Job cancel |
| 2.2 | FILE_PUSH misses job_id | C9 | Input files |
| 2.3 | Wrong Command text | JDWCMD.md | Correct task |
| 3.1 | CRED_PUSH_ACK mismatch | C6 | Credentials |
| 3.2 | CredCapture unwired | C7 | Saved logins |
| 3.3 | ttl_s vs ttl_secs | C10 | VNC TTL |
| 4.1 | No ACK frames | — | Reliability |
| 4.2 | StateSync unwired | C3 | Reconnect |
| 4.3 | Cloud outbox missing | drain.md | Drain sync |

**For immediate job flow to work, fix 1.1 and 1.2 first.**
