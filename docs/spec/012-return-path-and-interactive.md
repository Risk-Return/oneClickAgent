# 12 — Return Path & Interactive Plan (Result Pull, VNC, Login)

Detailed implementation plan addressing the gaps catalogued in [audit 10](../audit/10-device-cloud-communication.md). Sources of truth this spec must remain consistent with: `01-architecture.md`, `04-agent-container.md`, `05-tunnel-protocol.md` (§4.3, §4.7, §4.9, §4.10, §9, §10), `06-data-model.md`, `07-api.md`, `09-web-ui.md`.

This document is **prescriptive**: it names exact files, frame fields, endpoint shapes, DB columns, and sequencing so implementers do not need to re-derive design decisions. Where a choice is open, it is flagged "OPEN QUESTION" at the bottom. Anything not flagged is **locked**.

---

## 0. Reading order for implementers

1. This file (12) — full context
2. [audit 10](../audit/10-device-cloud-communication.md) — gap IDs (C1–C6, V1–V5, L1–L4) referenced throughout
3. [05 §4](./05-tunnel-protocol.md) — tunnel frames being added/changed
4. [07 §5, §9](./07-api.md) — REST/WS endpoints being added/changed
5. [04 §HTTP API](./04-agent-container.md) — agent HTTP surface being extended

Every workstream below cites a gap ID from audit 10. Do not start a workstream without re-reading the corresponding audit section first.

---

## 1. Scope

In scope:

- **Workstream A** — Workspace path mapping (C1) and input file staging (C4).
- **Workstream B** — Output file return path: DB linkage (C2), download API (C2), UI surface (C3), failed-job pull (C5), backpressure (C6).
- **Workstream C** — VNC end-to-end fixes: async ready handshake (V1), absolute `wss://` URL with auth token (V2, V3), relay framing (V4), reconnect lookup (V5).
- **Workstream D** — Login support: `JOB_LOGIN_REQUIRED` event chain (L1), `save-login` origin propagation (L2), `CRED_CAPTURE` schema reconciliation (L3), `CRED_CAPTURE_ACK` direction (L4).

Out of scope (deferred):

- Replacing the polling-based progress loop in `device/iagent_device/jobs/dispatcher.py` with a streaming agent API.
- Cross-session credential sharing, credential rotation policy, or per-org vault keys.
- Any frontend redesign beyond the panels needed for output files and login prompts.
- Refactoring the Outbox `send_fn` signature mismatch (audit 03 B1) — covered separately; this spec assumes audit 03 B1 has been resolved before any of D2/D3/L-events are exercised in production. If it has not, fix it first.

---

## 2. Glossary used here

| Term | Meaning |
|------|---------|
| "agent" | the Docker container running the AI brain + browser |
| "device" | the Python process on the local host that owns the docker daemon |
| "gateway" | the Go process in the cloud |
| "web" | the browser-side React app |
| `{data_dir}` | the device data directory (`device.config.data_dir`); typically `~/.iagent/data` |
| `{baseDir}` | the gateway file-store base (`relay.NewRelay` arg); typically `/var/lib/iagent/files` |

---

## 3. Workstream A — Workspace path mapping & input staging

### A1. Fix workspace bind mount (gap C1)

**Decision (locked):** keep the agent contract `workspace_dir = /work/workspaces/{job_id}` and bind-mount the host workspace at the container's `/work` parent. The agent code, `agent/Dockerfile` `VOLUME ["/work"]`, and the spec language ("agent writes outputs to `/work/output`") all already assume `/work`. Changing the agent contract instead would require touching `04-agent-container.md`, the agent Dockerfile, and existing skill scripts. Bind-mount is the smaller blast radius.

**Concrete changes:**

1. `device/iagent_device/docker/manager.py`
   - Replace the single `workspace_mount = f"{self.data_dir}/workspaces:/workspaces:rw"` (line ~91) with **two** named host paths:
     - `{data_dir}/work/workspaces` → `/work/workspaces` (rw)
     - `{data_dir}/work/output` → `/work/output` (rw)  *(only if executor uses a top-level `/work/output`; otherwise drop this and per-job outputs live under `/work/workspaces/{id}/output`)*
   - Ensure the host directories exist (`pathlib.Path(...).mkdir(parents=True, exist_ok=True)`) before `containers.run`.
   - Keep `read_only=True` on the container; the bind mounts are writable so the agent can still write.

2. `device/iagent_device/jobs/dispatcher.py:88`
   - No change to `workspace_dir=f"/work/workspaces/{job_id}"`.

3. `device/iagent_device/files/puller.py:22`
   - Change `ws = self.workspace_dir / job_id` to `ws = self.workspace_dir / "workspaces" / job_id / "output"` so it scans the **output subtree only** (current code recurses the entire workspace, which would also pull `inputs/` — that's wrong).
   - `self.workspace_dir` continues to point at `{data_dir}/work` (renamed from `{data_dir}/workspaces`).

4. `device/iagent_device/config.py`
   - Rename `workspace_dir` default from `~/.iagent/data/workspaces` to `~/.iagent/data/work`.
   - Document the new layout in the docstring: `{data_dir}/work/workspaces/{job_id}/{inputs,output,profile,...}`.

5. **One-shot migration**: on device start, if `{data_dir}/workspaces` exists and `{data_dir}/work/workspaces` does not, log a warning and create the new tree empty. Do **not** auto-migrate old workspaces; jobs in flight during upgrade are best-effort lost (acceptable for current dev stage).

**Acceptance test:** integration test that submits a job whose command writes a file to `/work/workspaces/{id}/output/hello.txt`, then asserts the file appears at `{data_dir}/work/workspaces/{id}/output/hello.txt` on the host.

### A2. Implement `PushFilesToDevice` (gap C4)

**Concrete changes** in `gateway/internal/httpapi/jobs_handler.go:286-289` and a new helper:

1. Replace the stub with a call to a new package `gateway/internal/relay`:
   ```go
   func (deps *Dependencies) PushFilesToDevice(ctx context.Context, job *model.Job, deviceID model.UUID) error {
       return deps.FileRelay.PushJobInputs(ctx, job, deviceID)
   }
   ```
2. Add `PushJobInputs(ctx, job, deviceID) error` to `gateway/internal/relay/relay.go`:
   - Read `job.FileIDs` (already populated from `job_files` join in jobs_handler).
   - For each file: open it from the file store, compute / re-use stored sha256, chunk at 256 KiB base64, emit `FILE_PUSH_BEGIN` → `FILE_CHUNK` × N → `FILE_PUSH_END`, awaiting `FILE_ACK` per file with timeout 30 s.
   - Mirror the receive side `OnFilePullBegin/Chunk/End` already implemented; refactor the chunking helper into a shared internal function so push and pull use the same code.
   - Return the first error; abort remaining files.
3. Sequencing: `PushFilesToDevice` MUST complete before `JOB_DISPATCH` is sent. The current `dispatcher.py:48-59` 30 s wait stays as a safety net but should be unreachable when this works.

**Acceptance test:** integration test that submits a job with one input file ≥ 600 KiB (forces multi-chunk), and asserts the agent sees the file under `/work/workspaces/{id}/inputs/` with byte-identical sha256.

---

## 4. Workstream B — Output file return path

### B1. Insert `job_files` rows on `FILE_PULL_END` (gap C2 part 1)

`gateway/internal/relay/relay.go:386-396` currently writes the file to disk and updates `model.File` in the `files` table but does **not** insert into `job_files`.

**DB migration (locked, required):** the current `job_files` schema (`06-data-model.md §1.5`) is `(job_id, file_id)` only — there is no way to distinguish an input file (uploaded by the user, pushed to the device pre-job) from an output file (produced by the agent, pulled from the device post-job). Both currently end up in the same join table without a discriminator.

Add a `role` column:

```sql
ALTER TABLE job_files
  ADD COLUMN role text NOT NULL DEFAULT 'input'
  CHECK (role IN ('input','output'));

CREATE INDEX idx_job_files_job_role ON job_files (job_id, role);
```

- Backfill: existing rows are user-uploaded inputs → default `'input'` is correct.
- Update `06-data-model.md §1.5` accordingly (see §7).
- Update the Go model `model.JobFile` and any insert/select callsites in `gateway/internal/store` to carry `role`.

**Concrete changes:**

1. After `OnFilePullEnd` successfully writes the file:
   - Resolve the `Job` by `job_id` to obtain `user_id`.
   - Insert (or upsert) `job_files (job_id, file_id, role)` with `role = 'output'`.
   - Set `model.File.UserID = job.UserID`, `File.Status = "staged_cloud"` (per `files.status` enum in `06-data-model.md §1.6`), and ensure `File.Sha256`, `File.Size`, `File.Name` are populated from the `FILE_PULL_BEGIN` payload.
2. Send `FILE_PULL_ACK { status: "RECEIVED" }` only after the DB transaction commits. Failure path → `status: "ERROR"`.
3. The input-push side (Workstream A2) MUST insert with `role = 'input'`. Verify after PR1 that the existing `jobs_handler.go:62-65` insert is updated to pass `'input'` explicitly rather than relying on the column default — explicit is safer when the default later changes.

### B2. Output download API (gap C2 part 2)

Add **two** REST routes under `gateway/internal/httpapi/router.go` in the authenticated `/api/v1` group:

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/jobs/{job_id}/output` | List output files for a job |
| `GET` | `/jobs/{job_id}/output/{file_id}` | Stream a single output file |

**Response shapes (locked):**

```jsonc
// GET /jobs/{job_id}/output
{
  "job_id": "01J...",
  "files": [
    { "file_id":"01J...", "name":"result.txt", "size":1234, "sha256":"...", "created_at":"2026-06-09T10:00:00Z" }
  ]
}
```

```
GET /jobs/{job_id}/output/{file_id}
200 OK
Content-Type: application/octet-stream
Content-Disposition: attachment; filename="result.txt"
Content-Length: <size>
ETag: "<sha256>"
<bytes>
```

**Tenant isolation:** both handlers MUST verify `job.user_id == claims.user_id` (or admin role) before responding, identical to existing `GET /jobs/{id}/result`.

**Path resolution:** files live at `{baseDir}/jobs/{job_id}/output/{name}` (already so per `relay.go:386-395`). Translate `{file_id}` → `name` via the `files` table (`File.Name`). Reject if the resolved path escapes `{baseDir}` (defence in depth).

**Role filter:** the list endpoint MUST filter `job_files.role = 'output'`; the download endpoint MUST verify `(job_id, file_id, role='output')` before serving — input files are never returned to the user via this route, even though they share the `files` table.

**Streaming:** use `http.ServeFile` or `io.Copy` with a 64 KiB buffer. No range requests required in v1.

### B3. Web UI output panel (gap C3)

In `web/src/pages/JobsPage.tsx`:

1. After the existing result JSON block (`JobsPage.tsx:270-284`), add a `<JobOutputs jobId={...} />` panel that:
   - Calls `GET /api/v1/jobs/{id}/output` via `apiClient.get` typed by a new `JobOutputListSchema` in `web/src/api/schemas.ts`.
   - Renders one row per file: name, size (human-readable), sha256 short prefix, **Download** button.
   - Download button performs `apiClient.getBlob('/jobs/{id}/output/{file_id}')` → `URL.createObjectURL` → `<a download>` click → `revokeObjectURL`. (Reuse `apiClient` if it already has a blob helper; otherwise add one.)
2. Schemas (locked):
   ```ts
   export const JobOutputFileSchema = z.object({
     file_id: z.string(),
     name: z.string(),
     size: z.number().int().nonnegative(),
     sha256: z.string(),
     created_at: z.string().datetime(),
   });
   export const JobOutputListSchema = z.object({
     job_id: z.string(),
     files: z.array(JobOutputFileSchema),
   });
   ```
3. The panel polls every 5 s while `job.status === "running"`, and stops once status is terminal. (Streaming via the existing `/ws` event bus is a Tier-3 nicety; not required.)
4. The existing `downloadFile` helper that just `JSON.stringify`s the result (`JobsPage.tsx:185-189`) stays for the **result JSON** download but is renamed `downloadResultJson`. Output-file download is a separate code path.

### B4. Pull on `failed` and `cancelled` (gap C5)

`device/iagent_device/jobs/dispatcher.py:158-174`:

- Move `await self.puller.pull_outputs(job_id)` outside the `if status == "succeeded"` branch so it also runs for `status in {"failed", "cancelled"}`.
- Skip pulling if `JOB_RESULT` was never emitted (e.g., agent unreachable from the start) — gate on a `had_terminal_status` bool.
- Order: `pull_outputs` THEN `JOB_RESULT`. Today the order is reversed; flip it so the gateway has the files in DB before publishing `WSEventJobResult` to subscribers (web can then immediately render the file list). If pull fails, still emit `JOB_RESULT` and log an error-counter metric.

### B5. Puller backpressure (gap C6)

`device/iagent_device/files/puller.py:41-72`:

- Per file, maintain a `dict[int, asyncio.Event]` keyed by chunk index.
- Before sending chunk `N`, if `inflight >= 8`, `await` the lowest unacked chunk's event. Use `asyncio.wait_for` with 10 s timeout.
- On `FILE_PULL_ACK { status: "ERROR" }` for a chunk: cancel inflight, retry whole file from chunk 0 up to 3 times, then surface as `JOB_PROGRESS message="output file {name} failed to upload"` and continue with next file.
- This requires the gateway to ACK **per-chunk**, not only on `FILE_PULL_END`. Update `relay.go` to send `FILE_PULL_ACK` after each chunk write. (Spec §4.10 already implies per-chunk ACKs by saying "mirror §5", which has per-chunk ACKs.)

---

## 5. Workstream C — VNC end-to-end

### C1. `VNC_OPEN` request becomes async-ready (gaps V1, V2, V3)

**Decision (locked):** **synchronous wait-for-ready**, not the eventbus variant. Rationale: simpler client code, matches spec §9 step 7 ("POST response / noVNC config"), avoids exposing a new WS event type. Cost: the POST handler holds a connection up to 15 s.

**Concrete changes:**

1. `gateway/internal/httpapi/vnc_handler.go` (the POST handler):
   - After enqueueing `VNC_OPEN`, register a one-shot channel keyed by `session_id` on `deps.VNCRelay` (or a new `deps.VNCReady` map).
   - `select` on `<-readyCh` (resolved by `OnVNCOpened`) and `time.After(15 * time.Second)`.
   - On timeout: clean up the registration, return `504 Gateway Timeout` with `{ "error":"vnc_open_timeout" }`.
   - On error from device (`VNC_OPENED { status:"error" }`): return `502 Bad Gateway` with the device's `message`.
2. `gateway/cmd/gateway/main.go` `OnVNCOpened` handler — after recording the password, fire the registered channel with the password and `ws_url`.
3. **New response shape (locked):**
   ```jsonc
   // POST /api/v1/jobs/{id}/vnc → 201
   {
     "session_id": "01J...",
     "ws_url": "wss://gateway.example.com/ws/vnc/01J...?token=<short_lived_jwt>",
     "rfb_password": "ephemeral-base64",
     "ttl_secs": 300
   }
   ```
   - `ws_url` is **absolute** (scheme + host + path + query). Resolve scheme/host from request `Host` + `X-Forwarded-Proto` (or config `gateway.public_url`).
   - `?token=` **reuses the existing `session_token`** that `vnc_handler.go:45-60` already mints when creating the `vnc_sessions` row. Rationale: the token is already scoped to `(session_id, device_id, user_id)` per `05 §9`, has the right TTL, and is already validated by `/ws/vnc/{id}` on the device side; minting a second JWT just for the browser side adds a code path with no security gain. Tighten `vnc_handler.go:289-301` to: (a) accept `?token=<session_token>`, (b) verify it matches the `vnc_sessions.session_token` row, (c) verify `session_id` in URL matches the row, (d) verify `user_id` matches the JWT bearer if a user JWT is also present (defence in depth).
   - `rfb_password` is the secret returned by the agent; consumed once by `noVNC` then discarded.
4. **Web** `web/src/components/VNCPanel.tsx`:
   - Use the absolute `wsUrl` verbatim (no prefix, no rewrite).
   - **Subprotocol negotiation (locked):** prefer `iagent.novnc.v1` between browser ↔ gateway; fall back to the empty subprotocol if the noVNC library refuses custom subprotocols. The gateway MUST accept both — clients are not pinned to v1. Device ↔ gateway uses `iagent.session.v1` independently (no fallback needed; the device is our code).

### C2. Fix `vncrelay` byte pump (gap V4)

`gateway/internal/vncrelay/relay.go:209-217` — replace:

```go
msgType, reader, err := src.NextReader()
n, err := io.CopyBuffer(dst.UnderlyingConn(), reader, msgBuf)
```

with:

```go
msgType, reader, err := src.NextReader()
if err != nil { return err }
if msgType != websocket.BinaryMessage { return errBadMsgType }
writer, err := dst.NextWriter(websocket.BinaryMessage)
if err != nil { return err }
if _, err := io.CopyBuffer(writer, reader, msgBuf); err != nil { _ = writer.Close(); return err }
if err := writer.Close(); err != nil { return err }
```

Add a unit test in `gateway/internal/vncrelay/relay_test.go` that uses two `httptest.Server`s + `gorilla/websocket.Dial` to round-trip a 1 MiB pseudo-random byte stream through the pump in both directions and assert sha256 equality. This test must be in the standard `go test ./...` set.

### C3. Reconnect-friendly `GET /jobs/{id}/vnc` (gap V5)

`gateway/internal/httpapi/vnc_handler.go:233-269`:

- After loading the DB row, also look up the in-memory `deps.VNCRelay.Get(sessionID)`. If present, include the live `rfb_password`, `ws_url` (rebuilt with a fresh short-lived token), and `ttl_secs` in the response.
- If absent (gateway restart, session truly closed), return `404 Not Found` so the client knows to re-open via POST.

---

## 6. Workstream D — Login support

### D1. Agent emits `login_required` (gap L1 part 1)

**Decision (locked):** generic event channel on the agent runtime API rather than a dedicated `/login_required` route. This keeps the door open for future event types (`captcha_required`, `mfa_required`, `tos_required`) without churning the contract every time.

**New agent endpoint:**

```
POST /jobs/{job_id}/events
Body: { "type": "login_required" | "info" | ..., "origin"?: "https://...", "label"?: "Gmail", "screenshot_b64"?: "..." }
→ 202 Accepted { "event_seq": <int> }
```

- `event_seq` is monotonically increasing per `(job_id)` on the agent.
- Agent stores the last 32 events in memory under the job; not persisted.

**New agent client code:** in `agent/iagent_agent/runtime/executor.py`, when the browser navigation lands on a page matching a configured login-detection rule (heuristic: redirect to a `/login` path, presence of password input, or explicit skill hook), call the local event endpoint with `type="login_required"`, `origin = new URL(page.url).origin`, optionally a `screenshot_b64` (PNG ≤ 200 KiB).

**Detection (locked v1, intentionally simple):** only fire when the brain's tool layer explicitly invokes a `request_human_login` tool. Heuristic auto-detection is **out of scope** for v1; otherwise we churn the agent. Document the tool in `04-agent-container.md`.

**Device polling:** `device/iagent_device/jobs/dispatcher.py` already polls `GET /jobs/{id}` for status; extend the poll to also call `GET /jobs/{id}/events?since=<seq>` and forward each new event to the gateway as `JOB_LOGIN_REQUIRED` (or a future event-type frame). Poll interval stays at 1 s.

### D2. New tunnel frame `JOB_LOGIN_REQUIRED` (gap L1 part 2)

Add to `05-tunnel-protocol.md §4.3`:

| Type | Dir | Payload |
|------|-----|---------|
| `JOB_LOGIN_REQUIRED` | D→G | `{ job_id, event_seq, origin, label?, login_kind? }` |

- `event_seq` enables idempotency (gateway dedupes by `(job_id, event_seq)` like `JOB_PROGRESS`).
- `login_kind` is an optional hint: `"qr"` | `"form"` | `"unknown"` (default). When the agent knows the page is a QR-code login (e.g., WeChat / Alipay / 飞书), it sets `"qr"` so the web UI can adjust copy ("scan the QR code shown in the browser"). The agent does NOT need to detect this perfectly — `"unknown"` is always acceptable.
- **No screenshot is transmitted.** The QR code (or login form) is shown live inside the agent's browser; the user sees it via the VNC live view and scans/types directly. This is simpler, lower-latency, and avoids a stale-screenshot UX. Implementations MUST NOT add a screenshot field in v1.

**Implementation locations:**
- `gateway/internal/model/types.go` — add `FrameJobLoginRequired`, `JobLoginRequiredPayload`.
- `gateway/internal/tunnel/router.go` — register handler, dispatch to `OnJobLoginRequired`.
- `gateway/cmd/gateway/main.go` — handler:
  - Persist a row in a new `job_login_events` table (or reuse `job_events` if it exists in `06-data-model.md`; check before adding) with `(job_id, event_seq, origin, created_at)`.
  - Publish `WSEventLoginRequired { job_id, origin, label }` on `pubsub.JobTopic(job_id)`.
- Device: `device/iagent_device/jobs/dispatcher.py` emits the frame when the agent event arrives.

### D3. Gateway publishes `WSEventLoginRequired` (gap L1 part 3)

In `gateway/internal/model/ws_events.go` (or wherever `WSEventJobResult` lives), add:

```go
type WSEventLoginRequired struct {
    JobID  model.UUID `json:"job_id"`
    Origin string     `json:"origin"`
    Label  string     `json:"label,omitempty"`
    At     time.Time  `json:"at"`
}
const WSEventTypeLoginRequired = "job.login_required"
```

Subscribers on `/api/v1/ws?topic=job:<id>` receive this alongside existing `job.progress` / `job.result` events.

### D4. Web prompts user on `login_required` (gap L1 part 4)

**Decision (locked):** do **not** auto-open the VNC modal. Instead show a **persistent, prominent prompt** that the user must explicitly act on. Rationale: an auto-popup is intrusive and can interrupt the user mid-task; the QR-code login flow needs the user to be ready (phone in hand) anyway, so making them click is the correct UX gate.

`web/src/pages/JobsPage.tsx`:

1. Subscribe to the existing `/ws` channel for the active job.
2. On `event.type === "job.login_required"`:
   - **Add a sticky banner** at the top of the job detail card: `"Login required for {origin}. Open the remote browser to scan the QR code or sign in."` with a primary button `[Open Browser]`. The banner uses an attention-grabbing color (warning / accent) but does not block the page.
   - **Also raise a toast** with the same message and the same action button — toasts are dismissable; the banner is not, until the login is satisfied (job leaves `login_required` state, status changes, or user closes the VNC session after success).
   - **Do not** call `openVNC.mutate(...)` automatically. The user clicks `[Open Browser]`, which runs the existing manual flow.
   - When `login_kind === "qr"`, the banner copy switches to `"Scan the QR code shown in the remote browser to sign in to {origin}."` to set expectations.
   - If a VNC session is already open when the event fires, skip the banner and instead flash a small inline notice inside the VNC panel: `"Action needed: log in to {origin}"`.
3. **State clearing:** the banner is cleared when (a) a subsequent `JOB_PROGRESS` arrives whose `message` does not indicate login, OR (b) `JOB_RESULT` arrives, OR (c) the user explicitly dismisses with an X button (which records a "user-acknowledged" timestamp; if a new `login_required` arrives after that, the banner reappears).
4. **No localStorage preference toggle in v1.** Auto-open is permanently off; the prompt is permanently on. Revisit only if user feedback demands a per-user toggle.

### D5. `save-login` carries origin (gap L2)

**Concrete changes:**

1. **Agent**: add `GET /browser/active_origin` → `{ origin: "https://..." }` returning the origin of the topmost browser tab. (Reads from the existing camoufox/playwright context; trivial.)
2. **Device**: add a passthrough or call site in `device/iagent_device/creds/relay.py` exposed via the gateway request flow.
3. **Gateway** `POST /api/v1/vnc/{sid}/save-login`:
   - **Locked request shape:**
     ```jsonc
     { "label": "Gmail Personal", "origin": "https://accounts.google.com" }
     ```
   - `origin` is **required**, validated as a non-empty origin (`scheme://host[:port]`, no path, no query, no trailing slash). Reject 400 if missing or malformed.
   - The handler at `vnc_handler.go:113-121` removes the hard-coded `Origin: ""` and uses `req.Origin`.
4. **Web** `web/src/components/VNCPanel.tsx`:
   - Before showing the "Save login" prompt, call a new endpoint `GET /api/v1/jobs/{job_id}/browser/active_origin` (gateway proxies to device → agent).
   - Pre-fill the origin in the prompt; allow user to override (one input field).
   - Disable the "Save" button while the origin field is empty or invalid.

### D6. `CRED_CAPTURE` field rename (gap L3)

**Decision (locked):** rename **device + gateway** code to use `storage_state` (matching `05 §4.9`). Spec stays as-is. Rationale: source of truth is the spec; renaming code is mechanical.

**Concrete changes:**

- `device/iagent_device/creds/relay.py:125-133` — payload field `data` → `storage_state`.
- `gateway/cmd/gateway/main.go:248` and `gateway/internal/model/types.go` `CredCapturePayload` — field `Data` → `StorageState`, JSON tag `storage_state`.
- The value remains base64-encoded JSON; **add a `storage_state_encoding: "base64"` field** to the payload so future variants (raw, gzip+base64) can coexist. Locked v1 value: `"base64"`.

### D7. `CRED_CAPTURE_ACK` direction (gap L4)

`device/iagent_device/creds/relay.py:106-110, 135-138`:

- Replace the device's emission of `CRED_CAPTURE_ACK` on capture failure with an `ERROR` frame: `{ code: "CRED_CAPTURE_FAILED", message, ref_msg_id: <session_id-tagged> }`.
- The gateway already has a generic `OnError` handler; route it through to the user-facing layer as a toast on the live VNC panel (`Save login failed: {message}`).

---

## 7. Spec deltas (other documents that must be updated as part of this work)

| File | Section | Change |
|------|---------|--------|
| `05-tunnel-protocol.md` | §4.3 | Add `JOB_LOGIN_REQUIRED` row (D2). |
| `05-tunnel-protocol.md` | §4.9 | Add `storage_state_encoding: "base64"` field (D6). Clarify `CRED_CAPTURE_ACK` is G→D **only** (D7). |
| `05-tunnel-protocol.md` | §9 | Lock subprotocol names: browser↔gateway `iagent.novnc.v1`; device↔gateway `iagent.session.v1` (C1). |
| `04-agent-container.md` | HTTP API | Add `POST /jobs/{job_id}/events`, `GET /jobs/{job_id}/events?since=`, `GET /browser/active_origin` (D1, D5). |
| `06-data-model.md` | §1.5 `job_files` | Add `role text NOT NULL DEFAULT 'input' CHECK (role IN ('input','output'))` column + `idx_job_files_job_role` index (B1). |
| `06-data-model.md` | new §1.x | Add `job_login_events (job_id, event_seq, origin, label, login_kind, created_at)` table for audit / replay (D2). |
| `07-api.md` | §5 (jobs) | Add `GET /jobs/{id}/output`, `GET /jobs/{id}/output/{file_id}` (B2). Update `POST /jobs/{id}/vnc` response (C1). Add `GET /jobs/{id}/browser/active_origin` (D5). |
| `07-api.md` | §9 (WS) | Add `job.login_required` event type (D3). |
| `09-web-ui.md` | Job detail | Document new Output panel (B3) and login-required auto-open (D4). |

These deltas are part of the implementation PR(s); merging code without spec updates is a regression.

---

## 8. Sequencing & dependency graph

```
A1 (workspace mount)  ──┬──► B1 (job_files)  ─► B2 (download API) ─► B3 (web UI)
                        │                                                 ▲
                        └────────► B4 (failed-job pull) ──────────────────┘
                                                                          │
                                                                          │ (independent)
A2 (push inputs) ─────────────────────────────────────────────────────────┘

C2 (relay framing)  ─► C1 (async ready)  ─► C3 (reconnect)  ◄── prerequisite for any VNC work in D

D6 (CRED_CAPTURE rename) ─► D7 (ACK direction)
                                                          (D-block needs C-block done)
D1 (agent events) ─► D2 (frame) ─► D3 (gateway WS event) ─► D4 (web auto-open) ─► D5 (save-login origin)
```

**Recommended PR cadence:**

1. **PR1 — Workstream A** (file-system fixes). Small, mechanical, unblocks B and Tier-3 testing of the existing `FILE_PULL_*` plumbing.
2. **PR2 — Workstream B** (output download API + UI). Builds on PR1.
3. **PR3 — Workstream C** (VNC end-to-end). Independent of A/B; can run in parallel after PR1 lands.
4. **PR4 — Workstream D1–D4** (login event chain). Depends on PR3.
5. **PR5 — Workstream D5–D7** (save-login origin + schema reconciliation). Depends on PR4.

Each PR ships its own spec updates per §7.

---

## 9. Testing matrix

Every workstream has both unit and end-to-end coverage. End-to-end tests run against the production code paths (no mocks in the chain) per `AGENTS.md`.

| ID | Test kind | Description |
|----|-----------|-------------|
| A1-E2E | E2E | Submit job that writes `output/a.txt`; assert host file under `{data_dir}/work/workspaces/{id}/output/a.txt`. |
| A2-E2E | E2E | Submit job with 600 KiB input; assert agent reads byte-identical content from `/work/workspaces/{id}/inputs/`. |
| B1-Unit | Unit | `relay.OnFilePullEnd` inserts `job_files` row with `role="output"` and correct `user_id`. |
| B2-E2E | E2E | After A1-E2E, `GET /jobs/{id}/output` lists the file; `GET /jobs/{id}/output/{file_id}` returns body with matching sha256. Cross-tenant fetch returns 404. |
| B3-Web | Component | Vitest + RTL: panel renders rows, download button triggers blob fetch. |
| B4-E2E | E2E | Job that exits non-zero after writing `output/partial.log`; assert file still pulled and `JOB_RESULT` arrives with `status="failed"`. |
| B5-Unit | Unit | Puller with mocked outbox: 20-chunk file; only ≤ 8 in flight at any time; ACK ERROR triggers retry. |
| C1-E2E | E2E | POST `/jobs/{id}/vnc` returns absolute `wss://`, `rfb_password` non-empty, `ttl_secs > 0`. Browser noVNC handshake completes. |
| C2-Unit | Unit | `vncrelay` round-trips 1 MiB random bytes; sha256 matches both directions. |
| C3-E2E | E2E | After C1-E2E, kill the browser tab; reopen via `GET /jobs/{id}/vnc`; receive same `session_id` and a fresh `ws_url`; reconnect succeeds. |
| D1-Unit | Unit | Agent `POST /jobs/{id}/events` returns monotonic `event_seq`; `GET /jobs/{id}/events?since=N` filters. |
| D2-D3-E2E | E2E | Agent emits `login_required` with `login_kind="qr"`; web `/ws` subscriber receives `job.login_required` with the same `login_kind`. |
| D4-Web | Component | Mock WS event; assert sticky banner + toast both render with `[Open Browser]` button; assert VNC panel does **not** open automatically. Click button → `openVNC.mutate` invoked. |
| D4-QR-E2E | E2E | Job that triggers `login_kind="qr"`; user clicks `[Open Browser]`; VNC live view shows the agent's browser at the QR page; simulated post-scan navigation clears the banner. |
| D5-E2E | E2E | In a live VNC session, click "Save login" with origin pre-filled; assert `browser_credentials` row has correct `origin`; submit follow-up job with that `credential_id`; agent loads page already authenticated. |
| D6-Unit | Unit | `CRED_CAPTURE` JSON has `storage_state` and `storage_state_encoding="base64"`. |
| D7-Unit | Unit | Capture failure path emits `ERROR { code:"CRED_CAPTURE_FAILED" }`, **never** `CRED_CAPTURE_ACK`. |

E2E tests run against `iagent_e2e` DB per `AGENTS.md`. Never run against `iagent`.

---

## 10. Out of scope / non-goals

- **Heuristic login detection.** v1 only fires `login_required` when the brain explicitly calls a tool. No DOM scanning, no URL pattern matching at the agent layer. Document in `04-agent-container.md` as a known limitation.
- **Range requests / resumable downloads** for output files. Files are typically < 50 MiB and a full GET is acceptable.
- **Per-chunk progress events** for output uploads visible in the web UI. The puller becomes more reliable (B5) but the UI just shows "uploading..." until done.
- **Re-encrypting existing `browser_credentials` rows** that have empty `origin`. Migration only protects new rows.
- **Multi-user simultaneous VNC viewing of the same session.** First connection wins; second is rejected with `4002 SUPERSEDED`. (Already the spec's behaviour.)
- **Mobile / touch noVNC client.** Desktop browser only.

---

## 11. Resolved design decisions

The five questions raised in earlier drafts are all resolved. Recorded here for future reviewers.

1. **`job_files.role` column — required (locked in B1).** Verification of `06-data-model.md §1.5` confirmed the table currently has only `(job_id, file_id)`; there is no way to distinguish input from output files. The migration adds a `role text NOT NULL DEFAULT 'input' CHECK (role IN ('input','output'))` column. This satisfies the product requirement: the local device specifies an output directory, the device pulls everything in it to the cloud, and the cloud surfaces only `role='output'` files for user download.

2. **noVNC subprotocol — fallback supported (locked in C1 step 4).** The gateway accepts both `iagent.novnc.v1` and the empty subprotocol on the browser ↔ gateway socket. The web client prefers v1 and falls back to empty if the noVNC library refuses. Either path produces a working remote-login UX, which is the only product requirement.

3. **Browser-side `?token=` — reuse `session_token` (locked in C1 step 3).** The token already minted for the device side is reused for the browser side. It is scoped to `(session_id, device_id, user_id)` per `05 §9`, has the right TTL, and is sufficient to gate access to the per-session VNC relay. This keeps the cookie-relay flow simple: the user logs in via VNC, captured cookies travel via the existing `CRED_CAPTURE` path, and re-injection on subsequent jobs uses `CRED_PUSH` (already working — see audit 10 §3.5). No new JWT scope is needed for this product flow.

4. **Login screenshots — not transmitted (locked in D2).** The QR code (or login form) is rendered live in the agent's browser; the user sees and scans it directly through the VNC live view. There is no screenshot field on `JOB_LOGIN_REQUIRED` and no thumbnail endpoint. Rationale: the QR-login product requirement is "user can scan a QR to log in"; the lowest-latency, freshest, and simplest implementation is the live VNC stream the system already supports. A screenshot would be stale by the time the user sees it, and QR codes refresh frequently.

5. **Auto-open VNC — disabled; persistent prompt instead (locked in D4).** When `login_required` fires the web UI shows a sticky banner + a toast, both with an `[Open Browser]` button. The user clicks to open VNC. No automatic popup. Rationale: QR-code logins require the user to be ready (phone in hand); forcing the browser open before that is intrusive. The persistent banner ensures the user cannot miss the prompt even if they ignore the toast.

---

## 12. Definition of done

A workstream is "done" when:

- All listed concrete code changes are merged.
- All listed tests in §9 pass in CI (`go test ./...`, `pytest`, `vitest run`).
- Lint/typecheck pass per `AGENTS.md` for every touched module.
- The relevant spec deltas from §7 are merged in the same PR(s).
- A manual smoke test of the user-visible flow (submit job → see output files → open VNC → save login → submit follow-up job that uses that login) succeeds end-to-end against a fresh `dev-up` stack.
- Audit 10's corresponding gap row is annotated with the PR number that closed it; once all rows are closed, audit 10 is marked **resolved** in `docs/audit/README.md`.
