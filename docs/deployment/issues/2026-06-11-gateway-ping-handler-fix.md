# Gateway Fix: SetPingHandler for Device Keepalive Pings

Date: 2026-06-11
Status: OPEN — code fix identified, needs gateway rebuild + redeploy

---

## 1. Symptom

Device disconnects every ~50 seconds with:
```
websockets.exceptions.ConnectionClosedError: sent 1011 (internal error) keepalive ping timeout; no close frame received
```

The device's `websockets>=16.0` Python library sends WS-level ping frames every 30s
(`ping_interval=30`) and expects pong responses within 20s (`ping_timeout=20`).
The gateway never responds with a pong, so the device closes the connection after 50s.

This is a separate mechanism from the device's application-level JSON PING/PONG
frames (sent every 15s via the heartbeat loop), which work fine.

---

## 2. Root Cause

`gorilla/websocket v1.5.3` **should** auto-pong by default when no `PingHandler` is set,
but it's not working in the deployed binary. The most likely causes:

- **A)** The `SetReadDeadline(30s)` on the read pump causes `ReadMessage()` to timeout
  on the deadline before gorilla/websocket can process the incoming ping control frame
  and write the auto-pong.

- **B)** The `SetWriteDeadline(10s)` set by the write pump expires, and gorilla/websocket's
  internal auto-pong (`c.write(PongMessage, ...)`) fails silently because the write
  deadline on the underlying `net.Conn` is in the past.

Regardless of the exact internal cause, the fix is to **not rely on gorilla/websocket's
auto-pong** and instead set an explicit ping handler.

---

## 3. Code Fix

### File: `gateway/internal/tunnel/device_conn.go`

In `StartReadPump()` (around line 93), add a `SetPingHandler` alongside the existing `SetPongHandler`:

```go
// WebSocket-level ping/pong as secondary liveness check (§3).
c.ws.SetPingHandler(func(appData string) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    err := c.ws.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
    if err != nil {
        c.logger.Warn("write pong failed", "error", err)
    }
    c.touch()
    return err
})

c.ws.SetPongHandler(func(appData string) error {
    c.touch()
    return nil
})
```

The `c.mu.Lock()` is required because `WriteControl` writes to the connection and must be serialized with the write pump's `WriteMessage` calls (gorilla/websocket rule: only one concurrent writer).

### Build & Restart

```bash
cd /root/projects/oneClickAgent/gateway
git pull origin main
go build -o bin/gateway ./cmd/gateway && go vet ./...

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

---

## 4. Verification

After restart, check device log:
```bash
tail -f /tmp/device-cloud15.log | grep "read loop exited"
# Should see NO output — tunnel stays connected
```

The device should receive HELLO_ACK and maintain a stable connection with no more
`1011 keepalive ping timeout` errors.
