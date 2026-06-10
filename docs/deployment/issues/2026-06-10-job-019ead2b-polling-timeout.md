# Job `019ead2b` Failure Analysis — Cloud Side

**Date:** 2026-06-10 00:15 CST
**Job ID:** `019ead2b-4e3e-7e8b-97c8-ace37338e1a2`
**Agent ID:** `019ead2a-f7a6-7da4-a4bb-77b2d7c35686`
**Device ID:** `019ea7ff-4976-76e6-8086-638dbebbde4b`

## DB Record

```
status     | failed
error_code | (empty)
agent_id   | 019ead2a
device_id  | 019ea7ff
created_at | 2026-06-10 00:15:56.990 CST
started_at | 2026-06-10 00:15:56.993 CST
finished_at| 2026-06-10 01:16:04.038 CST
```

## Timeline

| Time (CST) | Event |
|-------------|-------|
| 00:15:56 | Job submitted, agent `019ead2a` allocated, `JOB_DISPATCH` sent |
| 00:15:58 | `JOB_DISPATCH` retransmitted (retry 1) — device never ACK'd |
| 00:16:00 | `JOB_DISPATCH` retransmitted (retry 2) — device never ACK'd |
| 00:16–01:16 | UI polling `/jobs/.../output` every 5s — zero output files |
| 01:16:04 | Agent released → `JOB_RESULT{status:"failed"}` received, no `error_code` |

## Gateway Frame Activity

| Frame Direction | Frame Type | Received? |
|-----------------|-----------|-----------|
| G→D | `JOB_DISPATCH` | Sent (retransmitted ×2) |
| D→G | `JOB_ACCEPTED` | **Never received** |
| D→G | `JOB_PROGRESS` | **Never received** |
| D→G | `FILE_PULL_BEGIN/CHUNK/END` | **Never received** |
| D→G | `JOB_RESULT` | Received at 01:16:04 (`status:"failed"`) |

## Root Cause

**Device-side polling timeout.** The local device reported that the agent completed the task successfully internally. However, the device's polling mechanism (which queries the agent container's HTTP API for status/results) timed out after 3600 seconds (1 hour). Since no progress or result frames were received, the gateway saw:

1. `JOB_DISPATCH` frame never ACK'd by device (retransmitted twice)
2. No `JOB_ACCEPTED` — gateway never transitioned to `running`
3. No `JOB_PROGRESS` — UI showed no progress
4. No `FILE_PULL_*` — no output file transfers
5. At exactly 1h, `JOB_RESULT{failed}` with no error_code

The 1-hour delta between `started_at` and `finished_at` matches the device's default poll timeout.

## Cloud-Side Assessment

The gateway correctly:
- Dispatched the job (HTTP 201)
- Retransmitted the unacknowledged dispatch frame
- Processed the `JOB_RESULT{failed}` when received
- Released the agent back to pool

No cloud-side code changes are needed for this specific failure path.

## Recommended Device-Side Investigation

1. Why did the agent container's HTTP API become unreachable to the device's poll loop?
2. Check agent Docker networking and firewall rules
3. Verify the agent's `/status` and `/result` endpoints respond during long-running tasks
4. Consider reducing the poll timeout or adding a reconnection mechanism
