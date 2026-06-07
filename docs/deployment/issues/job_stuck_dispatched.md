# Job Stuck at "Dispatched" Status

**Date:** 2026-06-07 15:09  
**Status:** Resolved — root cause identified, fix committed  
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

### Device-side (confirmed healthy)

Run on the device machine:

```bash
# Agent container is running and healthy
docker ps --filter 'label=iagent.pool=true'
# → agent-019ea0d0-a592-75, Up, healthy, port 42000

# Agent HTTP API responds correctly
curl http://localhost:42000/healthz
# → {"status":"ok","busy":false}

# Agent has no current job — never received one
curl http://localhost:42000/status
# → {"agent_id":"","current_job":null}

# Agent container logs — only healthchecks, no POST /jobs
docker logs agent-019ea0d0-a592-75
# → Only GET /healthz entries
```

**Device logs:** No `JOB_DISPATCH` frame handling logged. Only health checks against agent containers. No tunnel frame for this job was ever received.

## Root Cause

**The bug is in the gateway, not the device.**

`gateway/internal/httpapi/jobs_handler.go:handleSubmitJob()` calls `Allocate()` which updates the job status to `dispatched` in PostgreSQL and allocates the agent, but **never sends the `JOB_DISPATCH` tunnel frame** to the device.

The frame-sending logic (`dispatchJob()` → `hub.SendFrame()`) is only called from `dequeueNext()` for queued jobs. When an agent is immediately available (no queue), the dispatch is skipped entirely.

**Affected code** (`gateway/internal/httpapi/jobs_handler.go`, lines 92-95):

```go
// Agent allocated — updated DB but NEVER sent the frame:
_ = deps.Jobs.SetAgent(r.Context(), job.ID, agent.ID, agent.DeviceID)
_ = deps.PushFilesToDevice(r.Context(), job, agent.DeviceID)
// Missing: hub.SendFrame(FrameJobDispatch, ...)
```

The device never knew the job existed — its logs show zero evidence of any `JOB_DISPATCH` frame.

## Fix

Commit: `3f099ab` — `fix(gateway): send JOB_DISPATCH frame on immediate agent allocation`

Added dispatch logic after agent allocation, mirroring the pattern from `dequeueNext()`:

```go
dispatchPayload := model.JobDispatchPayload{
    JobID:       job.ID,
    UserID:      job.UserID,
    AgentID:     agent.ID,
    Command:     job.Command,
    SkillID:     job.SkillID,
    SubmittedAt: job.SubmittedAt.UnixMilli(),
}
if job.Params != nil {
    dispatchPayload.Params = *job.Params
}
if dispatchFrame, err := tunnel.NewFrame(model.FrameJobDispatch, dispatchPayload); err == nil {
    if sendErr := deps.Hub.SendFrame(agent.DeviceID, dispatchFrame); sendErr != nil {
        obs.Logger("http.jobs").Error("failed to send JOB_DISPATCH frame",
            "job_id", job.ID.String(),
            "agent_id", agent.ID.String(),
            "error", sendErr,
        )
    }
}
```

## Deployment

| Step | Status |
|------|--------|
| Code fix (dispatch) | Committed & pushed to `main` |
| Code fix (workspace) | Committed & pushed to `main` |
| `go build` | Pass |
| `go vet` | Pass |
| Deploy to `deepwitai.cn` | **Pending** — cloud-side deployment needed |
| Stuck job cleanup | Cancel `019ea0ea-4d46` manually after deploy |

## Cloud-Side Action Items

### 1. Deploy gateway fix

Build and deploy commit `3f099ab` (or later) to `deepwitai.cn`:

```bash
cd gateway && git pull && go build -o bin/gateway ./cmd/gateway && go vet ./...
# Restart gateway service
```

This adds the missing `JOB_DISPATCH` frame send in `handleSubmitJob`.

### 2. Restart device

After gateway deploy, restart the device process on the local machine so it picks up the workspace path fix (`7478c93`):

```bash
# On the local device machine:
pkill -f iagent_device
IAGENT_DEVICE_DATA_DIR=/tmp/iagent-cloud \
  IAGENT_GATEWAY_URL=https://deepwitai.cn/aiproduct \
  nohup venv/bin/python -m iagent_device run &>/tmp/device-cloud4.log &
```

### 3. Cancel stuck job

Via the cloud gateway API or DB:

```sql
UPDATE jobs SET status='cancelled' WHERE id='019ea0ea-4d46-7b0f-9266-f51c584d019b';
UPDATE agents SET status='idle', job_id=NULL WHERE job_id='019ea0ea-4d46-7b0f-9266-f51c584d019b';
```

## Verification

1. Cancel the stuck job (`019ea0ea-4d46-7b0f-9266-f51c584d019b`)
2. Submit a new test job from the web UI
3. Confirm `JOB_DISPATCH` appears in gateway tunnel logs
4. Confirm device receives the frame, sends `JOB_ACCEPTED`, `JOB_PROGRESS`
5. Job progresses from `dispatched` → `running` → `succeeded`

## Second Issue: Workspace Permission Denied

After the dispatch fix allowed a job to reach the agent, it failed immediately with:

```
PermissionError: [Errno 13] Permission denied: '/workspaces/019ea122-b0d7...'
```

**Cause:** `device/iagent_device/jobs/dispatcher.py:85` hard-codes `workspace_dir=/workspaces/{job_id}`. The container user `app` only has write access to `/work`.

**Fix:** Commit `7478c93` — changed to `/work/workspaces/{job_id}`.

## Commit Summary

| Commit | File | Fix |
|--------|------|-----|
| `3f099ab` | `gateway/internal/httpapi/jobs_handler.go` | Send `JOB_DISPATCH` frame on immediate allocation |
| `7478c93` | `device/iagent_device/jobs/dispatcher.py` | Use `/work/workspaces/` instead of `/workspaces/` |
