# Browser-Ready Signal: How the Web UI Knows When to Open VNC

Date: 2026-06-13
Status: OPEN — analysis & design

---

## 1. Problem Statement

A browser-driving job (e.g., "open xiaohongshu.com and wait for login") runs opencode
which launches CloakBrowser in headed mode on Xvfb display :99. The browser is visible
via VNC once the user clicks "Open Browser" — but **the web UI has no way to know WHEN
the browser page has loaded and VNC would show something useful.**

Currently the user must guess when to click, often hitting `vnc_open_timeout` because
VNC was opened before x11vnc started, or opening VNC to an empty X display before the
browser launched. There is no "browser is ready, login now" prompt in the UI.

---

## 2. Desired User Experience

1. User submits a job
2. Job runs, opencode launches the browser
3. The web UI shows a **"Browser Ready — Open VNC to Login"** button/notification
4. User clicks it → VNC opens → user sees the login page → user logs in
5. Job continues after login detected

---

## 3. Existing Infrastructure (what we already have)

### 3.1 Agent event stream
`dispatcher.py:158-174` polls `GET /jobs/{job_id}/events?since=N` on the agent container.
This endpoint returns a list of events with `event_seq` and `type`.

The `JOB_LOGIN_REQUIRED` event is already handled — if `evt_type == "login_required"`,
a `JOB_LOGIN_REQUIRED` frame is sent to the gateway.

### 3.2 JOB_PROGRESS frames
The dispatcher sends `JOB_PROGRESS` with `status`, `percent`, `message`. The web UI
already renders these. A browser_ready signal could be a `JOB_PROGRESS` with a
distinct status field.

### 3.3 VNC lifecycle
`VNC_OPEN → VNC_OPENED → bridge → VNC_CLOSE` is a separate path from job events.
The bridge is started on-demand, not automatically.

---

## 4. Challenges

### C1. Signal origin — where does "browser ready" come from?
The browser launch happens inside the opencode process, which runs a Python script
(using cloakbrowser/Playwright Sync API). The script detects page load via
`page.wait_for_load_state()` or similar. This detection happens **inside the opencode
subprocess**, not in the agent server.

**Challenge**: How does the opencode subprocess communicate back to the agent server
that the browser is ready?

### C2. Event delivery
The event must travel: opencode script → agent server → device dispatcher → tunnel →
gateway → web UI WebSocket. Each hop adds latency and potential loss if the tunnel
drops.

### C3. Tunnel instability
The tunnel disconnects every ~30s (pending gateway `SetPingHandler` fix). If a
`browser_ready` event is emitted during a disconnect window, it sits in the outbox
and may arrive late (or not at all before the 2-min login window expires).

### C4. Timing: browser launches may take 5-30 seconds
A cloakbrowser cold start (especially first launch) can take 5-30 seconds. The
user needs to see something useful during this wait — not just a loading spinner
with no indication that the browser is initializing.

### C5. Multiple browser launches per job
A job may launch the browser, close it, launch again. Each launch should emit
a new event. The web UI should handle "browser closed → re-launched" correctly.

### C6. VNC auto-open vs manual
Should the web UI auto-open VNC when `browser_ready` arrives? Or show a button?
Auto-open is convenient but may surprise the user. A button is more predictable
but adds an extra click.

### C7. Race between VNC open and browser ready
If VNC is opened BEFORE the browser launches (manually), the user sees the empty
Xvfb desktop. When the browser launches, the VNC stream updates live. This is
the ideal case — but requires x11vnc to already be running, which it currently
is only started by the VNC bridge.

---

## 5. What We Can Do

### 5.1 Option A: Browser script emits "browser_ready" via agent HTTP callback
The agent server already has a `callback_url` parameter in `create_job` payload
(`agentclient/client.py:40-41`). The opencode script could POST to
`http://localhost:8090/jobs/{job_id}/events` with a structured event.

**Implementation**:
1. Add a `/jobs/{job_id}/events` POST endpoint on the agent server that accepts
   events from the brain (currently the brain just runs a subprocess).
2. OpenCode brain adapter: after the script emits "Page loaded", have the script
   POST the event back to the agent.
3. The agent stores events in an in-memory list (already done via event_seq in
   status response).
4. Dispatcher polls events, sees `browser_ready`, emits `JOB_LOGIN_REQUIRED`
   (or a new `JOB_BROWSER_READY` frame type).
5. Web UI receives the frame and shows a prominent "Open VNC to Login" button.

**Complexity**: Medium. Needs brain/script cooperation. The opencode script
would need to know the agent's callback URL.

### 5.2 Option B: Monitor via opencode stderr parsing
The brain adapter already streams opencode stderr (`_stream_stderr`). When the
opencode script outputs a known pattern (e.g., `[BROWSER_READY]`), the brain
adapter triggers an event.

**Implementation**:
1. In the skill's Python script template, add a `print("[BROWSER_READY]")` after
   page load.
2. `brain_opencode._stream_stderr()` detects this pattern and calls a callback
   on the `ProgressEmitter`.
3. The executor converts this to an event in the job's event store.
4. Dispatcher polls, finds event, forwards to gateway.

**Complexity**: Low. Just need a marker pattern in the script + detection in stderr.

### 5.3 Option C: VNC bridge auto-starts with browser proxy
Instead of relying on the user to click "Open Browser", auto-open VNC when the
browser is detected (via the background `browser.launch_headless` in `/vnc/start`
that runs when user clicks "Open Browser").

**Implementation**:
1. The dispatcher detects `browser_ready` event.
2. Dispatcher sends a `VNC_OPEN` request to the gateway (or gateway auto-creates
   a VNC session).
3. Gateway sends `VNC_OPEN` to device → bridge starts → web UI auto-connects.

**Complexity**: Medium-High. Requires cross-module changes (dispatcher ↔ VNC).

### 5.4 Immediate low-effort improvement: JOB_PROGRESS with browser status
Even without a dedicated event, the opencode script can output progress messages
that the dispatcher forwards. Currently `_guess_progress` only parses percentages.
We can extend it to recognize status messages like "Browser launched, waiting for
login..." and include them in JOB_PROGRESS.message.

**Implementation**:
1. Support progress messages beyond just percentage.
2. Web UI renders the message prominently (e.g., a status banner).
3. When message contains "Browser" or "login", show a "Open VNC" button next to it.

**Complexity**: Low. Changes to `_stream_output` parsing + web UI message display.

---

## 6. Recommended Approach

**Short-term** (minimal changes, immediate effect):
- Option D: Extend JOB_PROGRESS messages to include browser status text.
  The skill's Python script already outputs descriptive messages ("Navigating to
  xiaohongshu.com...", "Page loaded. Waiting up to 120s for login...").
  The dispatcher's `_guess_progress` ignores these. Add a message field to
  JOB_PROGRESS that includes the latest non-percent output line.
- Web UI shows this message + an "Open VNC" button when the message suggests
  browser activity.

**Medium-term** (dedicated event, better UX):
- Option B: Parse opencode stderr for `[BROWSER_READY]` marker → emit
  `JOB_LOGIN_REQUIRED` or new `JOB_BROWSER_READY` frame → web UI shows
  prominent notification.
- Pre-start x11vnc on browser_ready (call `/vnc/start` from dispatcher) so
  the VNC is viewable immediately when the user clicks.

**Long-term** (seamless):
- Option C: Automatic VNC open when browser_ready detected. Gateway creates
  VNC session, device bridges, web UI auto-connects noVNC. User sees browser
  without any extra clicks.

---

## 7. Files to Modify (Option B — recommended medium-term)

| Module | File | Change |
|--------|------|--------|
| Agent — brain | `agent/iagent_agent/adapter/brain_opencode.py` | Detect `[BROWSER_READY]` in stderr, call emit callback |
| Agent — executor | `agent/iagent_agent/runtime/executor.py` | Store browser_ready event in job events |
| Agent — server | `agent/iagent_agent/server.py` | Expose event in `/jobs/{id}/events` response |
| Device — dispatcher | `device/iagent_device/jobs/dispatcher.py` | Handle `browser_ready` event type, emit to gateway |
| Gateway — hub | `gateway/internal/tunnel/hub.go` | Handle new frame type if needed, or reuse JOB_LOGIN_REQUIRED |
| Web — UI | `web/src/` | Show "Open VNC" notification on browser_ready event |
| Agent — skill | CLAUDE skill template | Add `print("[BROWSER_READY]")` after page load |

---

## 8. Edge Cases & Considerations

| Edge Case | Handling |
|-----------|----------|
| Browser fails to launch (Xvfb dead, binary missing) | brain_opencode emits `[BROWSER_ERROR]` instead, web UI shows error |
| Tunnel drops before event arrives | Event stored in outbox, delivered on reconnect (existing pattern) |
| VNC opened manually before browser_ready | VNC shows empty Xvfb desktop; browser appears when launched (works today) |
| Job reuses existing browser context | No new browser_ready — UI should already show VNC option from previous event |
| Multiple browser launches in one job | Each launch emits a fresh event with incremented seq, UI updates accordingly |
| Browser launched but VNC port busy | Bridge detects ConnectionRefused, retries with backoff, emits error to UI |
