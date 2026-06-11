# Gateway Redeploy Required: Device Tunnel Keeps Disconnecting

Date: 2026-06-11
Status: OPEN — gateway binary needs rebuild + restart

---

## 1. Symptom

Device connects successfully, receives HELLO_ACK, but disconnects after ~60s with:

```
websockets.exceptions.ConnectionClosedError: sent 1011 (internal error) keepalive ping timeout; no close frame received
```

This repeats in a loop — the device reconnects, works briefly, then disconnects again. Frames like AGENT_CREATE do get through during the short connection windows, but any long-running operation (job dispatch, progress polling) is unreliable.

---

## 2. Root Cause

The device's `websockets>=13.0` Python library sends automatic WebSocket ping frames every `ping_interval=30s`. It expects pong responses within `ping_timeout=20s`. If no pong arrives, the client closes the connection with code 1011.

The gateway uses `gorilla/websocket` which **does** auto-reply to pings by default (when no `PingHandler` is set). But the gateway also has `SetReadDeadline(30s)` added in the bfd66e7 fix. If the device sends a ping, gorilla/websocket auto-pongs, but the connection is closed before the pong arrives because:

- `SetReadDeadline(30s)` resets the read deadline on each `ReadMessage()`
- If no application-level frame arrives for 30s (between the device's manual PINGs), the read deadline fires
- The connection is closed by the gateway before the device's pong timeout expires

**The commit bfd66e7 added WS timeouts on the gateway but the production gateway binary on `deepwitai.cn` has NOT been rebuilt or restarted since that commit.** The running gateway is either:
- Missing the PongHandler (so never touches the liveness timer, never keeps the connection alive), OR
- Missing the read deadline reset in the PongHandler (so the 30s read deadline fires and kills the connection)

---

## 3. Action Required

### 3.1 Rebuild the gateway binary

```bash
cd /root/projects/oneClickAgent/gateway
git pull origin main
go build -o bin/gateway ./cmd/gateway && go vet ./...
```

### 3.2 Restart the gateway

```bash
fuser -k 42080/tcp; sleep 1
nohup env \
  IAGENT_CORS_ORIGINS='*' \
  IAGENT_CRED_KEY='AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=' \
  IAGENT_DB_URL='postgres://iagent:iagent_dev_password@localhost:5432/iagent?sslmode=disable' \
  IAGENT_ENV='development' \
  IAGENT_FILE_STORE='local:/tmp/iagent-files' \
  IAGENT_HTTP_ADDR=':42080' \
  IAGENT_JWT_SECRET='dev-jwt-secret-at-least-32-characters-long!!' \
  IAGENT_LOG_FORMAT='text' \
  IAGENT_LOG_LEVEL='debug' \
  IAGENT_WEB_DIST_DIR='/root/projects/oneClickAgent/web/dist' \
  /root/projects/oneClickAgent/gateway/bin/gateway \
  > /tmp/gateway.log 2>&1 &
```

### 3.3 Verify

```bash
# Gateway health
curl -s -o /dev/null -w "%{http_code}" http://localhost:42080/healthz

# After restart, device should reconnect and STAY connected
# Check device log on the local machine for no more "read loop exited" every 60s
tail -f /tmp/device-cloud15.log | grep "read loop exited"
# (should see NO output after gateway restart)
```

---

## 4. Why This Matters

Without a stable tunnel:
- Agents can be created but job dispatch is unreliable
- Job progress/results may be lost (frames dropped during disconnect windows)
- VNC relay fails mid-session
- File push/pull is interrupted

The device-side reconnect + logging fix (21c3ef7) makes disconnects visible and recovery fast, but the root cause is the gateway not replying to WebSocket pings.
