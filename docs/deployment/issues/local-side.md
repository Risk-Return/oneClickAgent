# Cloud ↔ Device Communication Gaps — Local (Device) Side

**Date:** 2026-06-08
**Scope:** Local device (Python) side of the reverse-tunnel contract, plus the device↔agent
HTTP boundary that the tunnel ultimately drives.
**Method:** Code audit of `device/` and `agent/` against `docs/spec/05-tunnel-protocol.md` and
`docs/spec/04-agent-container.md`, cross-checked against the gateway (`gateway/`).
**Companion doc:** `docs/deployment/issues/cloud-side.md` (gateway-side gaps).

> The single most important device-side bug is **L-OUTBOX** (the durable outbox never gets its
> entries acked, so progress/result frames pile up and re-flood on every reconnect). Combined with
> the cloud-side result-type bug, this is the core of the "command gets stuck / goes quiet" report.

## Severity legend

- **CRITICAL** — breaks the core job flow or reliably loses/duplicates job events.
- **HIGH** — a major feature broken or silently lost (creds, files, callbacks).
- **MEDIUM** — degraded / racy behavior.
- **LOW** — spec drift, resource leaks.

---

## Summary table

| ID | Sev | Area | One-line |
|----|-----|------|----------|
| L-OUTBOX | CRITICAL | Outbox | Stored `msg_id` ≠ wire `msg_id` → outbox entries never acked → re-flood on every reconnect, unbounded growth |
| L1 | CRITICAL | JOB_RESULT | sends `result` as object + non-spec `error_msg`; mismatched with gateway type (pairs with cloud C1) |
| L2 | HIGH | Agent dispatch | `create_job` positional-arg bug: `callback_url` passed into `inputs_dir`; agent never gets a callback URL |
| L3 | HIGH | Progress | no `event_seq`; 10-minute hard polling cap fails long jobs; callback path dead (relies on polling) |
| L4 | HIGH | Credentials | dispatch-time credential inject loop is dead/incorrect (no `storage_state`, ids never sent) |
| L5 | MEDIUM | Credentials | `CRED_PUSH` not synchronized with job start → cookies may be injected after `brain.run` begins |
| L6 | MEDIUM | JOB_CANCEL | reads `agent_id` from frame (gateway doesn't send it) → agent `/cancel` never called |
| L7 | MEDIUM | JOB_CANCEL | agent released before container confirmed stopped → pool hands out a busy agent |
| L8 | MEDIUM | STATE_SYNC | always sends `jobs: []` → reconnect reconciliation impossible |
| L9 | LOW | Dedup | `_processed_msg_ids` set grows unbounded |
| L10 | LOW | FILE_ACK / status casing | minor status-string drift vs spec (works because gateway uses lowercase too) |

---

## L-OUTBOX — CRITICAL — outbox entries are never acked (re-flood + leak)

**Files:** `device/iagent_device/tunnel/outbox.py:21-42`, `device/iagent_device/tunnel/client.py:157-165,202-206`,
`device/iagent_device/__main__.py:175`.

The outbox is supposed to give at-least-once delivery: persist a frame, send it, and delete it
when the gateway ACKs. But the **persisted `msg_id` and the on-the-wire `msg_id` are different
values**:

```python
# outbox.py
async def enqueue_and_send(self, frame_type, payload):
    msg_id = new_msg_id()                 # (A) stored in SQLite
    self.repo.enqueue(msg_id, str(frame_type), payload)
    result = self.send_fn(frame_type, payload)   # send_fn = TunnelClient._send
    ...
```

```python
# client.py  _send → encode_frame
def encode_frame(frame_type, payload=None, ack_id=None):
    frame = { ..., "msg_id": new_msg_id(), ... }   # (B) a *different* id goes on the wire
```

The gateway ACKs id **(B)**. On receipt the device does `self.outbox.ack(ack_id)` with **(B)**,
but the row is keyed by **(A)** → the row is **never marked acked**.

Consequences:
1. Every `JOB_PROGRESS` / `JOB_RESULT` / `AGENT_STATUS` / `FILE_PURGED` stays "unacked" in SQLite
   forever → table grows without bound.
2. On *every* reconnect, `outbox.flush()` (`client.py:104-106`) re-sends **all** of them —
   including terminal results of jobs that finished long ago. The gateway re-processes stale
   results (it dedups by `msg_id`, but flush calls `_send` which mints yet another new `msg_id`,
   so dedup doesn't even catch them). This produces phantom status changes and can re-release /
   re-touch agents.
3. There is **no in-connection retransmit** for outbox frames: `send_fn` is the raw `_send`, which
   does not register anything in `_pending_acks`, so the `_retransmit_loop` never retries a lost
   progress/result frame until the next reconnect.

**Fix:** thread a single `msg_id` end-to-end. Options:
- Make `enqueue_and_send` generate the `msg_id` and pass it explicitly into the send path
  (`encode_frame(..., msg_id=...)`), so the stored id == wire id; ACK then matches.
- Or route outbox sends through `send_with_ack` (which already tracks `_pending_acks` and supports
  retransmit), and have the ACK handler call `outbox.ack(stored_id)`.

Also add a guard so `flush()` only re-sends frames for **non-terminal** jobs, or bounded by age,
to avoid replaying ancient results.

---

## L1 — CRITICAL — JOB_RESULT payload shape disagrees with gateway

**Files:** `device/iagent_device/jobs/dispatcher.py:147-162`, agent `agent/iagent_agent/runtime/context.py:38-49`,
gateway `gateway/internal/model/types.go:682-687`.

Device sends, on success, `result` as a **JSON object** (`result_data = status_data["result"]`,
which is `JobResult.model_dump()`), and on failure a non-spec `error_msg` string. The gateway
declares `Result *string` and `ErrorMsg *string`. The object→string mismatch makes the gateway
reject successful results (full analysis in cloud-side **C1**).

The spec (§4.3) says `JOB_RESULT = { job_id, status, result:{...}, error?:{code,message}, finished_at }`.

**Fix (coordinated with cloud C1):**
- Send `result` as a JSON object (gateway switches `Result` to `json.RawMessage`).
- Replace `error_msg` with spec's `error:{code,message}`.
- Include `finished_at`.

```python
await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
    "job_id": job_id,
    "status": "succeeded",
    "result": result_data,            # object — gateway accepts as raw JSON
    "finished_at": int(time.time()*1000),
})
# failure:
{ "job_id": job_id, "status": "failed",
  "error": {"code": "AGENT_ERROR", "message": str(e)}, "finished_at": ... }
```

---

## L2 — HIGH — `create_job` positional-arg bug; agent never receives `callback_url`

**Files:** `device/iagent_device/jobs/dispatcher.py:85`, `device/iagent_device/agentclient/client.py:30-42`,
agent `agent/iagent_agent/server.py:124-151`, `device/iagent_device/jobs/callback_server.py` (whole),
`device/iagent_device/__main__.py:126-128`.

The client signature is:

```python
async def create_job(self, job_id, command, params=None, inputs_dir="", skill_id="", workspace_dir=""):
    payload = { "job_id", "command", "params", "inputs_dir", "workspace_dir", ["skill_id"] }
    # NOTE: there is no "callback_url" field in this payload at all
```

The dispatcher calls it positionally:

```python
await client.create_job(job_id, command, {}, self.callback_url, skill_id, workspace_dir=f"/work/workspaces/{job_id}")
#                                          ^^^^^^^^^^^^^^^^^ lands in `inputs_dir`, not a callback
```

So `callback_url` is silently placed into `inputs_dir`, and the agent's `/jobs` body never carries
a `callback_url`. The agent reads `body.get("callback_url")` → `None`
(`server.py:134`), so the agent's push-callback (`CallbackClient`) is disabled and the **entire
`CallbackServer` started in `__main__.py:126-128` is dead code**. Progress only works because the
dispatcher *polls* the agent (`_poll_progress`).

(It happens not to corrupt the workspace because `workspace_dir` is passed as a keyword and the
agent prefers it over `inputs_dir` — `server.py:135`. But the design intent is clearly broken.)

**Fix:** decide on one progress transport and make it consistent:
- **Preferred (push):** add `callback_url` to the `create_job` payload and pass it by keyword;
  drop the polling loop. This removes the 10-minute cap (L3) and is lower-latency.
- **Or (poll):** delete the unused `CallbackServer` and `callback_url` plumbing entirely.

Either way, stop passing `callback_url` positionally.

---

## L3 — HIGH — progress: no `event_seq`, polling cap fails long jobs

**Files:** `device/iagent_device/jobs/dispatcher.py:87-92,123-173`.

- `JOB_PROGRESS` frames omit `event_seq` (`dispatcher.py:87,133`), which the spec requires for
  idempotent ordering (§4.3). (The callback path *does* include it — `callback_server.py:114` —
  but that path is dead per L2.)
- `_poll_progress` loops `for _ in range(300)` with `await asyncio.sleep(2)` ⇒ a hard **~10-minute
  ceiling**. Any job that legitimately runs longer is force-failed with `"job timed out"`
  (`dispatcher.py:168-173`) even though the agent is still working. For browser/login-style jobs
  this is easily hit.
- The poll loop swallows all exceptions (`except Exception: pass`, line 164-165), so a transient
  agent error is invisible and just burns iterations.

**Fix:** include `event_seq` (monotonic per job). Replace the fixed iteration cap with a wall-clock
budget tied to job class, or move to the push-callback model (L2) where the agent drives terminal
state. Log (not swallow) poll errors.

---

## L4 — HIGH — dispatch-time credential injection is dead / incorrect

**Files:** `device/iagent_device/jobs/dispatcher.py:74-81`, `device/iagent_device/creds/relay.py:65-85`,
gateway `gateway/internal/httpapi/jobs_handler.go:120-144`.

`handle_job_dispatch` loops over `credential_ids` and calls `cred_relay.inject_credential` with
`{job_id, credential_id, agent_id}` — but **no `storage_state`**. `inject_credential` returns
immediately when `storage_state` is empty (`relay.py:72-73`). Moreover `credential_ids` is never
populated by the gateway (cloud **C5**), so the loop never even runs.

The real injection path is `handle_cred_push` (`relay.py:20-63`), driven by `CRED_PUSH` frames that
*do* carry `storage_state`. So `inject_credential` is dead, misleading code.

**Fix:** delete `inject_credential` and the dispatch-time loop; rely solely on `CRED_PUSH`. Then
coordinate ordering (L5).

---

## L5 — MEDIUM — `CRED_PUSH` not synchronized with job start

**Files:** `device/iagent_device/jobs/dispatcher.py:73-94`, `device/iagent_device/creds/relay.py:20-63`,
spec §10 ("Agent writes into /work/profile **before** brain.run").

`handle_job_dispatch` does not wait for any credential injection before calling
`client.create_job` (which starts `brain.run`). `CRED_PUSH` is handled by a *separate* frame
handler with no coordination, and the gateway sends `CRED_PUSH` **after** `JOB_DISPATCH`
(`jobs_handler.go:109` then `:133`). So the agent can begin executing before cookies are injected,
defeating "start already signed in."

**Fix:** have the dispatch wait until all expected credentials are injected before `create_job`.
This needs the gateway to tell the device *how many* credentials to expect — list `credential_ids`
in `JOB_DISPATCH` (cloud C5). The device then awaits N successful `CRED_PUSH` injections (with a
timeout) before starting the job.

---

## L6 — MEDIUM — JOB_CANCEL handler needs `agent_id` the gateway doesn't send

**Files:** `device/iagent_device/jobs/dispatcher.py:175-185`, gateway `gateway/internal/httpapi/jobs_handler.go:243-250`.

```python
async def handle_job_cancel(self, payload):
    job_id = payload.get("job_id", "")
    agent_id = payload.get("agent_id", "")   # ← gateway sends only {job_id, reason}
    if agent_id:                              # ← always false → agent never cancelled
        client = self.docker.get_client(agent_id)
        if client: await client.cancel_job(job_id)
    ...
```

The gateway's `JOB_CANCEL` is `{job_id, reason}` (no `agent_id`), so the agent's `/cancel` is
never invoked. The container keeps running.

**Fix:** look up `agent_id` from the local job record by `job_id` (the device's `JobRepo` already
stores it — `dispatcher.py:60`) instead of trusting the frame:

```python
job = self.job_repo.get_by_id(job_id)
agent_id = job["agent_id"] if job else ""
```

---

## L7 — MEDIUM — agent released on cancel before container confirmed stopped

**Files:** `device/iagent_device/jobs/dispatcher.py:184-185`.

`handle_job_cancel` marks the job `cancelled` and calls `agent_repo.release(agent_id)` even though
(per L6) the agent's `/cancel` was never called and the container is still busy. The pool then
considers the agent idle and may dispatch a new job to it → the agent returns `409 BUSY` → device
emits `JOB_REJECTED` → (gateway drops it, cloud C2) → second job stuck.

**Fix:** call agent `/cancel`, confirm the agent reports idle (poll `/healthz`/`/status`), *then*
release. If the agent doesn't stop within a timeout, recycle the container rather than returning it
to the pool.

---

## L8 — MEDIUM — STATE_SYNC always reports empty jobs

**Files:** `device/iagent_device/tunnel/client.py:113-133`.

```python
await self._send(FrameType.STATE_SYNC, {"jobs": [], "agents": self.hello_extras.get("agents", [])})
```

`jobs` is hard-coded empty, so the gateway (once it wires `OnStateSync`, cloud C3) still cannot
learn which jobs were in-flight at reconnect. Reconnect reconciliation is impossible from the
device side too.

**Fix:** build the `jobs` list from `JobRepo` for jobs in non-terminal local states
(`queued`/`running`), e.g. `[{job_id, agent_id, status, percent}]`, and send it on (re)connect.

---

## L9 — LOW — `_processed_msg_ids` grows unbounded

**Files:** `device/iagent_device/tunnel/client.py:66,150-155`.

The idempotency set accumulates one entry per inbound frame for the life of the process. Bound it
(time/size window) or replace with semantic idempotency (`(job_id, event_seq)` etc.).

---

## L10 — LOW — status-string drift (FILE_ACK / progress) vs spec

**Files:** `device/iagent_device/files/stager.py:69-78`, `device/iagent_device/creds/relay.py:96-128`.

The device uses lowercase status strings (`"staged_device"`, `"error"`) where the spec uses
uppercase (`STAGED_DEVICE`, `ERROR`). This currently works only because the gateway also uses
lowercase enums (`gateway/internal/model/types.go:126-132`). It is fragile spec drift; pick one
casing and document it. (Note `CRED_CAPTURE_ACK` error uses `"error"` while `CRED_PUSH_ACK` uses
`"ERROR"` — even the device is internally inconsistent.)

---

## Cross-cutting note: two progress transports, neither fully wired

The device has **both** a push transport (`CallbackServer` + agent `CallbackClient`) and a pull
transport (`_poll_progress`). Today only polling is effective (L2 disables push). Pick one as the
contract and remove the other to eliminate the dead `event_seq`/callback path and the duplicate
`JOB_PROGRESS` shapes (`dispatcher.py` vs `callback_server.py` emit different field sets).

---

## Recommended fix order (device side)

1. **L-OUTBOX** — fixes the re-flood/leak and restores reliable single-delivery.
2. **L1** (coordinate with cloud C1) — successful results parse.
3. **L2 + L3** — choose push vs poll; remove the 10-minute cap and the dead callback server.
4. **L6 + L7** — make cancel actually stop the agent before release.
5. **L4 + L5** (with cloud C5) — credentials injected before `brain.run`.
6. **L8** (with cloud C3) — reconnect reconciliation.
7. **L9 + L10** — leaks / spec drift.

## Verification checklist

- [ ] Run a job, let it finish, reconnect the tunnel several times → gateway does **not** re-receive
      the old `JOB_RESULT`; outbox table is empty/pruned.
- [ ] Run a >10-minute job → it is not force-failed with `"job timed out"`.
- [ ] Cancel a running job → agent `/cancel` is hit, container goes idle, *then* the agent is
      released; a follow-up job is not rejected with `BUSY`.
- [ ] Submit with saved logins → device waits for `CRED_PUSH` injection before starting; agent
      starts already signed in.
- [ ] Restart the device with a job running → `STATE_SYNC` reports that job; gateway reconciles.
- [ ] Upload a file + submit → file appears under the agent's `inputs` dir (needs cloud C9 too).
