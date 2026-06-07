# Agent Drain Not Synchronized During Tunnel Disconnection

**Date:** 2026-06-07
**Status:** Open — cloud-side fix needed

## Symptoms

After draining agent pools from the cloud web UI, the following issues occur:

1. **Stale agent records on device**: Agents drained on the cloud side remain in the device's
   SQLite database as stale records. When the device reconciles, it tries to restart containers
   for these deleted agents, fails (container doesn't exist), and eventually exceeds max restarts
   (default 3, but can balloon to 500+ over time).

2. **Pool creation blocked**: With stale records clogging the device DB, new agents can't be
   created because the reconciliation loop is stuck retrying deleted agents.

3. **Manual cleanup required**: The only recovery is to manually delete stale records from
   the device's SQLite DB and restart the device.

## Root Cause

The cloud→device drain flow works when the tunnel is connected:

```
Cloud: DrainAgent() → sends AGENT_ACTION{drain} → Device: deletes container + DB record
```

But if the **tunnel is disconnected** when the drain occurs, the `AGENT_ACTION` frame is
**lost forever**. The cloud gateway has no outbound buffer — frames are sent directly to
the WebSocket connection. If the connection is down, the frame is dropped.

### Why tunnel disconnections happen frequently

- The device connects over a reverse WebSocket tunnel through the public internet
- Gateway restarts, network blips, and load balancer timeouts all cause disconnects
- During our testing, the device reconnected multiple times in a single session

### Device outbox vs cloud outbox

| Component | Has outbox? | Replays on reconnect? |
|-----------|-------------|----------------------|
| Device → Cloud | Yes (`Outbox` class) | Yes — queues frames during disconnect, flushes on reconnect |
| Cloud → Device | **No** | **No** — frames are sent synchronously, dropped if disconnected |

The device already has `AckTracker` and `Outbox` for frames it sends to the cloud.
The cloud gateway needs an equivalent mechanism for frames it sends to the device.

## Affected Frames

All cloud→device frames are affected, not just `AGENT_ACTION`:

| Frame Type | Impact if Lost |
|-----------|---------------|
| `AGENT_CREATE` | New pool agents never created on device |
| `AGENT_ACTION` (drain/restart) | Stale records accumulate on device |
| `SKILL_SYNC` | Skills not installed on agents |
| `JOB_DISPATCH` | Jobs stuck at "dispatched" (also affected by the separate bug in `3f099ab`) |
| `JOB_CANCEL` | Jobs continue running on device after cancellation |
| `CREDENTIAL_PUSH` | Login credentials never reach agent |
| `FILE_PUSH_*` | Input files never reach device |

## Fix: Cloud-Side Outbox for Device Frames

The cloud gateway needs an outbound frame buffer per device, similar to the device's
`Outbox` class at `device/iagent_device/tunnel/outbox.py`.

### High-level design

1. **Per-device outbound queue** in the Hub — a channel/buffer of frames keyed by device ID
2. **On send**: If the device is connected, send directly. If disconnected, enqueue to buffer
   with a TTL.
3. **On device connect (HELLO)**: The Hub replays all buffered frames for that device
4. **TTL and dedup**: Buffered frames older than N minutes are dropped. Frames are idempotent
   (similar to `AckTracker` on the device side).

### Existing code to model after

- `device/iagent_device/tunnel/outbox.py` — device-side outbox with retry and dedup
- `device/iagent_device/tunnel/ack_tracker.py` — per-message-id tracking

### Minimal fix (for now)

Until a full outbox is implemented, the device reconciliation should be more tolerant:

1. **Cap max restarts** at a lower value (e.g., 10 instead of infinite growth)
2. **Auto-delete agents** whose containers don't exist after N restart attempts
3. **On HELLO**, the cloud should re-send current agent list for reconciliation

## Current Workaround

When stale records block pool creation:

```bash
# On the device machine, clean stale agent records from SQLite
sqlite3 $IAGENT_DEVICE_DATA_DIR/device.db \
  "DELETE FROM agents WHERE restarts > 10"

# Restart the device
pkill -f iagent_device
# Start device normally
```

## Verification

1. Create 4 agents via cloud web UI
2. Drain 2 agents
3. Disconnect device (kill device process)
4. Reconnect device
5. Confirm the 2 drained agents are NOT in the device DB
6. Confirm reconciliation doesn't try to restart deleted agents
