# Cloud-Side Inspection: Device Tunnel Disconnects Every ~30s

Date: 2026-06-11
Status: OPEN — needs gateway log inspection

---

## 1. Symptom

After the latest gateway redeploy, the device tunnel still disconnects every ~30 seconds.
Error changed from `1011 keepalive ping timeout` (before redeploy) to `no close frame received or sent` (after redeploy).

Device log pattern:
```
22:23:43  received HELLO_ACK from gateway (session_id=..., heartbeat_s=15)
22:24:13  read loop error              ← exactly 30s later
22:24:13  read loop exited, connection=alive
22:24:13  heartbeat send failed, exiting heartbeat loop
22:24:13  connection closed gracefully, reconnecting immediately
22:24:13  outbox flushed after reconnect
22:24:13  received HELLO_ACK from gateway (next session)
```

The device sends application-level PING frames every 15s (heartbeat), but the connection drops at exactly 30s intervals — matching the gateway's `readTimeout = 30 * time.Second`.

---

## 2. Code Analysis (gateway side)

### 2.1 Read pump — correct on paper

`gateway/internal/tunnel/device_conn.go:118-126`:
```go
for {
    c.ws.SetReadDeadline(time.Now().Add(readTimeout))  // line 119
    _, msg, err := c.ws.ReadMessage()                  // line 120
    if err != nil {
        if !c.closed.Load() {
            c.logger.Error("read error", "error", err)  // line 123
        }
        return  // line 125 — exits read pump → defer Close() fires
    }
```

`SetReadDeadline` IS reset on every loop iteration (line 119). The device's JSON PING frames (sent every 15s) should arrive as data frames and reset the deadline. If the PINGs are arriving, the deadline should be continuously pushed forward and never expire.

### 2.2 Close path — close frame may be lost

`gateway/internal/tunnel/device_conn.go:531-543`:
```go
func (c *DeviceConn) Close(code int, reason string) {
    ...
    c.ws.WriteControl(websocket.CloseMessage, msg, time.Now().Add(5*time.Second))
    c.ws.Close()
    ...
}
```

`WriteControl` sends a WS close frame with 5s timeout. If it times out, the error is silently ignored. Then `ws.Close()` tears down the TCP connection. The device sees a TCP RST without a WS close frame → `no close frame received or sent`.

### 2.3 Write pump ping — possible trigger

`gateway/internal/tunnel/device_conn.go:221-229`:
```go
case <-wsPingTicker.C:
    c.mu.Lock()
    c.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
    err := c.ws.WriteMessage(websocket.PingMessage, nil)
    c.mu.Unlock()
    if err != nil {
        c.logger.Error("write error on ping", "error", err)
        return  // exits write pump → defer Close() fires
    }
```

If a WS-level ping write fails (e.g., TCP buffer issue), the write pump exits, triggering `Close()`.

---

## 3. Inspection Commands (run on the cloud server)

### 3.1 Verify the running binary is the latest code

```bash
# Check git log on the server
cd /root/projects/oneClickAgent && git log --oneline -5

# Check binary build time
ls -la /root/projects/oneClickAgent/gateway/bin/gateway

# Verify the critical fix is in the source
grep -n "SetReadDeadline\|PongHandler" /root/projects/oneClickAgent/gateway/internal/tunnel/device_conn.go
```

Expected output should include both:
- Line 94: `c.ws.SetPongHandler(...)`
- Line 119: `c.ws.SetReadDeadline(...)`

### 3.2 Check gateway logs for read errors

```bash
grep "read error" /tmp/gateway.log | tail -20
```

This will show WHAT error `ReadMessage()` is returning and WHEN. Look for:
- `i/o timeout` — read deadline expired (device PINGs not arriving)
- `connection reset by peer` — device disconnected
- `use of closed network connection` — double-close

### 3.3 Check gateway logs for write pump failures

```bash
grep "write error on ping\|write error\|write pump done\|device connection closed" /tmp/gateway.log | tail -20
```

### 3.4 Check if the device is registered in the hub

```bash
grep "received HELLO\|device registered\|device unregistered" /tmp/gateway.log | tail -20
```

### 3.5 Correlate disconnects with gateway events

```bash
# Get a timestamp around a disconnect from device log
# 22:24:13 UTC+8 = 14:24:13 UTC

grep "14:24" /tmp/gateway.log | head -20
```

---

## 4. Leading Hypothesis

The `readTimeout` (30s) is expiring because the device's application-level PING frames are **not resetting the read deadline**. This could happen if:

- **H1**: The gateway binary is stale — `git pull` wasn't run before `go build`, so the running code lacks `SetReadDeadline` reset in the loop (it was set once outside the loop in the old code).
- **H2**: The device's JSON PING frames are arriving at the gateway but `ReadMessage()` is not returning them (blocked on a previous frame's processing).
- **H3**: The write pump exits (ping write fails) → `Close()` fires → read pump exits → device sees TCP RST. This would show `"write error on ping"` in the gateway log at the exact disconnect time.

**H1 is the most likely.** If the gateway binary was rebuilt from old code (before bfd66e7), the `SetReadDeadline` might have been set once before the loop and never reset, causing exactly 30s disconnects regardless of incoming frames.
