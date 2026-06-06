# Cloud Gateway 502 Bad Gateway After Redeploy

**Date:** 2026-06-06 14:27 CST  
**Status:** Open — cloud gateway backend process appears down  
**Gateway:** `https://deepwitai.cn/aiproduct`

## Symptoms

After pulling latest gateway code (commit `7ee8374`), the device cannot connect:

```
GET  /aiproduct/healthz         → 502 Bad Gateway
GET  /aiproduct/tunnel          → 502 Bad Gateway (nginx)
```

The nginx reverse proxy is running but the Go gateway backend is unreachable.

## Device Log

```
websockets.exceptions.InvalidStatus: server rejected WebSocket connection: HTTP 502
```

## Context

- Gateway was redeployed with commit `7ee8374` (drain + allocator fixes)
- Before redeploy: tunnel was connected and stable for 20s+
- After redeploy: 502 on all endpoints

## Additional Issue: Stale Agent Containers

Before the gateway went down, 5 agent containers existed in the local Docker (from previous pool_size=5). Running the device with `IAGENT_POOL_SIZE=0` and stale SQLite agent records caused the reconcile loop to health-check old containers on ports 42000-42004.

Since the cloud gateway is down, the device cannot:
- Establish a tunnel connection
- Receive AGENT_CREATE frames to create fresh containers
- Send AGENT_STATUS updates

## Steps Taken

1. `docker stop` all 5 containers
2. `docker rm -f` removed all agent containers
3. Deleted old SQLite enrollment DB (`/tmp/iagent-cloud-test3`)
4. Waiting for cloud gateway to come back online

## Next Steps

1. Cloud admin checks the gateway process on deepwitai.cn:
   ```bash
   systemctl status iagent-gateway
   journalctl -u iagent-gateway --since "5 minutes ago"
   ```
2. Ensure the Go binary was rebuilt and the process restarted
3. After gateway is healthy, re-enroll the device with a fresh code
