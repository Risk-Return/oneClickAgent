# Job Stuck at "Dispatched" Status

**Date:** 2026-06-07 15:09  
**Status:** Open — device-side investigation needed  
**Gateway:** `https://deepwitai.cn/aiproduct`  
**Job ID:** `019ea0ea-4d46-7b0f-9266-f51c584d019b`

## Symptoms

A job submitted via the web UI shows status `dispatched` and never progresses to `running` or `succeeded`. The job stays stuck indefinitely.

## Investigation

### Cloud-side (gateway)

**Job record in PostgreSQL:**
```
id:       019ea0ea-4d46-7b0f-9266-f51c584d019b
status:   dispatched
agent_id: 019ea0d0-a592-75ae-9e07-fd767e31da7e
command:  try to search a random topic in xiaohongshu...
```

**Agent record:**
```
id:       019ea0d0-a592-75ae-9e07-fd767e31da7e
device:   019e9c59-1d82-7a22-8b41-814680cafabd
name:     agent-019ea0d0
status:   idle
port:     42000
```

**Gateway logs:**
- `POST /api/v1/jobs` → 201 (job created, dispatched to idle agent)
- No `JOB_ACCEPTED`, `JOB_PROGRESS`, or `JOB_RESULT` received from device

**Tunnel status:** Device is connected and stable.

### Conclusion

The cloud side is working correctly — the allocator found an idle agent, updated the job to `dispatched`, and sent a `JOB_DISPATCH` frame to the device. The device received it but never responded with `JOB_ACCEPTED`.

## Device-Side Diagnosis

Run these commands on the local device machine:

```bash
# 1. Check if the container is running
docker ps --filter 'label=iagent.pool=true'

# 2. Test agent HTTP API directly
curl -s http://localhost:42000/healthz
# Expected: {"status":"ok","busy":false}

# 3. Check device logs for dispatch errors
journalctl -u iagent-device --since "10 minutes ago" | grep -i 'job\|dispatch\|error'

# 4. Check container logs for crashes
docker logs agent-019ea0d0
```

## Likely Causes

1. **Agent container crashed** — Docker container exists but the internal FastAPI process died
2. **Agent HTTP API not responding** — network issue between device and agent container
3. **OpenCode CLI not installed** — agent container missing `opencode` binary, causing job submission to fail
4. **Disk/CPU/memory limit** — container resource limits preventing startup

## Resolution

Once the device-side issue is fixed, the stuck job will need to be cancelled and a new one submitted (no auto-retry for dispatched jobs).
