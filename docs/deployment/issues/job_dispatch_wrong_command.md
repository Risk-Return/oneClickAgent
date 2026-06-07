# JOB_DISPATCH Command Field Contains Wrong Text

**Date:** 2026-06-07
**Status:** Open — cloud-side fix claimed but not verified

## Symptom

Jobs reach the agent but opencode receives a file path or job ID as the task text
instead of the user's actual command.

## Evidence

**Agent container log** (`docker logs agent-019ea2cb-e18d-74`):

```
2026-06-07 16:01:52,378 [iagent_agent.adapter.brain_opencode] INFO:
opencode[019ea2d1-b2c3-78bc-899a-88adc80a11cb]:
  Complete the following task. Save all output files to /work/output
  (markdown summaries, JSON, Excel, DOCX, HTML, PDF, etc.).

Task:
docs/deployment/issues/agent_output_files_cleanup.md
```

**Expected command text (user input from web UI):**
```
try to search a random topic in xiaohongshu, list the most three popular notes
```

**Actual command text received:** `docs/deployment/issues/agent_output_files_cleanup.md`

**Full opencode command line from process:**
```
/usr/bin/opencode run --dangerously-skip-permissions
  Use the skill instructions from ~/.claude/skills/default/SKILL.md to complete this task.
  Complete the following task. Save all output files to /work/output (markdown summaries, JSON, Excel, DOCX, HTML, PDF, etc.).
  Task: docs/deployment/issues/agent_output_files_cleanup.md
  Write all output files to /work/workspaces/019ea2d1-b2c3-78bc-899a-88adc80a11cb/output. Create a summary at /work/workspaces/019ea2d1-b2c3-78bc-899a-88adc80a11cb/output/summary.md when done.
```

The `ctx.command` value from the JOB_DISPATCH payload is:
```
Complete the following task. Save all output files to /work/output (markdown summaries, JSON, Excel, DOCX, HTML, PDF, etc.).  Task: docs/deployment/issues/agent_output_files_cleanup.md
```

This means the cloud gateway is embedding `"Complete the following task. Save all output files to /work/output..."` as the Command field AND appending `"Task: <wrong_text>"` after it. Neither the wrapper text nor the wrong task text should be in the JOB_DISPATCH payload's Command field.

## Previous Occurrences

| Job ID | Command Text Received | Expected |
|--------|----------------------|----------|
| `019ea1fd-a7b9...` | `019ea1fd-a7b9-7993-8e4b-47549d1e98a2` (prev job ID) | User text |
| `019ea281-2ad9...` | Correct text? | — |
| `019ea29d-96ae...` | `Task: try to search...` (has Task: prefix) | `try to search...` |
| `019ea2d1-b2c3...` | `docs/deployment/issues/agent_output_files_cleanup.md` (file path) | User text |

## Root Cause

The cloud gateway's `handleSubmitJob` builds the JOB_DISPATCH payload with `Command: job.Command`.
The issue is in how `job.Command` is populated — it appears to be set to the first file attachment
name or a wrapper text instead of the user's actual input from the web UI.

The gateway also appears to be wrapping the command text with:
```
Complete the following task. Save all output files to /work/output (...)
  Task: {actual_command?}
```

This wrapper text should be generated on the agent side (in `SKILL_PROMPT_TEMPLATE`),
not included in the JOB_DISPATCH payload. The payload's Command field should contain
only the raw user input.

## Affected Code

**Gateway** — `handleSubmitJob()` populates `JobDispatchPayload.Command` with wrong value.

## Fix

1. Verify what the web UI sends as the "command" field in the job creation request
2. Ensure `job.Command` is set to the raw user input text, not a file path or wrapper text
3. Remove any wrapper text ("Complete the following task...", "Task:") from the Command field — this belongs in the agent-side SKILL_PROMPT_TEMPLATE
