# Audit 11 — Tunnel Reliability Re-audit (Return Path & Interactive Surface)

**Date:** 2026-06-10
**Scope:** Verify whether the gaps catalogued in `docs/audit/10-device-cloud-communication.md` (gap IDs **C1–C6**, **V1–V5**, **L1–L4**) have been correctly fixed in the current source tree, and identify any new reliability/robustness defects introduced by the fixes. Triggered by the user-reported symptom **"cloud still cannot poll any results."**

Reading order followed: `docs/audit/10-device-cloud-communication.md` → `docs/spec/12-return-path-and-interactive.md` → source under `gateway/`, `device/`, `agent/`.

---

## Summary

| Gap | Title | Status | Notes |
|---|---|---|---|
| C1 | Workspace mount mismatch | **Fixed** | `device/iagent_device/docker/manager.py:91` mounts `{data_dir}/work:/work:rw`; `config.py:128-131` creates `{data_dir}/work/workspaces` on startup. |
| C2 | No download endpoint for output files | **Fixed** | `gateway/internal/httpapi/router.go:133-134`, `jobs_handler.go:342-459` — `GET /jobs/{jobID}/output` and `GET /jobs/{jobID}/output/{fileID}` with tenant/role filter and path-escape defence. |
| C3 | Web UI cannot list/download outputs | **Not verified** in this pass — left for a UI-focused audit. |
| C4 | `PushFilesToDevice` is a stub | **Fixed** | `relay.go:241-255` (`PushJobInputs`) iterates files, calls real `PushFile`. |
| C5 | Outputs only pulled on `succeeded` | **Fixed** | `dispatcher.py:179-184` calls `puller.pull_outputs` for `succeeded`/`failed`/`cancelled`. |
| C6 | No backpressure / ACK loop on output pull | **Partially fixed; CRITICAL bug introduced — see §1** | inflight≤8 + per-chunk ACK protocol exists, but the device-side ACK handler never fires the asyncio.Event, so every chunk wait times out. |
| V1–V5 | VNC issues | **Appear fixed** | See §3. |
| L1 | `JOB_LOGIN_REQUIRED` frame | **Fixed** | `dispatcher.py:155-172` polls `agent/events`, forwards; gateway `main.go:320-334` publishes `WSEventLoginRequired`. |
| L2 | `save-login` origin propagation | **Fixed** | `vnc_handler.go:133-145` requires non-empty `origin`, sends `CRED_CAPTURE`. |
| L3 | `CRED_CAPTURE` schema inconsistency | **Fixed** | `creds/relay.py:131-133` and `main.go:255-265` both use `storage_state` field with base64 encoding. |
| L4 | `CRED_CAPTURE_ACK` direction | **Fixed** | `main.go:290-295` sends ACK gateway→device after vault store; device handler exists. |

**Bottom line:** the structural bugs from audit 10 are addressed. However the new code introduces a **single critical defect that explains the user-reported "cannot poll results" symptom** (B1 below), plus a **second, latent defect that would still purge outputs even if B1 were fixed** (B2). Both must be patched before the return path will work end-to-end.

---

## §1. CRITICAL bugs in the new code

### B1 — Per-chunk `FILE_PULL_ACK` does not unblock the puller

**File:** `device/iagent_device/files/puller.py:128-141`

```python
def handle_pull_ack(self, payload: dict) -> None:
    file_id = payload.get("file_id", "")
    status = payload.get("status", "")
    chunk_index = payload.get("chunk_index", -1)
    if file_id not in self._events:
        return
    if chunk_index >= 0:
        self._ack_status.setdefault(file_id, {})[chunk_index] = status   # ← evt.set() NEVER called
    elif file_id in self._events:
        for idx, evt in self._events[file_id].items():
            self._ack_status.setdefault(file_id, {})[idx] = status
            evt.set()
```

The send loop (`puller.py:79-116`) does:

```python
await asyncio.wait_for(evt.wait(), timeout=CHUNK_ACK_TIMEOUT)   # 10 s
```

Because the per-chunk branch only writes `_ack_status` and never calls `evt.set()`, `evt.wait()` always times out → `RuntimeError("chunk N ack timeout")` → 3 retries → file abandoned.

**Effect:** every output file fails to upload, including 1-chunk files (because the post-loop "wait remaining chunks" block at `puller.py:104-116` always awaits at least one Event). After ~3 × CHUNK_ACK_TIMEOUT × N_files (~30 s per file) of dead time, `pull_outputs` returns having transferred nothing. JOB_RESULT then fires with an empty output set.

**This matches the user symptom exactly: "cloud still cannot poll any results."**

**Patch:**
```python
if chunk_index >= 0:
    self._ack_status.setdefault(file_id, {})[chunk_index] = status
    evt = self._events.get(file_id, {}).get(chunk_index)
    if evt is not None:
        evt.set()
elif file_id in self._events:
    ...
```

Severity: **Critical** — root cause of the reported symptom.

---

### B2 — `CleanupJobFiles` deletes output files immediately on `JOB_RESULT`

**File:** `gateway/cmd/gateway/main.go:184` (callback)
**File:** `gateway/internal/relay/relay.go:276-290` (implementation)

```go
// main.go OnJobResult:
_ = fileRelay.CleanupJobFiles(ctx, payload.JobID)

// relay.go:
func (r *FileRelay) CleanupJobFiles(ctx context.Context, jobID model.UUID) error {
    files, err := r.store.ListByJob(ctx, jobID)   // ← returns ALL roles, including 'output'
    ...
    for _, f := range files {
        _ = r.store.MarkPurged(ctx, f.ID)
        if f.StorageURI != "" {
            _ = os.Remove(f.StorageURI)
        }
    }
}
```

Sequence:
1. Device finishes pulling outputs → `OnFilePullEnd` writes file to disk and inserts `job_files (role='output')` (`relay.go:422-427`).
2. Device sends `JOB_RESULT`.
3. Gateway `OnJobResult` callback calls `CleanupJobFiles` → **purges every row, output included, including the just-saved files on disk**.

Even if B1 were fixed, the user would still see no output files in the download API because they would be deleted milliseconds after arriving.

**Patch:** `CleanupJobFiles` should only purge `role='input'` (inputs were the temporary staging — outputs are the user's results and have their own retention TTL). `store.ListByJobAndRole(ctx, jobID, "input")` already exists (`store/files.go:142`).

Severity: **Critical** — primary cause if B1 were resolved alone.

---

### B3 — `pull_outputs` blocks `JOB_RESULT` for ~30 s × N during failure

**File:** `device/iagent_device/jobs/dispatcher.py:179-198`

`pull_outputs` is awaited *before* `JOB_RESULT` is sent. Today (because of B1) this blocks for `MAX_RETRIES × CHUNK_ACK_TIMEOUT × N_files` (~90 s/file) before the dispatcher gives up and emits `JOB_RESULT`. The exception is swallowed, so the result still eventually flows, but the user sees a long stall and the log fills with timeout errors.

After B1 is fixed this becomes a non-issue for the happy path, but recommend adding a **wallclock cap** on `pull_outputs` (e.g. 60 s total) so a stuck transfer cannot indefinitely delay the result.

Severity: **Significant** — UX regression while B1 is unpatched, latent defence-in-depth gap after.

---

### B4 — File-level `RECEIVED` ACK ambiguity (latent)

**File:** `gateway/internal/relay/relay.go:433-441`

```go
ack := model.FilePullAckPayload{ FileID: fileID, Status: status, Error: errMsg }
if len(chunkIndex) > 0 { ack.ChunkIndex = chunkIndex[0] }
```

The struct field is `ChunkIndex int  json:"chunk_index,omitempty"`. For a file-level `RECEIVED` ACK, no chunk index is passed, so the field is omitted (Go zero-value + `omitempty`). Python's `handle_pull_ack` defaults `chunk_index = -1` and falls into the `elif` branch — **but** by the time this ACK arrives, `_send_file_once` has already returned and called `_cleanup_file_events`, so `file_id not in self._events` and the handler returns early. The ACK is silently dropped.

This is currently harmless (no code path awaits the file-level ACK) but is a latent bug: a future change that does await `RECEIVED` will deadlock. Worse, if a `CHUNK_OK` ACK ever lacks an explicit `chunk_index` field for any reason, Python will mis-classify it as a file-level ACK and broadcast `evt.set()` to every pending chunk with status `"CHUNK_OK"` — potentially racing the send loop.

**Patch:** make the wire-level distinction explicit. Either always set `chunk_index` (use `-1` for file-level) and remove `omitempty`, or split into two separate frame types (`FILE_PULL_CHUNK_ACK` vs `FILE_PULL_FILE_ACK`).

Severity: **Minor** today, **Significant** if relied upon.

---

## §2. Other observations on the return-path code

- `puller.py:36-37` skips `inputs/` subtree under `output/` — guards against accidental re-upload of inputs that were copied into the workspace. Fine.
- `dispatcher.py:182` swallows `pull_outputs` exceptions broadly. After B1 is fixed, consider distinguishing "no outputs to pull" from "transfer failure" so the gateway can present a partial-result status.
- `relay.go:386-415` writes outputs under `{baseDir}/jobs/{jobID}/output/{name}`. There is no path-escape check on `pt.name` here — although the device should never produce a relative path, a malicious agent could send `name="../../etc/passwd"`. The download API has its own escape guard, but writes happen first. Consider `filepath.Base` or rejecting names containing `..` / path separators.
- `OnFilePullEnd` writes the file in one pass with all chunks held in memory (`relay.go:400-405`). For the configured `maxSize` cap this is acceptable, but a streaming write would reduce peak RAM use during many concurrent transfers.

## §3. VNC subsystem (V1–V5)

Spot-checked, no defects found:

- `gateway/internal/httpapi/vnc_handler.go:74-86` blocks on `WaitReady(15s)` and returns `rfb_password` + `ws_url` to the browser (V2 fix).
- `vncrelay/relay.go:140-186` correctly fans `MarkReady` / `MarkError` through `readyCh` (V3).
- `vncrelay/relay.go:233-285` pairs browser↔device sockets and pumps binary frames (V1).
- `vncbridge/bridge.py:74-124` dials `relay_url` with `Authorization: Bearer <session_token>` and bridges to local agent RFB port (V4, V5).
- Reaper at `relay.go:347-397` enforces idle/max TTL.

## §4. Login support (L1–L4)

- L1 `JOB_LOGIN_REQUIRED`: device polls `agent/events`, gateway publishes WSEvent. Working.
- L2 `save-login` requires non-empty `origin`. Working.
- L3 `CRED_CAPTURE` schema: `storage_state` field consistent on both ends, base64 + sha256 verified. Working.
- L4 `CRED_CAPTURE_ACK`: gateway sends `STORED` ACK to device after vault insert. Working.

One observation: `creds/relay.py:115-118` JSON-encodes `storage_state` if the agent returns a dict. The hash is then computed over the JSON-encoded string. Gateway re-decodes from base64 and verifies that hash. Round-trip is consistent **only if the agent always returns the same shape**. Recommend documenting the wire contract: agent returns the storage state as a JSON string (Playwright `storage_state()` JSON) — the device should not re-serialise.

---

## Recommended patch order

1. **Fix B1** (`puller.py:128-141`) — single 3-line change. This alone restores most of the return path.
2. **Fix B2** (`relay.go:276-290`) — change `ListByJob` → `ListByJobAndRole(ctx, jobID, "input")`. After this, output files survive and become downloadable via the C2 endpoint.
3. **Add wallclock cap to `pull_outputs`** (B3) — defence in depth.
4. **Disambiguate file-level vs chunk-level ACK** (B4) — preventative.
5. Path-escape guard on `pt.name` in `OnFilePullEnd` — preventative.

Items 1 + 2 are sufficient to resolve the user-reported symptom. The rest are robustness improvements.
