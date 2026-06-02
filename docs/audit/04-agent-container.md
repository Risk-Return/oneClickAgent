# Audit 04 — Agent Container Implementation vs Spec

**Date:** 2026-06-02
**Scope:** Line-by-line audit of all 12 Python source files + Dockerfile + deps manifests in `agent/` against `docs/spec/04-agent-container.md`, cross-referenced with `docs/spec/01-architecture.md`, `docs/spec/00-overview.md`, and `docs/braionstorm/goal.md`.

---

## Summary

| Category | Count |
|----------|-------|
| Critical gaps | 3 |
| Significant gaps | 7 |
| Minor gaps | 7 |

Overall: the HTTP API surface, workspace hygiene, skill CRUD, and browser state injection are solidly implemented with good test coverage (15 contract tests). However, the brain adapter layer is **entirely stub-only** — the spec-default `opencode` brain has zero code, making the agent unable to execute real work. The Dockerfile is structurally correct but lacks multi-arch build support and tmpfs. Several protocol details (callback payload fields, POST /jobs body shape, credential-injection wiring) diverge from the spec.

---

## 1. Critical Gaps

### C1 — No opencode brain adapter; `_make_brain()` always returns StubBrain

- **File:** `agent/iagent_agent/server.py:36-39`
- **Severity:** Critical
- **Detail:** The `_make_brain()` function checks `IAGENT_BRAIN` but always returns `StubBrain` regardless of the value. The `brain_type == "opencode"` branch does not exist. This means the spec-default behavior (`IAGENT_BRAIN=opencode` per §9) is completely unimplemented — the agent can only run the 4-step stub progress simulation and never executes a real LLM-driven job.
- **Spec ref:** `04-agent-container §2`: "Default agent: opencode", `§9`: `IAGENT_BRAIN` default = `opencode` ("selects the AgentBrain adapter"). Also `goal.md` line 95: "default agent should be pre-installed: opencode".

### C2 — `IAGENT_AGENT_ID` env var never consumed

- **File:** `agent/iagent_agent/server.py` (absent), `agent/iagent_agent/__main__.py` (absent)
- **Severity:** Critical
- **Detail:** The spec §9 configuration table lists `IAGENT_AGENT_ID` as an injected identity variable ("identity (label-matched)"). No code in the agent reads this variable. The agent has no self-awareness of its own identity. This means the device cannot label-match agents, and the agent cannot include its ID in responses to the device (e.g. in `/status` or callback events).
- **Spec ref:** `04-agent-container §9` table, `01-architecture §2`: "Every routable entity has a stable ID: agent_id".

### C3 — `IAGENT_BRAIN` default is `stub`, not `opencode`

- **File:** `agent/iagent_agent/server.py:35`
- **Severity:** Critical
- **Detail:** `os.getenv("IAGENT_BRAIN", "stub")` — the code defaults to the stub brain. The spec §9 says the default is `opencode`. Combined with C1 (no opencode adapter exists), this means running the agent without explicitly setting `IAGENT_BRAIN` to anything would use the stub, and setting it to the spec default (`opencode`) would silently fall through to the stub since `brain_type == "stub"` is the only recognized value. The Dockerfile at `agent/Dockerfile:138` confirms this: `ENV IAGENT_BRAIN=stub`.
- **Spec ref:** `04-agent-container §9`: `IAGENT_BRAIN` default = `opencode`.

---

## 2. Significant Gaps

### S1 — `POST /jobs` body does not accept `inputs_dir` / `output_dir`

- **File:** `agent/iagent_agent/server.py:127-148` (submit_job handler)
- **Severity:** Significant
- **Detail:** The spec §3 table says POST `/jobs` body includes `{job_id, command, params, inputs_dir, output_dir, skill_id?, callback_url}`. The handler reads `job_id`, `command`, `params`, `skill_id`, and `callback_url` but ignores `inputs_dir` and `output_dir`. Instead, the executor hardcodes them from `self._workspace.inputs` / `self._workspace.output` (`executor.py:99-100`). If the device staged files to a non-standard path, the agent would use the wrong directory.
- **Spec ref:** `04-agent-container §3` table: POST `/jobs` body includes `inputs_dir` and `output_dir`.

### S2 — No disk quota enforcement

- **File:** `agent/iagent_agent/workspace.py` (no disk check), `agent/iagent_agent/runtime/executor.py` (no quota logic)
- **Severity:** Significant
- **Detail:** Spec §8 says "Agent should fail fast with a clear `error` if a job exceeds workspace disk quota." No code measures or enforces disk usage before or during job execution. The device applies container-level limits but the agent has no self-check.
- **Spec ref:** `04-agent-container §8`: "Agent should fail fast with a clear `error` if a job exceeds workspace disk quota."

### S3 — No multi-arch Docker build support

- **File:** `agent/Dockerfile` (entire file)
- **Severity:** Significant
- **Detail:** Spec §10 requires "Multi-arch build (`linux/amd64`, `linux/arm64`) so it runs under Docker Desktop on both Intel and Apple Silicon." The Dockerfile is a single-stage (`FROM ubuntu:24.04`) with no platform flags, no buildx `--platform` directives, and no multi-stage manifest strategy. The dev record claims this is done, but the Dockerfile contains no multi-arch instructions.
- **Spec ref:** `04-agent-container §10`: "Multi-arch build (linux/amd64, linux/arm64)".

### S4 — No tmpfs for `/tmp`

- **File:** `agent/Dockerfile:127` (final stage)
- **Severity:** Significant
- **Detail:** Spec §10 says "Writable areas limited to `/work` (volume) + tmpfs `/tmp`". The Dockerfile creates `/tmp/.X11-unix` at line 127 but never mounts `/tmp` as tmpfs. A tmpfs mount is a container runtime concern (applied by the device), but the spec implies the image should document or default to it. The Dockerfile only declares the `/work` volume; `/tmp` has no tmpfs configuration.
- **Spec ref:** `04-agent-container §10`: "Writable areas limited to /work (volume) + tmpfs /tmp".

### S5 — `credentials_injected` always `False` in JobContext

- **File:** `agent/iagent_agent/runtime/executor.py:102`
- **Severity:** Significant
- **Detail:** `JobContext.credentials_injected` is hardcoded to `False` when building the `JobContext` for `brain.run()`. The spec §7 says login cookies are injected via `POST /browser/state` before `brain.run` starts, and `JobContext.credentials_injected = true` should signal this to the brain. But the executor submits the job and immediately starts `brain.run()` — there is no waiting for a potential credential injection call from the device. The dev record acknowledges this as a known gap.
- **Spec ref:** `04-agent-container §7`: "the agent writes it into the ephemeral /work/profile before brain.run. JobContext.credentials_injected = true."

### S6 — Progress callback payload missing `ts` and `finished_at`

- **File:** `agent/iagent_agent/runtime/context.py:53-60`
- **Severity:** Significant
- **Detail:** The callback payload in `post_event()` includes `event_seq`, `status`, `percent`, `message`, `result?`, `error?` but is missing the `ts` (timestamp) field required by the spec, and does not include `finished_at` on terminal events. The spec §3 callback format requires: `{event_seq, status, percent, message, ts}` and on terminal: `{event_seq, status:"SUCCEEDED"|"FAILED", result?, error?, finished_at}`.
- **Spec ref:** `04-agent-container §3`: "POST {callback_url}/jobs/{job_id}/events { event_seq, status, percent, message, ts }" and "terminal: { event_seq, status:"SUCCEEDED"|"FAILED", result?, error?, finished_at }".

### S7 — Container security hardening left to device; Dockerfile has none

- **File:** `agent/Dockerfile` (entire file)
- **Severity:** Significant
- **Detail:** Spec §11 says "Drops capabilities, non-root, no host network, pids limit." The Dockerfile only implements non-root (`USER app`). Capabilities dropping (`cap_drop`), `pids_limit`, and `no_host_network` are not configured in the Dockerfile. These are applied at container runtime by the device (`03-local-device §7`), but the spec explicitly lists them as agent container security requirements. The non-root user is correctly set.
- **Spec ref:** `04-agent-container §11`: "Drops capabilities, non-root, no host network, pids limit."

---

## 3. Minor Gaps

### M1 — VNC entrypoint scripts not used by Python runtime

- **File:** `agent/vnc/entrypoint.sh` vs `agent/iagent_agent/browser/manager.py:88-133`
- **Severity:** Minor
- **Detail:** The spec §2 image layout includes `vnc/entrypoint.sh` scripts for Xvfb + x11vnc lifecycle. The `BrowserManager` and `VNCStack` classes launch Xvfb and x11vnc directly via `subprocess.Popen()` with hardcoded args, bypassing the entrypoint scripts entirely. The scripts exist but are dead code. The behavior is identical; this is an architectural drift.
- **Spec ref:** `04-agent-container §2` layout diagram: `vnc/ — entrypoint scripts: Xvfb + x11vnc + browser supervisor`.

### M2 — `IAGENT_AGENT_HOST` env var not in spec config table

- **File:** `agent/iagent_agent/__main__.py:8`
- **Severity:** Minor
- **Detail:** The entry point reads `IAGENT_AGENT_HOST` (default `0.0.0.0`) but this variable is not listed in the spec §9 configuration table. The spec only lists `IAGENT_AGENT_PORT` for the container's HTTP binding. Adding capacity to bind to a specific IP is useful but undocumented in the contract.
- **Spec ref:** `04-agent-container §9` configuration table.

### M3 — No callback behavior in contract tests

- **File:** `agent/tests/test_server.py` (entire file)
- **Severity:** Minor
- **Detail:** Spec §12 requires "Verify callback + polling fallback behavior." The 15 contract tests cover job polling (`GET /jobs/{id}`) but never test the callback path (`CallbackClient.post_event()` to a callback URL). There's no mock callback server or assertion that callback POSTs are sent.
- **Spec ref:** `04-agent-container §12`: "Verify callback + polling fallback behavior."

### M4 — No VNC RFB handshake integration test

- **File:** `agent/tests/test_server.py` (entire file)
- **Severity:** Minor
- **Detail:** Spec §12 requires "VNC: POST /vnc/start brings up Xvfb+x11vnc on loopback; an RFB handshake succeeds through a local bridge; POST /vnc/stop and job terminal tear it down." The tests only cover the 409 error (no active job) case and basic info endpoint. No test verifies Xvfb/x11vnc process startup, RFB handshake, or teardown.
- **Spec ref:** `04-agent-container §12`: "VNC: POST /vnc/start brings up Xvfb+x11vnc on loopback; an RFB handshake succeeds..."

### M5 — No image build smoke test

- **File:** No build test file in `agent/`
- **Severity:** Minor
- **Detail:** Spec §12 requires "Image: build smoke test asserts opencode, camoufox, node/python/go/rust/java are present and dependency caches are warm." No build smoke test script exists. The `pyproject.toml` has `pytest` for dev but no Docker build validation.
- **Spec ref:** `04-agent-container §12`: "Image: build smoke test asserts opencode, camoufox, node/python/go/rust/java are present..."

### M6 — `GET /browser/state?origin=` does not filter by origin

- **File:** `agent/iagent_agent/server.py:197-202`
- **Severity:** Minor
- **Detail:** The endpoint accepts an `origin: str = Query("")` parameter per the spec, but the handler reads the full storage-state file without filtering by origin. The `BrowserManager.export_state()` returns the entire `storage_state.json` regardless. Spec §7 says "Capture (save login): `GET /browser/state?origin=<site>`; the agent exports the current cookies + localStorage." The origin parameter is accepted but never used to filter the exported state.
- **Spec ref:** `04-agent-container §7`: "GET /browser/state?origin=<site>; the agent exports the current cookies + localStorage."

### M7 — `IAGENT_STUB_DELAY` env var not in spec config table

- **File:** `agent/iagent_agent/server.py:38`
- **Severity:** Minor
- **Detail:** The stub brain reads `IAGENT_STUB_DELAY` for configurable test delays, but this env var is not listed in the spec §9 configuration table. It's only relevant for stub testing, so this is defensible, but it's an undocumented configuration surface.
- **Spec ref:** `04-agent-container §9` configuration table.

---

## 4. What's Solidly Implemented

| # | Feature | Spec § | Evidence |
|---|---------|--------|----------|
| 1 | All 15 HTTP API routes match the spec table | §3 | `server.py:92-202` |
| 2 | Full workspace layout: `/work/{inputs,scratch,output,profile}` | §4 | `workspace.py:8-62` |
| 3 | Workspace wipe on all terminal states (success/fail/cancel) | §4 | `executor.py:113,119,134` |
| 4 | Single-job concurrency with `409 BUSY` | §3, §7 | `server.py:129-131` |
| 5 | Skill CRUD: install (idempotent), update, disable, enable, delete | §5 | `skills/loader.py:47-93` |
| 6 | Skill-enforced job submission (`SKILL_NOT_ENABLED` 422) | §5 | `server.py:136-138` |
| 7 | AgentBrain Protocol with `run()` and `cancel()` | §2 | `adapter/protocol.py:23-25` |
| 8 | JobContext matches spec fields (job_id, command, params, inputs_dir, output_dir, skill_id, credentials_injected, browser) | §2 | `adapter/protocol.py:9-17` |
| 9 | Browser storage-state inject + export via file-based round-trip | §7 | `browser/manager.py:39-60` |
| 10 | VNC stack on-demand (start Xvfb + x11vnc) with per-session random RFB password, loopback-only binding | §6 | `browser/manager.py:87-140` |
| 11 | VNC stack teardown on stop + job terminal | §6 | `browser/manager.py:142-156` |
| 12 | Stub brain with cancel support via in-memory set | — | `adapter/brain_stub.py:14-33` |
| 13 | Progress callback to device `{callback_url}/jobs/{job_id}/events` | §3 | `runtime/context.py:42-57` |
| 14 | Job state machine: RUNNING → SUCCEEDED / FAILED / CANCELLED | §4 | `runtime/context.py:15-17` |
| 15 | `JobRecord.event_seq` monotonic per-job for idempotent ordering | §3 | `runtime/context.py:34` |
| 16 | Multi-stage Dockerfile with all toolchains (Node 20, Python 3.12, Go 1.22, Rust, Java 21, Xvfb, x11vnc) | §10 | `Dockerfile:1-141` |
| 17 | Dependency cache warm-up from all 5 manifests (`npm ci`, `pip install`, `go mod download`, `cargo fetch`, `mvn dependency:go-offline`) | §2 | `Dockerfile:107-117` |
| 18 | Non-root `app` user + HEALTHCHECK via `/healthz` | §10 | `Dockerfile:127,139-140` |
| 19 | 15 contract tests covering healthz, status, job submit/poll/cancel, skills CRUD, skill validation, VNC info, browser state injection, workspace wipe | §12 | `tests/test_server.py` (all 15 tests) |
| 20 | `IAGENT_VNC_ENABLED`, `IAGENT_VNC_PORT`, `IAGENT_VNC_DISPLAY`, `IAGENT_BROWSER_CMD`, `IAGENT_BROWSER_PROFILE` all match spec defaults | §9 | `server.py:58-63` |

---

## 5. Fix Priority

| Priority | Gap | Impact |
|----------|-----|--------|
| P0 | **C1** — No opencode brain adapter; agent cannot execute real work | Agent is non-functional for production; all jobs run stub simulation |
| P0 | **C3** — `IAGENT_BRAIN` defaults to `stub` not `opencode` | Default behavior violates spec and means deployed agents silently use stub |
| P1 | **C2** — `IAGENT_AGENT_ID` not consumed | Agent has no self-identity; device cannot label-match |
| P1 | **S5** — `credentials_injected` always False | Login cookie injection doesn't signal the brain; breaks credential flow |
| P1 | **S6** — Callback missing `ts` and `finished_at` | Device/gateway can't compute latency or detect stalled jobs |
| P2 | **S1** — `inputs_dir` / `output_dir` not accepted in POST /jobs | Device mounting non-standard paths will break |
| P2 | **S7** — Container security hardening missing from Dockerfile | Relies entirely on device for security boundaries |
| P2 | **S2** — No disk quota enforcement | Agent may fill disk without warning |
| P3 | **S3** — No multi-arch build support | Cannot run on ARM64 (Apple Silicon) without emulation |
| P3 | **S4** — No tmpfs for `/tmp` | `/tmp` uses container overlay instead of memory |
| P4 | **M1-M7** — Minor gaps (unused scripts, missing tests, origin filtering, env var docs) | Cosmetic or test coverage gaps; no functional breakage |

---

## 6. Dev Progress Doc Accuracy

The dev record at `docs/dev/04-agent-container.md` marks all 14 packages as **"Done"** and lists 8 known gaps. This audit finds:

- The **"Done"** assessment is accurate for the HTTP API surface, workspace, skills, browser manager, and tests — these are genuinely complete.
- The dev record honestly acknowledges the 8 known gaps (no opencode adapter, no VNC integration test, no disk quota, no image build smoke test, no camoufox arch verification, no resource self-limits, no credential injection wiring). These are all confirmed.
- However, the dev record **overlooks** several spec compliance issues: `IAGENT_BRAIN` default mismatch (stub vs opencode), `_make_brain()` dead code for non-stub brains, missing callback payload fields (`ts`, `finished_at`), `POST /jobs` body shape mismatch (`inputs_dir`/`output_dir`), `IAGENT_AGENT_ID` never consumed, no multi-arch build support, no tmpfs for `/tmp`, and container security hardening absent from Dockerfile.
- The dev record should be updated with these additional gaps.
