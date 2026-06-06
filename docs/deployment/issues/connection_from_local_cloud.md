# Connection from Local Device to Cloud Gateway

**Date:** 2026-06-06  
**Status:** Fixed — deployed to cloud gateway on 2026-06-06  
**Gateway:** `https://deepwitai.cn/aiproduct`  
**Enrollment code used:** `019e9b14-c82b-738a-a6bb-42bfc2b425b6`  
**Enrolled device ID:** `019e9b14-c82b-7390-8555-481e0cf6fb3a`

## Test Steps Performed

### 1. Gateway health check — PASSED
```
GET https://deepwitai.cn/aiproduct/healthz → 200 {"status":"ok"}
```

### 2. Device enrollment — PASSED
```
POST https://deepwitai.cn/aiproduct/api/v1/devices/enroll
  {"enrollment_code": "019e9b14-c82b-738a-a6bb-42bfc2b425b6"}
  → 200 {"device_id": "019e9b14-c82b-7390-8555-481e0cf6fb3a", "device_token": "..."}
```

### 3. Tunnel connection — FAILED

```
Device → wss://deepwitai.cn/aiproduct/tunnel
  Sec-WebSocket-Protocol: iagent.tunnel.v1
  Authorization: Bearer <device_token>

Gateway response header:
  Sec-WebSocket-Protocol: iagent.tunnel.v1, iagent.tunnel.v1  ← DUPLICATE
```

## Root Cause

**Duplicate `Sec-WebSocket-Protocol` header** in gateway response. The gorilla/websocket upgrader automatically sets the subprotocol header when `Subprotocols` is configured. The gateway code also manually writes the header in the response headers map, causing it to appear twice:

```go
// gateway/internal/httpapi/tunnel_handler.go

upgrader := websocket.Upgrader{
    Subprotocols: []string{model.SubprotocolTunnel},  // ← upgrader sets it automatically
}

conn, err := upgrader.Upgrade(w, r, http.Header{
    "Sec-WebSocket-Protocol": {model.SubprotocolTunnel},  // ← THIS IS THE DUPLICATE
})
```

The `websockets` v16 client library is strict about subprotocol negotiation and rejects responses with multiple values.

## Fix

Remove the manual `Sec-WebSocket-Protocol` header from `tunnel_handler.go` line 64 (the upgrader handles it):

```diff
-conn, err := tunnelUpgrader.Upgrade(w, r, http.Header{
-    "Sec-WebSocket-Protocol": {model.SubprotocolTunnel},
-})
+conn, err := tunnelUpgrader.Upgrade(w, r, nil)
```

This fix is already committed to `main` (commit `e96872c`) but must be redeployed to the cloud gateway.

## Impact

The device cannot establish the tunnel connection. All three reconnect attempts fail with the same error. Enrollment works correctly, so the fleet dashboard should show the device as "enrolled" but it will never reach "online".

## Next Steps

1. Apply the commit `e96872c` (or cherry-pick the tunnel_handler.go fix) to the cloud gateway deployment
2. Rebuild the gateway: `cd gateway && go build -o bin/gateway ./cmd/gateway`
3. Restart the gateway service on deepwitai.cn
4. The device will auto-reconnect after the gateway restarts (exponential backoff up to 30s max)
5. No changes needed on the device side

## Verification (2026-06-06 12:59 CST)

Cloud gateway deployed fix. Re-tested with new enrollment code:

```
Enrollment: 019e9b49-fa7d-7617-9472-429bfaf317e5
Device ID:   019e9b49-fa7d-761e-a0d6-f171648d57da
Tunnel:      wss://deepwitai.cn/aiproduct/tunnel → CONNECTED
HELLO_ACK:   received (session_id=019e9b4c-852d-7f3a-a35b-5189fe833a43, heartbeat_s=15)
Stability:   no disconnects after 10s+
```

**Status: RESOLVED.** Device is online on the cloud gateway.
