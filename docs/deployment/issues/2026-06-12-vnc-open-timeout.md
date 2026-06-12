# Diagnosis Runbook: "vnc_open_timeout" when clicking "Open Browser"

Date: 2026-06-12
Status: OPEN — local-side (device + agent container) investigation
Audience: on-server / on-device agent (keep steps literal; copy-paste commands)

---

## 1. Symptom

- A browser-driving job is `running` in the cloud UI.
- User clicks **"Open Browser"** to watch the agent's browser over VNC.
- The web UI shows **`vnc_open_timeout`** instead of the live screen.

---

## 2. How VNC open is SUPPOSED to work

```
[web] click "Open Browser"
   POST /jobs/{id}/vnc
[gateway] create session -> send VNC_OPEN frame to device
          -> WaitReady(40s)   (was 15s; bumped on cloud side 2026-06-12)
[device]  handle_vnc_open():
          1. get docker client for agent_id        (vncbridge/bridge.py:45)
          2. HTTP POST agent /vnc/start             (bridge.py:50)
                -> agent starts Xvfb + x11vnc (~1s)
                -> agent ALSO launches the browser headed  (server.py:249-254)
          3. reply VNC_OPENED {status:ready, rfb_password}  (bridge.py:56)
          4. dial gateway /session/{id} and bridge RFB bytes (bridge.py:62)
[gateway] WaitReady unblocks -> returns ws_url + password
[web]     noVNC connects -> live browser visible
```

`vnc_open_timeout` means the gateway received **NO** `VNC_OPENED` frame (neither
`ready` nor `error`) within its wait window. So the device either never replied, or
replied too late.

---

## 3. Root-cause analysis (why a pure timeout happens)

A *pure timeout* (not an error message) narrows to these causes:

| # | Cause | Where | Why it times out |
|---|-------|-------|------------------|
| C1 | **`/vnc/start` blocks > gateway window** while launching the browser | agent `server.py:249-254` (`launch_headless`) | Device's agent-HTTP client timeout is **30s** (`agentclient/client.py:13`); the gateway used to wait only **15s**. A slow Chromium (cloakbrowser) cold start, or a missing binary triggering a multi-hundred-MB download, makes `/vnc/start` take >15s. Device replies too late. |
| C2 | **`get_client(agent_id)` returns None** | device `vncbridge/bridge.py:45-47` | The handler `return`s WITHOUT sending any frame (the error branch at line 68 only runs for exceptions). Guaranteed silent timeout. |
| C3 | **Device never received VNC_OPEN** | tunnel | If the tunnel dropped/reconnected at that moment the frame may be lost. |
| C4 | **x11vnc / Xvfb failed to start** but `/vnc/start` still hung | agent `browser/manager.py:223-253` | x11vnc binary missing or port busy; `subprocess.Popen` + `time.sleep(1)` may return but then browser launch hangs. |

NOTE: a 409 `NO_ACTIVE_JOB`, a 400 `VNC_DISABLED`, or an exception inside `/vnc/start`
produce an **error reply**, which surfaces a *different* message (e.g. "VNC session
error: ...") — NOT a timeout. So if you literally see `vnc_open_timeout`, focus on
C1–C4, with **C1 the most likely**.

---

## 4. Set up logging (do this first if logs are missing)

### 4.1 Device log
The device should already write to a log file when started (see project AGENTS.md, e.g.
`/tmp/device-cloud15.log`). If it is NOT logging at debug level, restart it with:

```bash
# stop existing device
ps -ef | grep iagent_device | grep -v grep | awk '{print $2}' | xargs -r kill -9

# restart with debug logging captured to a file (adjust data dir / gateway URL to yours)
# IMPORTANT: run under `sg docker` for Docker socket access
python3 -c "
import subprocess
p = subprocess.Popen(
  ['sg','docker','-c',
   'DOCKER_HOST=unix:///var/run/docker.sock '
   'IAGENT_DEVICE_DATA_DIR=/tmp/iagent-cloud-new '
   'IAGENT_GATEWAY_URL=https://deepwitai.cn/aiproduct '
   'IAGENT_LOG_LEVEL=DEBUG '
   'exec <PATH_TO_DEVICE_VENV>/bin/python -m iagent_device run'],
  stdout=open('/tmp/device-vnc.log','w'), stderr=subprocess.STDOUT,
  start_new_session=True)
print('PID', p.pid)
"
```

If the device code does not honor `IAGENT_LOG_LEVEL`, add at process start (temporary):
`logging.basicConfig(level=logging.DEBUG)`.

### 4.2 Agent container log
The agent logs to stdout. Stream it (replace `<name>` with the busy agent):

```bash
sg docker -c "docker logs -f agent-<name>" 2>&1 | grep -vi "healthz\|/status"
```

To find which container holds the running job (its name / port):

```bash
sg docker -c "docker ps --filter 'label=iagent.pool=true' --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'"
```

---

## 5. What to observe (reproduce the click, watch in real time)

Open three terminals BEFORE clicking "Open Browser":

1. Device log:    `tail -f /tmp/device-vnc.log | grep -i "vnc\|session"`
2. Agent log:     `sg docker -c "docker logs -f agent-<name>"`
3. A timer you control in step 6.3.

Then click **Open Browser** in the UI and watch the order of events:

- Device log SHOULD show: received `VNC_OPEN`, then a `/vnc/start` call, then `VNC_OPENED`.
- Agent log SHOULD show: `VNC stack started display=:99 port=5901`, then browser launch lines.

Record WHERE the sequence stalls or stops.

---

## 6. How to check / find the reason (step by step)

### 6.1 Is the cloakbrowser binary present? (tests C1 download-loop)
```bash
sg docker -c "docker exec agent-<name> sh -c 'ls -la /work/.cloakbrowser/chromium-*/chrome'"
```
- Missing  => C1 (binary download on first launch makes /vnc/start hang). 
- Present  => binary is fine; check launch speed (6.3).

### 6.2 Did the device get a docker client for the agent? (tests C2)
```bash
grep -i "vnc" /tmp/device-vnc.log | tail -20
```
- If you see "received VNC_OPEN" but then NOTHING (no /vnc/start, no VNC_OPENED) => C2
  (`get_client` returned None and the handler returned silently).
- If you DON'T even see "received VNC_OPEN" => C3 (frame never arrived).

### 6.3 Time the /vnc/start call directly (tests C1 / C4)
While the job is still `running` (executor busy), call the agent directly and time it:
```bash
sg docker -c "docker exec agent-<name> sh -c 'time curl -s -X POST http://localhost:8090/vnc/start'"
```
- Takes < 5s and returns `{rfb_port, rfb_password}` => agent side is fine; suspect C3 / tunnel.
- Takes 15–40s before returning => C1 (slow browser cold start). The bumped 40s gateway
  timeout should now cover it; re-test from the UI.
- Hangs > 40s or returns `409 NO_ACTIVE_JOB` => the job is no longer busy (browser already
  done) OR C4. Check `state.executor.busy` by re-checking the job is actually running.
- Returns 400 `VNC_DISABLED` => the agent was started without VNC enabled (env flag).

### 6.4 Is x11vnc actually running after the call? (tests C4)
```bash
sg docker -c "docker exec agent-<name> sh -c 'ps aux | grep -E \"x11vnc|Xvfb\" | grep -v grep'"
sg docker -c "docker exec agent-<name> sh -c 'ss -ltnp | grep 5901 || netstat -ltnp | grep 5901'"
```
- No x11vnc / no listener on 5901 => C4 (VNC server failed to start; check binary + display).

### 6.5 Tunnel stability (tests C3)
```bash
grep -i "disconnect\|reconnect\|no close frame\|read error" /tmp/device-vnc.log | tail
```
- Disconnect right around the click => C3 (frame lost during reconnect).

---

## 7. Solutions per case

### C1 — /vnc/start blocks on browser launch (MOST LIKELY)
1. **Pre-stage the cloakbrowser binary** so launch is instant (no download):
   ```bash
   # copy the host binary into the bind-mounted work dir used by containers
   cp -a /home/<user>/.cloakbrowser/chromium-<ver> /tmp/iagent-cloud-new/work/.cloakbrowser/
   # (or into a running container)
   sg docker -c "docker cp /home/<user>/.cloakbrowser/chromium-<ver> agent-<name>:/work/.cloakbrowser/"
   ```
2. The **cloud gateway timeout has been bumped to 40s** (was 15s) so a normal cold
   start now fits. Re-test from the UI.
3. **Best fix (code, separate change):** decouple browser launch from `/vnc/start` so
   the agent starts x11vnc and replies `VNC_OPENED` immediately, then launches the
   browser in the background. RFB becomes viewable within ~1s regardless of browser
   start time. (File: `agent/iagent_agent/server.py:241-255`.)

### C2 — get_client returns None (silent no-reply)
1. Confirm the agent_id the gateway sent matches a container the device tracks:
   ```bash
   grep -i "vnc_open\|agent_id" /tmp/device-vnc.log | tail
   sg docker -c "docker ps --filter 'label=iagent.pool=true' --format '{{.Names}}'"
   ```
2. If the device's in-memory docker client map is stale (e.g. after a device restart
   while containers kept running), restart the device so it re-discovers running
   containers, then re-run the job.
3. **Code hardening (separate change):** in `vncbridge/bridge.py:46-47`, send a
   `VNC_OPENED {status:"error", error:"agent not reachable"}` frame instead of a silent
   `return`, so the UI shows a real error rather than a timeout.

### C3 — VNC_OPEN never arrived / tunnel drop
1. Verify the tunnel is healthy:
   ```bash
   grep -i "tunnel\|HELLO\|reconnect" /tmp/device-vnc.log | tail
   ```
2. Ensure `ping_interval=30` is set on the tunnel client (known fix for 2-min drops).
3. Re-click "Open Browser" once the tunnel is stable.

### C4 — x11vnc / Xvfb failed
1. Check the binaries exist in the image:
   ```bash
   sg docker -c "docker exec agent-<name> sh -c 'which Xvfb x11vnc'"
   ```
   Missing => rebuild the agent image (`agent/Dockerfile`) with `xvfb` and `x11vnc`.
2. Check display `:99` is up:
   ```bash
   sg docker -c "docker exec agent-<name> sh -c 'DISPLAY=:99 xdpyinfo | head -3'"
   ```
3. Port 5901 busy from a stale session => `vnc/stop` then retry, or recreate the container.

---

## 8. What to report back
1. Step 6.1 — binary present? (yes/no)
2. Step 6.2 — device log: did it log "received VNC_OPEN" and "/vnc/start"?
3. Step 6.3 — measured duration of the direct `/vnc/start` call.
4. Step 6.4 — is x11vnc listening on 5901 afterward?
5. Step 6.5 — any tunnel disconnect around the click?

These five map directly to C1–C4 in §3 and the fixes in §7.

---

## 9. Cloud-side change already applied (for reference)
`gateway/internal/httpapi/vnc_handler.go` — `WaitReady` timeout raised from **15s → 40s**
so it exceeds the device's 30s agent-HTTP client timeout. Rebuild + restart the gateway:
```bash
cd gateway && go build -o bin/gateway ./cmd/gateway && go vet ./...
```
