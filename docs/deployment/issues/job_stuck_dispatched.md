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
| Code fix | Committed & pushed to `main` |
| `go build` | Pass |
| `go vet` | Pass |
| Deploy to `deepwitai.cn` | **Pending** — cloud-side deployment needed |
| Stuck job cleanup | Cancel `019ea0ea-4d46` manually after deploy |

## Verification

After deploying the fix, the fix can be verified by:

1. Cancel the stuck job (`019ea0ea-4d46-7b0f-9266-f51c584d019b`)
2. Submit a new test job from the web UI
3. Confirm `JOB_DISPATCH` appears in gateway logs
4. Confirm device receives the frame and responds with `JOB_ACCEPTED`
5. Job progresses from `dispatched` → `running` → `succeeded`
