# Local Side — Result/Status Reporting Not Reaching Cloud

**Date:** 2026-06-09
**Scope:** Device (Python) → Gateway reporting of job progress/results, and the device↔agent
HTTP boundary that feeds it.
**Status:** Root causes identified; fixes applied (see "Fix" sections).
**Related:** `docs/deployment/issues/local-side.md`, `docs/deployment/issues/cloud-side.md`
(earlier full audit — most of those items are now fixed; this doc covers what remained).

## Context

After the first audit round, the gateway side was largely aligned:
- `JobResultPayload.Result` is now `*json.RawMessage` (accepts the agent's object result).
- `OnJobAccepted` / `OnJobRejected` / `OnStateSync` handlers are wired.
- `UpdateProgress` now takes a status.
- The gateway ACKs all non-control frames, and the device outbox now stores the **same** `msg_id`
  it puts on the wire, so ACKs match.

Yet end-to-end a container still fails to get its result back to the cloud reliably, with
symptoms: "framework not matched between cloud and local", "callback URL", "status report
failed". The remaining root causes are all on the **device↔agent / device→gateway reporting
path**.

## Symptoms

- A job runs in the container but the cloud never moves it to `succeeded`/`failed` (stays
  `running`), or the cloud receives **duplicate** terminal results.
- Failed jobs never report — they hang until the 1-hour poll timeout, then report a generic
  "job timed out".
- Behavior differs between environments (works on a plain Linux Docker host, breaks on custom
  Docker networks / macOS / Windows).

---

## Root causes

### R1 (CRITICAL) — Two competing result transports run at once

**Files:** `device/iagent_device/jobs/dispatcher.py:75,85` (poll), `device/iagent_device/jobs/callback_server.py`,
`device/iagent_device/__main__.py:126-139`, agent `agent/iagent_agent/runtime/context.py:52-79`.

The device drives the agent **and** asks the agent to call back:

1. `dispatcher.handle_job_dispatch` passes `callback_url` to `create_job`, so the agent's
   `CallbackClient` POSTs every event to the device's `CallbackServer`, which enqueues
   `JOB_PROGRESS` / `JOB_RESULT`.
2. `dispatcher._poll_progress` *also* polls `GET /jobs/{id}` every 2s and enqueues
   `JOB_PROGRESS` / `JOB_RESULT`.

Both paths fire. When the callback host is reachable, the cloud receives **two** terminal
`JOB_RESULT` frames per job → the gateway runs its release/cleanup logic twice (the second release
can hit an agent that has already been re-allocated to a different job). When the callback host is
*not* reachable, only polling works — so the system behaves differently per environment, which
looks like "framework not matched".

**Decision:** standardize on **polling only**. In this architecture the device created the
container and always reaches it at `127.0.0.1:<mapped_port>`; the reverse direction
(container → device host) is the fragile one (see R3). Polling is the robust, cross-platform
choice. 2s latency is acceptable for this product (progress-level UI only).

**Fix:** the device no longer passes `callback_url` to the agent and no longer starts the
`CallbackServer`. `_poll_progress` is the single source of truth for progress + terminal result +
output-file pull.

---

### R2 (CRITICAL) — `error` field type mismatch makes failed jobs hang

**Files:** `device/iagent_device/jobs/dispatcher.py:143,161`, `device/iagent_device/jobs/callback_server.py:117-123`,
agent `agent/iagent_agent/runtime/context.py:38-49`.

The agent reports a failed job with `error` as a **string** (`record.error = str(exc)`,
`to_dict()` sets `d["error"] = self.error`). But the device treats it as a **dict**:

```python
error_data = status_data.get("error", {})
... error_data.get("message", status)   # AttributeError when error_data is a str
```

For a failed job, `status_data["error"]` is a string → `str.get` raises `AttributeError` →
caught by the poll loop's `except Exception: logger.warning("poll error")` → the loop keeps
spinning and **never emits the terminal `JOB_RESULT`** until the 1-hour timeout fires. The
callback path has the identical bug (`error_data.get("message", "")`).

So *successful* jobs report (no `error` access), but *failed* jobs never report correctly — a
classic "status report failed".

**Fix:** normalize `error` to a message string whether the agent sends a string or a
`{code, message}` dict.

---

### R3 (HIGH) — Callback URL host is hardcoded and non-portable

**Files:** `device/iagent_device/__main__.py:127`.

```python
callback_server = CallbackServer("0.0.0.0", 0, outbox, advertise_host="172.17.0.1")
```

`172.17.0.1` is the default Docker bridge gateway on Linux **only**. It does not exist on Docker
Desktop (macOS/Windows, which would need `host.docker.internal`) and is wrong whenever the agent
container is attached to a user-defined bridge or a compose network. When the agent can't reach it,
callbacks silently fail (agent logs a warning, job still completes), and the device falls back to
polling — which is exactly the inconsistent, environment-dependent behavior reported. The spec
requires the device/agent to run on Windows and macOS too (`00-overview §4`).

**Fix:** with R1 (polling only), the callback server is removed entirely, eliminating this
host-reachability problem. (If push callbacks are ever revived, host detection must be derived from
the container's actual network — e.g. inspect the bridge gateway or use `host.docker.internal` on
Desktop — not hardcoded.)

---

## Lower-severity / follow-ups (not blocking, noted for alignment)

### N1 — Failed-job `error_msg` is dropped by the gateway

`JobResultPayload.ErrorMsg` exists, but `OnJobResult` calls `UpdateResult(jobID, status, result)`
and never persists `error_msg` (`gateway/cmd/gateway/main.go:154-157`, `store/jobs.go UpdateResult`).
Failed jobs are marked `failed` but the reason is lost to the UI. Cloud-side follow-up: persist
`error_msg` into `jobs.error_message`.

### N2 — `AGENT_STATUS` carries fields the gateway ignores

The device sends `{agent_id, status, health, restarts, usage, ts}` (`dispatcher.py:105-112`); the
gateway `AgentStatusPayload` only reads `agent_id`, `status` (plus optional `container_id`,
`cpu_percent`, `memory_mb`). Harmless today (extra keys ignored), but the telemetry the device
sends does not match the keys the gateway expects (`usage.cpu_pct` vs `cpu_percent`). Align field
names if/when live telemetry is surfaced.

### N3 — Dead code after R1

`callback_server.py` and the agent's `CallbackClient` become unused once polling is authoritative.
Left in the tree for now (no longer wired) to keep this change focused; can be deleted in a
cleanup pass, or kept as the basis for a future push transport with proper host detection.

---

## Fixes applied (this change)

| File | Change |
|------|--------|
| `device/iagent_device/jobs/dispatcher.py` | Stop sending `callback_url` to the agent; drop the `callback_url` constructor arg; add `_error_message()` to normalize `error` (str or `{code,message}` dict) in `_poll_progress`; emit terminal `JOB_RESULT` for failed jobs reliably |
| `device/iagent_device/__main__.py` | Remove the `CallbackServer` creation/start/stop and the `callback_url` wiring; dispatcher runs poll-only; add missing `import time` (pre-existing `F821`) |
| `device/tests/conftest.py` | Fix stale `noop_send` fixture to accept the `msg_id` kwarg (matches the real `send_fn` since the L-OUTBOX fix) |
| `device/tests/test_dispatcher.py` | Add regression tests: failed job with **string** `error` and with **dict** `error` both emit exactly one terminal `JOB_RESULT failed` carrying the message |

## Verification checklist

- [ ] Submit a job that **succeeds** → cloud receives exactly **one** `JOB_RESULT succeeded`, job
      → `succeeded`, agent released once, output files relayed.
- [ ] Submit a job that **fails** (e.g. brain raises) → cloud receives `JOB_RESULT failed`
      promptly (not after 1h), with the agent's error message.
- [ ] Confirm no duplicate `JOB_RESULT` frames in gateway logs for a single job.
- [ ] Run on a host where the agent container is on a user-defined network → reporting still works
      (no dependency on `172.17.0.1`).
- [ ] `cd device && ruff check . && mypy .` pass; `cd agent && python -m pytest` pass.
