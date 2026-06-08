# Cloud ↔ Device Communication Gaps — Cloud (Gateway) Side

**Date:** 2026-06-08
**Scope:** Gateway (Go) side of the reverse-tunnel contract with the local device.
**Method:** Code audit of `gateway/` against `docs/spec/05-tunnel-protocol.md`, cross-checked
against the device (`device/`) and agent (`agent/`) implementations.
**Companion doc:** `docs/deployment/issues/local-side.md` (device-side gaps).

> Most of these were found by comparing the exact JSON field names / types each side
> emits vs. consumes. The symptom "send a command and it gets stuck" is explained by a
> small cluster of these (C1, C2, C4 in particular).

## Severity legend

- **CRITICAL** — breaks the core job flow (jobs get stuck / never complete).
- **HIGH** — a major feature is broken or silently lost (creds, file inputs, reconnect recovery).
- **MEDIUM** — degraded / racy behavior, works in the happy path only.
- **LOW** — spec drift, robustness, resource leaks.

---

## Summary table

| ID | Sev | Area | One-line |
|----|-----|------|----------|
| C1 | CRITICAL | JOB_RESULT | `result` typed as `*string` but device sends a JSON object → successful results rejected → job stuck `running` |
| C2 | CRITICAL | Handlers | `OnJobRejected` not wired → `JOB_REJECTED` dropped → rejected jobs stuck |
| C3 | HIGH | Handlers | `OnStateSync` not wired → no reconnect reconciliation (orphan jobs/agents) |
| C4 | HIGH | JOB_PROGRESS | gateway ignores device's `status`, always forces `running`; terminal progress regresses job |
| C5 | HIGH | JOB_DISPATCH | `file_ids` / `credential_ids` never populated in the dispatch frame |
| C6 | HIGH | CRED_PUSH_ACK | gateway checks `status=="ok"` but device sends `"INJECTED"` → success seen as failure |
| C7 | HIGH | CRED_CAPTURE | `OnCredCapture` not wired → saved logins from VNC silently dropped |
| C8 | MEDIUM | JOB_CANCEL | cancel frame omits `agent_id` → device can't forward cancel to the agent |
| C9 | MEDIUM | FILE_PUSH_BEGIN | frame omits `job_id` → device stages inputs under the wrong path |
| C10 | MEDIUM | VNC_OPEN | field `ttl_secs` vs device's `ttl_s` → session TTL lost on device |
| C11 | LOW | Handlers | `OnVNCClose` / `OnSkillDispatchAck` / `OnFilePurged` not wired |
| C12 | LOW | Conn | per-conn dedup map (`processed`) grows unbounded; app rate-limit disabled |

---

## C1 — CRITICAL — `JOB_RESULT.result` type mismatch drops successful results

**Files:** `gateway/internal/model/types.go:682-687`, `gateway/cmd/gateway/main.go:148-156`,
device `device/iagent_device/jobs/dispatcher.py:147-151`, agent `agent/iagent_agent/runtime/context.py:38-49`.

The gateway payload declares:

```go
type JobResultPayload struct {
    JobID    UUID      `json:"job_id"`
    Status   JobStatus `json:"status"`
    Result   *string   `json:"result,omitempty"`   // ← string
    ErrorMsg *string   `json:"error_msg,omitempty"`
}
```

But the device sends `result` as a **JSON object** (the agent's `JobResult.model_dump()`):

```python
# dispatcher.py — success branch
await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
    "job_id": job_id,
    "status": "succeeded",
    "result": result_data,          # ← dict, e.g. {"summary": ..., "artifacts": [...]}
})
```

`json.Unmarshal` of a JSON object into `*string` **fails**. The error happens inside
`device_conn.go:289-291`, so `HandleJobResult` is never called: the job is never marked
`SUCCEEDED`, files are never cleaned up, and the **agent is never released** back to the pool.
The `FAILED` branch happens to work because it uses `error_msg` (a string).

**Net effect:** *successful* jobs hang at `running` forever; *failed* jobs complete. This is the
primary "stuck" symptom.

**Fix (gateway):** make `Result` accept arbitrary JSON.

```go
Result *json.RawMessage `json:"result,omitempty"`
```

Then in `main.go` `OnJobResult` pass it straight through (it already converts to
`json.RawMessage`; the conversion just becomes a direct assignment). The DB column `jobs.result`
is already `json`/`jsonb`-compatible (`store/jobs.go:67-78` writes `json.RawMessage`).

**Coordination:** This must be agreed with the device — see local-side **L1**. Either side may own
the canonical shape, but they must match. Recommended: `result` is a free-form JSON object,
`error` is `{code, message}` (per spec §4.3), deprecate the ad-hoc `error_msg`.

---

## C2 — CRITICAL — `OnJobRejected` handler not wired → rejected jobs stuck

**Files:** `gateway/cmd/gateway/main.go:124-208` (handler block), `gateway/internal/tunnel/hub.go:291-296`,
device `device/iagent_device/jobs/dispatcher.py:67-71`.

`main.go` calls `tunnelHub.SetHandlers(tunnel.HubConfig{...})` but only sets:
`OnHello, OnJobProgress, OnJobResult, OnAgentStatus, OnSkillState, OnFileAck, OnVNCOpened,
OnCredPushAck, OnCredCaptureAck, OnFilePull{Begin,Chunk,End}`.

It does **not** set `OnJobRejected`. The hub handler is a nil-guarded no-op:

```go
func (h *Hub) HandleJobRejected(...) error {
    if h.onJobRejected != nil { return h.onJobRejected(...) }
    return nil   // ← silently dropped
}
```

The device emits `JOB_REJECTED` whenever the allocated agent is unreachable
(`dispatcher.py:67`). With the handler unwired, that rejection is dropped and the job sits in
`dispatched`/`running` indefinitely.

**Fix:** wire `OnJobRejected` to mark the job `FAILED` (with the rejection code/message), release
the agent, and publish a WS event — mirroring `OnJobResult`:

```go
OnJobRejected: func(ctx context.Context, deviceID model.UUID, p model.JobRejectedPayload) error {
    _ = jobs.UpdateResult(ctx, p.JobID, model.JobFailed, nil)
    if job, _ := jobs.GetByID(ctx, p.JobID); job != nil && job.AgentID != nil {
        _ = allocator.Release(ctx, *job.AgentID)
    }
    broker.Publish(pubsub.JobTopic(p.JobID), model.WSEvent{Type: model.WSEventJobResult, Topic: pubsub.JobTopic(p.JobID)})
    return nil
},
```

---

## C3 — HIGH — `OnStateSync` not wired → no reconnect reconciliation

**Files:** `gateway/cmd/gateway/main.go:124-208`, `gateway/internal/tunnel/hub.go:305-310`,
spec `docs/spec/01-architecture.md §4` (Data Flow & Source of Truth).

`STATE_SYNC` (device → gateway, on every (re)connect) is the mechanism that lets the gateway
mark orphaned `RUNNING` jobs as `FAILED` and release their agents when a device restarts. The
hub has an `onStateSync` hook and `HandleStateSync`, but `main.go` never sets it, so the snapshot
is discarded.

Combined with local-side **L8** (device always sends `jobs: []`), reconnect reconciliation is
fully non-functional today. After any device restart, jobs that were in-flight are never resolved.

**Fix:** wire `OnStateSync`. For each job the gateway believes is active for this device but the
device does *not* report, mark it `FAILED` and release the agent. (Requires L8 fixed first so the
device actually reports its in-flight jobs.)

---

## C4 — HIGH — JOB_PROGRESS ignores device status & forces `running`

**Files:** `gateway/cmd/gateway/main.go:138-147`, `gateway/internal/store/jobs.go:58-65`,
`gateway/internal/model/types.go:674-680`.

`JobProgressPayload` includes `Status` and `EventSeq`, but the handler throws both away:

```go
OnJobProgress: func(ctx, deviceID, payload) error {
    if err := jobs.UpdateProgress(ctx, payload.JobID, payload.Percent, payload.Message); err != nil { ... }
    ...
}
```

and `UpdateProgress` hard-codes the transition to `running`:

```sql
UPDATE jobs SET percent=$2, progress_message=$3,
  status=CASE WHEN $4='running' THEN $4 ELSE status END   -- $4 is always model.JobRunning
  WHERE id=$1
```

Two consequences:
1. The device sends a *terminal* `JOB_PROGRESS` (`status:"succeeded"`) right before `JOB_RESULT`
   (`dispatcher.py:132-138`). The gateway rewrites it back to `running`, so even a moment of
   correctness is undone.
2. `EventSeq` is never used for idempotency (spec §2/§4.3 require dedup by `(job_id, event_seq)`).
   Today dedup relies only on `msg_id` (`device_conn.go:247-253`).

**Fix:** pass `payload.Status` into the progress update and only advance toward `running` if the
job is not already terminal; never regress a terminal status. Persist `event_seq` and ignore
out-of-order / duplicate `(job_id, event_seq)`.

---

## C5 — HIGH — JOB_DISPATCH never carries `file_ids` / `credential_ids`

**Files:** `gateway/internal/pool/allocator.go:179-200`, `gateway/internal/httpapi/jobs_handler.go:97-117`,
`gateway/internal/model/types.go:662-672`.

`JobDispatchPayload` defines `FileIDs` and `CredentialIDs`, but **neither dispatch path sets
them**:

```go
payload := model.JobDispatchPayload{
    JobID: job.ID, UserID: job.UserID, AgentID: agent.ID,
    Command: job.Command, SkillID: job.SkillID, SubmittedAt: ...,
    // FileIDs:        ← never set
    // CredentialIDs:  ← never set
}
```

- The device's `handle_job_dispatch` reads `credential_ids` (`dispatcher.py:57`) but it is always
  empty, so the device-side per-credential inject loop is dead code (see L4).
- Credentials are instead pushed via separate `CRED_PUSH` frames — **but only from the immediate
  allocation path** (`jobs_handler.go:120-144`). The **queued** path (`allocator.dispatchJob`)
  pushes *no* credentials at all. So a job that waits in the queue loses its saved logins.
- Without `file_ids`, the device cannot know how many inputs to expect before starting (it relies
  on a best-effort timeout, see local-side L-notes).

**Fix:** populate `FileIDs` and `CredentialIDs` on the dispatch payload in *both* paths, and move
the `CRED_PUSH` loop into a shared helper invoked from the allocator path too. Decide one
canonical injection mechanism (recommend: keep `CRED_PUSH` for the actual storage-state, but list
`credential_ids` in the dispatch so the device can wait for all pushes before starting — see C8/L5
ordering).

---

## C6 — HIGH — CRED_PUSH_ACK status string mismatch

**Files:** `gateway/cmd/gateway/main.go:186-192`, device `device/iagent_device/creds/relay.py:52-56`,
spec §4.9.

Device acks a successful injection with `status:"INJECTED"` (spec-compliant). Gateway checks for
`"ok"`:

```go
OnCredPushAck: func(...) error {
    if payload.Status == "ok" {            // ← never true; device sends "INJECTED"
        return credStore.Touch(...)
    }
    slog.Error("credential push failed", ...)  // ← logged on every success
    return nil
}
```

So every successful credential injection is logged as a failure and the credential's `last_used`
is never bumped.

**Fix:** check `payload.Status == "INJECTED"` (and treat `"ERROR"` as failure). Align the
`CredPushAckPayload` doc-comment (`types.go:975` says `ok | error`).

---

## C7 — HIGH — `OnCredCapture` not wired → saved logins dropped

**Files:** `gateway/cmd/gateway/main.go:124-208`, `gateway/internal/httpapi/vnc_handler.go:113-123`,
device `device/iagent_device/creds/relay.py:87-123`.

The save-login flow is:
1. Web → `POST /vnc/{id}/save-login` → gateway sends `CRED_CAPTURE` (G→D) to ask the device to
   capture (`vnc_handler.go:114`).
2. Device pulls storage-state from the agent and replies with `CRED_CAPTURE` (D→G) carrying the
   data (`relay.py:115`).
3. Gateway should encrypt + store it.

`main.go` wires `OnCredCaptureAck` but **not** `OnCredCapture`. The inbound capture (step 2) hits
`Hub.HandleCredCapture` → nil hook → dropped. **Saved logins are never persisted.**

**Fix:** wire `OnCredCapture` to base64-decode `payload.Data`, verify `sha256`, encrypt
(AES-256-GCM via `CredVault`), insert into `browser_credentials`, and send `CRED_CAPTURE_ACK`
`{credential_id, status:"STORED"}`. Note the field name is `data` on both sides
(`CredCapturePayload.Data` ↔ device `"data"`), so that part matches — only the handler is missing.

> Also note `CredCaptureAckPayload` doc says status `STORED | error` (`types.go:994`) while
> `OnCredCaptureAck` checks `!= "ok"` (`main.go:194`) — harmless today (gateway both sends and,
> oddly, listens for this ack) but should be made consistent.

---

## C8 — MEDIUM — JOB_CANCEL omits `agent_id`

**Files:** `gateway/internal/httpapi/jobs_handler.go:243-250`, device `device/iagent_device/jobs/dispatcher.py:175-185`,
spec §4.2.

```go
frame, _ := tunnel.NewFrame(model.FrameJobCancel, map[string]interface{}{
    "job_id": jobID.String(),
    "reason": "user requested",
})
```

The device's cancel handler reads `agent_id` from the payload to find the container
(`dispatcher.py:177`); since it's absent, it never calls the agent's `/cancel`. The job is marked
`cancelled` and the agent released **while the container is still executing** the job. The next
dispatch can then land on a busy agent (→ `409 BUSY` → `JOB_REJECTED` → dropped, per C2).

**Fix:** the device should resolve `agent_id` from its own job record (local-side L6 — preferred,
since the device owns that mapping). As a belt-and-suspenders, the gateway can also include
`agent_id` in the cancel payload (it knows `job.AgentID`).

---

## C9 — MEDIUM — FILE_PUSH_BEGIN omits `job_id` → inputs staged to wrong path

**Files:** `gateway/internal/model/types.go:764-770`, `gateway/internal/relay/relay.go:150-161`,
device `device/iagent_device/files/stager.py:24-47`.

```go
type FilePushBeginPayload struct {
    FileID UUID; FileName string; SizeBytes int64; TotalChunks int; SHA256 string
    // ← no JobID
}
```

The device needs `job_id` to place the file in `workspace_dir/{job_id}/inputs`
(`stager.py:26,32`). With it missing, `job_id == ""`, so files land in `workspace_dir//inputs` and
are recorded against an empty job id. The agent (which is told `workspace_dir=/work/workspaces/{job_id}`,
`dispatcher.py:85`) looks under `.../{job_id}/inputs` and finds nothing. Also
`_wait_for_files(job_id)` lists by the real job id, finds nothing, and returns immediately, so the
job can start before its inputs are written.

**Fix:** add `JobID` to `FilePushBeginPayload` and set it in `relay.go`. (Field name should be
`job_id` to match the device.)

---

## C10 — MEDIUM — VNC_OPEN field name `ttl_secs` vs `ttl_s`

**Files:** `gateway/internal/model/types.go:938-945`, device `device/iagent_device/vncbridge/bridge.py:41`,
spec §4.8 (`ttl_s`).

Gateway emits `ttl_secs`; the device reads `ttl_s` → always `0` → the device bridge runs with no
TTL guard (`bridge.py:105-111`), so a session never auto-expires on the device side; it relies
solely on the gateway to send `VNC_CLOSE`.

**Fix:** rename the gateway tag to `ttl_s` (spec-aligned), or have the device read `ttl_secs`.
Pick the spec name `ttl_s` and change the gateway.

---

## C11 — LOW — Unwired inbound handlers: VNC_CLOSE / SKILL_DISPATCH_ACK / FILE_PURGED

**Files:** `gateway/cmd/gateway/main.go:124-208`, `gateway/internal/tunnel/hub.go`.

`OnVNCClose`, `OnSkillDispatchAck`, `OnFilePurged` are never set:
- `OnVNCClose` nil → when the device tears down a VNC bridge and emits `VNC_CLOSE`
  (`bridge.py:121`), the gateway's `vnc_sessions` row is not closed → session leak in gateway state.
- `OnSkillDispatchAck` nil → device's `SKILL_DISPATCH_ACK` (cache CACHED/ERROR) is dropped → the
  gateway can't tell whether a skill package was cached before issuing the install action.
- `OnFilePurged` nil → `FILE_PURGED` (`dispatcher.py:109`) dropped → cloud file rows never move to
  `PURGED`.

**Fix:** wire all three to their existing dispatch/relay/vnc-store methods.

---

## C12 — LOW — Connection robustness

**Files:** `gateway/internal/tunnel/device_conn.go:33,148,247-253`.

- `processed sync.Map` (msg_id dedup) is per-connection and **never pruned** — a long-lived tunnel
  accumulates one entry per frame forever (memory growth). Bound it (LRU / time window) or rely on
  `(job_id, event_seq)` semantic idempotency instead.
- Application-level rate limiting is disabled (`_ = c.checkRateLimit`, line 148) — acceptable for
  now (it previously caused disconnect storms) but leaves the tunnel unprotected from a misbehaving
  device. Re-introduce with a saner threshold once frame volume is understood.

---

## Recommended fix order (cloud side)

1. **C1** (result type) and **C2** (OnJobRejected) — unblocks the stuck-job symptom immediately.
2. **C4** (progress status) — prevents terminal-status regression.
3. **C6**, **C7** — restore credential inject/capture.
4. **C5**, **C8**, **C9** — restore files + credentials + cancel on the dispatch path.
5. **C3** + local L8 — reconnect reconciliation (a small project; design the reconcile rules).
6. **C10**, **C11**, **C12** — spec drift / leaks.

## Verification checklist

- [ ] Submit a job that succeeds with a non-empty `result` object → job reaches `succeeded`, agent
      released, output files downloadable.
- [ ] Submit a job to a device whose agent container is down → `JOB_REJECTED` → job `failed`.
- [ ] Submit with `credential_ids` (both immediate and queued paths) → `CRED_PUSH_ACK INJECTED`,
      no false-failure log, `last_used` bumped.
- [ ] Save a login from a VNC session → credential row created (encrypted).
- [ ] Cancel a running job → agent `/cancel` invoked, container idle confirmed before release.
- [ ] Upload a file then submit referencing it → file present in the agent's `inputs` dir.
- [ ] Restart the device mid-job → gateway marks the orphan job failed and releases the agent.
