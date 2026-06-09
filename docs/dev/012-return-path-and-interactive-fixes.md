# Dev Record — Return Path & Interactive Fixes (PR1–PR5)

**Date:** 2026-06-09
**Scope:** Fixes all 15 gaps identified in `docs/audit/10-device-cloud-communication.md`, implementing spec `docs/spec/012-return-path-and-interactive.md`.

---

## PR1 — Workstream A: Workspace Path Mapping & Input Staging

### A1 — Workspace path mapping (gap C1)

**Root cause:** Dispatcher told agent `workspace_dir=/work/workspaces/{id}` but Docker volume mount was `{data_dir}/workspaces:/workspaces`. Agent wrote to `/work/workspaces/{id}/output` (anonymous Docker VOLUME), device puller scanned `{data_dir}/workspaces/{id}` (empty).

**Files changed:**
- `device/iagent_device/docker/manager.py:91` — volume mount changed to `{data_dir}/work:/work:rw`
- `device/iagent_device/docker/manager.py:200` — reaper_cleanup path updated to `{data_dir}/work/workspaces/{job_id}`
- `device/iagent_device/config.py:51` — workspace_dir renamed to `device_data_dir / "work"`, with `/work/workspaces` subdirectory auto-created
- `device/iagent_device/files/puller.py:22-23` — scan path changed to `workspace_dir / "workspaces" / job_id / "output"`
- `device/iagent_device/files/stager.py:32,81` — staging paths updated to `workspace_dir / "workspaces" / job_id / "inputs"`

### A2 — PushFilesToDevice (gap C4)

**Root cause:** `PushFilesToDevice` in `gateway/internal/httpapi/jobs_handler.go:286-289` was a `return nil` stub.

**Files changed:**
- `gateway/internal/httpapi/jobs_handler.go:287-289` — replaced stub with `deps.Relay.PushJobInputs(ctx, job, deviceID)`
- `gateway/internal/relay/relay.go` — added `PushJobInputs(ctx, job, deviceID)` method that queries `job_files` and calls `PushFile` for each

---

## PR2 — Workstream B: Output File Return Path

### B1 — DB: role column + job_files insert (gap C2 part 1)

**Files changed:**
- `gateway/migrations/005_add_job_files_role.up.sql` — new migration adding `role text NOT NULL DEFAULT 'input' CHECK (role IN ('input','output'))` + index
- `gateway/internal/store/files.go:114` — `LinkToJob` updated to accept `role` parameter, upsert ON CONFLICT
- `gateway/internal/store/files.go` — added `ListByJobAndRole(ctx, jobID, role)` method
- `gateway/internal/store/interfaces.go` — updated `FileStoreInterface` with new signatures
- `gateway/internal/store/mock.go:418` — updated mock to match new signature + added `ListByJobAndRole`
- `gateway/internal/store/store_test.go:693` — updated test call to pass `"input"`
- `gateway/internal/httpapi/jobs_handler.go:64` — pass `"input"` role to `LinkToJob`
- `gateway/internal/relay/relay.go:427` — `OnFilePullEnd` inserts `job_files` row with `role="output"`
- `gateway/internal/relay/relay.go` — added `GetJobUserID` callback to resolve user_id from job
- `gateway/cmd/gateway/main.go` — wired `GetJobUserID` callback

### B2 — Download API (gap C2 part 2)

**Files changed:**
- `gateway/internal/httpapi/jobs_handler.go` — added `handleListJobOutputs()` (GET /jobs/{id}/output) and `handleDownloadJobOutput()` (GET /jobs/{id}/output/{file_id})
- `gateway/internal/httpapi/router.go:131-132` — registered new routes

### B3 — Web UI output panel (gap C3)

**Files changed:**
- `web/src/api/schemas.ts` — added `JobOutputFileSchema` and `JobOutputListSchema`
- `web/src/api/client.ts` — added `getBlob(path)` method for binary downloads
- `web/src/components/JobOutputs.tsx` — new component: polls output endpoint, renders file list with download buttons
- `web/src/pages/JobsPage.tsx` — integrated `<JobOutputs>` after result display

### B4 — Pull on failed/cancelled (gap C5)

**Root cause:** `pull_outputs` was only called inside `if status == "succeeded"` branch.

**Files changed:**
- `device/iagent_device/jobs/dispatcher.py:158-168` — moved `pull_outputs` call before the status branch, runs for all terminal statuses

### B5 — Puller backpressure (gap C6)

**Files changed:**
- `gateway/internal/relay/relay.go:332` — `OnFilePullChunk` now sends `FILE_PULL_ACK` per chunk with `chunk_index`
- `gateway/internal/model/types.go:813` — `FilePullAckPayload` now includes `ChunkIndex` field
- `gateway/internal/relay/relay.go:435` — `sendPullAck` accepts variadic `chunkIndex`
- `device/iagent_device/files/puller.py` — rewritten `_send_file` with:
  - `MAX_CHUNKS_IN_FLIGHT = 8` semaphore via `asyncio.Event`
  - Per-chunk ACK tracking with timeout
  - `MAX_RETRIES = 3` per file
  - `handle_pull_ack` updated to match per-chunk ACKs by chunk_index

---

## PR3 — Workstream C: VNC End-to-End

### C1 — Async-ready handshake (gaps V1, V2, V3)

**Files changed:**
- `gateway/internal/vncrelay/relay.go` — added `readyCh` and `readyErr` to `Session`, added `MarkError()` and `WaitReady()` methods
- `gateway/internal/vncrelay/relay.go:113` — `CreateSession` initializes `readyCh`
- `gateway/internal/vncrelay/relay.go:125` — `MarkReady` closes `readyCh` channel
- `gateway/cmd/gateway/main.go:229` — `OnVNCOpened` calls `MarkError` instead of `CloseSession` on error
- `gateway/internal/httpapi/vnc_handler.go:17-80` — `handleOpenVNC` rewritten to:
  - Build absolute `ws_url` with scheme + host + `?token=<session_token>`
  - Block on `WaitReady` for up to 15s
  - Return `rfb_password` in response
- `gateway/internal/httpapi/vnc_handler.go:289` — `handleVNCBrowserSocket` now accepts both `session_token` (via hash comparison) and JWT tokens

### C2 — Relay framing fix (gap V4)

**Files changed:**
- `gateway/internal/vncrelay/relay.go:198-223` — `pump()` replaced `dst.UnderlyingConn()` with `dst.NextWriter(websocket.BinaryMessage)`, validates `msgType == BinaryMessage`

### C3 — Reconnect-friendly GET (gap V5)

**Files changed:**
- `gateway/internal/httpapi/vnc_handler.go:233` — `handleGetJobVNC` now consults in-memory `VNCRelay.GetSession()` before falling back to DB, returns `rfb_password`, `ws_url` when live
- `gateway/internal/model/types.go:544` — `VNCStatusResponse` extended with `RFBPassword`, `WSUrl`, `TTLSecs` fields

---

## PR4 — Workstream D1-D4: Login Event Chain

### D1 — Agent events endpoint

**Files changed:**
- `agent/iagent_agent/runtime/executor.py` — added `_job_events` list, `post_event()`, `get_events_since()`, `clear_events()` methods
- `agent/iagent_agent/server.py:173-191` — added `POST /jobs/{job_id}/events` and `GET /jobs/{job_id}/events?since=N` endpoints

### D2 — JOB_LOGIN_REQUIRED frame

**Files changed:**
- `gateway/internal/model/types.go` — added `FrameJobLoginRequired`, `JobLoginRequiredPayload`, `WSEventLoginRequiredData`
- `gateway/internal/model/types.go:850` — added `WSEventLoginRequired = "job.login_required"`
- `gateway/internal/tunnel/router.go:161` — registered `FrameJobLoginRequired` → `HandleJobLoginRequired`
- `gateway/internal/tunnel/hub.go` — added `onJobLoginRequired` field, `HandleJobLoginRequired` method, wiring in `NewHub`/`SetHandlers`
- `gateway/internal/tunnel/device_conn.go:383` — added case for `FrameJobLoginRequired`
- `device/iagent_device/tunnel/codec.py:65` — added `JOB_LOGIN_REQUIRED` frame type
- `device/iagent_device/jobs/dispatcher.py:140-158` — poll agent events and emit `JOB_LOGIN_REQUIRED` frames
- `device/iagent_device/agentclient/client.py:96` — added `get_job_events()` method

### D3 — Gateway WS event

**Files changed:**
- `gateway/cmd/gateway/main.go:320` — `OnJobLoginRequired` handler publishes `WSEventLoginRequired` on job topic

### D4 — Web UI login prompt

**Files changed:**
- `web/src/pages/JobsPage.tsx` — added `loginRequired` state, WS handler for `job.login_required`, banner with `[Open Browser]` button
- Banner cleared on terminal status; toast shows with action button

---

## PR5 — Workstream D5-D7: Save-login Fixes

### D5 — Save-login carries origin (gap L2)

**Files changed:**
- `gateway/internal/model/types.go:1014` — `SaveLoginRequest` now includes `Origin` field
- `gateway/internal/httpapi/vnc_handler.go:107-114` — handler validates `req.Origin` is non-empty, passes it to `CredCapturePayload`
- `web/src/components/VNCPanel.tsx` — added origin input field, updated `onSaveLogin` signature to `(sessionId, label, origin)`
- `web/src/pages/JobsPage.tsx:183` — `handleSaveLogin` now passes `origin` in the POST body

### D6 — CRED_CAPTURE field rename (gap L3)

**Files changed:**
- `gateway/internal/model/types.go:987` — `CredCapturePayload.Data` → `StorageState` (JSON tag `storage_state`), added `StorageStateEncoding`
- `gateway/cmd/gateway/main.go:248` — `payload.Data` → `payload.StorageState`
- `device/iagent_device/creds/relay.py:131` — `"data"` → `"storage_state"`, added `"storage_state_encoding": "base64"`

### D7 — CRED_CAPTURE_ACK direction (gap L4)

**Files changed:**
- `device/iagent_device/creds/relay.py:106-110` — capture failure now emits `FrameType.ERROR` with `code: "CRED_CAPTURE_FAILED"` instead of `FrameType.CRED_CAPTURE_ACK`
- `device/iagent_device/creds/relay.py:135-138` — same change in exception handler

---

## Verification

| Check | Status |
|-------|--------|
| `go build ./...` | Pass |
| `go vet ./...` | Pass |
| Go unit tests (vncrelay, tunnel, etc.) | Pass (store tests fail due to missing PG) |
| Web TypeScript typecheck | Pre-existing `ImportMeta.env` errors only |
