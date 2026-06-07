# Agent Output Files Not Reaching Cloud

**Date:** 2026-06-07
**Status:** Partially fixed — agent wipe fixed, gateway FILE_PULL handler missing

## Symptoms

Job completes successfully but the cloud gateway receives no output files
(e.g., `summary.md`, screenshots, data files). The web UI shows job succeeded
but no results are available.

## Root Cause

Two issues in the file pull chain:

### Issue 1: Agent wipes output before device pulls (FIXED)

**Agent `executor.py:108`** — `_teardown()` called `self._workspace.wipe()` in
the `finally` block immediately after job completion. This deleted all output
files under `/work/workspaces/{job_id}/output/` before the device's next poll
cycle (up to 2 seconds later) could discover the completion and call `pull_outputs()`.

**Timeline:**
```
Agent:  status=SUCCEEDED → wipe() deletes files   ← FILES GONE
Device: poll → sees SUCCEEDED → pull_outputs()     ← TOO LATE, FILES DELETED
```

**Fix:** Commit `1d46d31` — removed `self._workspace.wipe()` from `_teardown()`.
The device's `dispatcher.py` `finally` block already handles cleanup via
`stager.cleanup()` and `reaper_cleanup()` after the pull completes.

### Issue 2: Gateway drops FILE_PULL_* frames (NEEDS CLOUD FIX)

**Gateway `internal/tunnel/device_conn.go`** — The `handleFrame` switch statement
(lines 255-419) handles all frame types, but `FILE_PULL_BEGIN`, `FILE_PULL_CHUNK`,
and `FILE_PULL_END` are **missing**. When the device puller sends file data chunks
over the tunnel, the gateway silently drops them:

```go
// handleFrame switch — these frame types are MISSING:
case model.FrameFilePullBegin:  // NOT PRESENT
case model.FrameFilePullChunk:  // NOT PRESENT
case model.FrameFilePullEnd:    // NOT PRESENT
```

The handlers exist (`hub.go` `HandleFilePullBegin/Chunk/End`, `Hub.onFilePullBegin`
callbacks wired in `main.go`), but they are **never called** because `handleFrame`
falls through to `default` which only logs and returns nil.

**Net effect:** The device chunks output files, base64-encodes them, sends them
over the tunnel — but the gateway never receives or stores them.

## Fix Checklist

| # | Issue | Commit | Status |
|---|-------|--------|--------|
| 1 | Agent wipes before device pulls | `1d46d31` | Fixed |
| 2 | Gateway drops FILE_PULL_* frames | — | **Cloud-side fix needed** |

## Cloud-Side Action Items

### 1. Add FILE_PULL_* cases to `device_conn.go` handleFrame

In `gateway/internal/tunnel/device_conn.go`, add these cases to the `handleFrame`
switch statement:

```go
case model.FrameFilePullBegin:
    if h := hw.hub.OnFilePullBegin(); h != nil {
        payload, _ := frame.Payload()
        h(hw.connID, payload)
    }

case model.FrameFilePullChunk:
    if h := hw.hub.OnFilePullChunk(); h != nil {
        payload, _ := frame.Payload()
        h(hw.connID, payload)
    }

case model.FrameFilePullEnd:
    if h := hw.hub.OnFilePullEnd(); h != nil {
        payload, _ := frame.Payload()
        h(hw.connID, payload)
    }
```

Build and deploy:
```bash
cd gateway && git pull && go build -o bin/gateway ./cmd/gateway && go vet ./...
# Restart gateway service
```

## Verification

1. Submit a test job from web UI with a task that writes output files
2. Confirm job succeeds
3. Device poll log shows output files pulled
4. Gateway log shows FILE_PULL_* frames processed
5. Output files appear in cloud file store
