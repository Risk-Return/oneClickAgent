# 04 — Agent Container (Generic Agent + HTTP API)

Each agent is one Docker container built on an **Ubuntu base image** with a **pre-installed toolchain** (default agent + headless browser + language runtimes), running a Python supervisor that exposes a **fixed HTTP API**. The spec defines the *contract, image, and lifecycle*; the concrete LLM/agent framework inside is swappable and **not bound** here.

## 1. Goals & Non-Goals

**Goals**
- Stable, minimal HTTP API the device can rely on regardless of the agent implementation.
- Ship a **ready-to-run image**: default agent (`opencode`), headless stealth browsers (`camoufox` and `cloakbrowser`), and pre-installed language runtimes + warmed dependency caches, so jobs need no on-the-fly installs.
- Execute exactly one job at a time, emit progress, return a progress-level result.
- Offer an **interactive VNC view** of the headless browser so a human can log in / take control live (relayed to the web UI through the device → gateway tunnel).
- Accept **injected login credentials** (cookies / browser storage state) for a job; never persist them — wipe with all other user data when the job ends.
- Manage its own skills. Wipe all user data after a job ends.

**Non-Goals**
- Talking to the gateway directly (only the device does).
- Persisting user data, browser profiles, or credentials across jobs.
- Exposing terminals/raw internals to users.
- Exposing the VNC/RFB server to the internet (reachable only via the device on loopback, then relayed over the tunnel).

## 2. Image Layout & Bundled Toolchain

```
agent/
├── Dockerfile             # ubuntu:24.04 base (see §10)
├── pyproject.toml
├── deps/                  # dependency manifests pre-installed at build time (see below)
│   ├── package.json       # global npm deps warmed into the image
│   ├── requirements.txt   # python deps
│   ├── go.mod             # go module cache warm-up
│   ├── Cargo.toml         # rust crate cache warm-up
│   └── pom.xml            # java deps (maven)
├── vnc/                   # entrypoint scripts: Xvfb + x11vnc + browser supervisor
└── iagent_agent/
    ├── server.py          # FastAPI app, routes
    ├── runtime/           # job executor, progress emitter, cancellation
    ├── browser/           # camoufox launcher, storage-state import/export, VNC control
    ├── skills/            # skill loader from manifests
    ├── workspace.py       # /work dir mgmt + cleanup
    └── adapter/           # pluggable "brain": maps command → actions (impl-specific)
```

### Bundled toolchain (baked into the image, per goal `docker` section)

| Component | Install (build time) | Purpose |
|-----------|----------------------|---------|
| Default agent: **opencode** | `npm i -g opencode-ai@latest` | the default coding/agent brain available to every container |
| Headless browser: **camoufox** | `npx @askjo/camofox-browser` | stealth Firefox-based browser driven headlessly; rendered to the Xvfb display for VNC |
| Headless browser: **cloakbrowser** | `pip install cloakbrowser` + `python -m cloakbrowser install` | stealth Chromium-based browser with 58 C++ source-level fingerprint patches; Playwright-compatible Python API; pre-downloaded binary (~206MB) |
| **Node.js 20 LTS** | nodesource / apt | JS/TS runtime + npm |
| **Python 3.12** | apt + `pip` | scripting + agent supervisor |
| **Go 1.22** | tarball to `/usr/local/go` | Go builds |
| **Rust (stable)** | `rustup` | Rust builds |
| **Java (Temurin 21 JDK)** | apt (adoptium) | JVM builds |
| Build basics | `build-essential`, `git`, `curl`, `ca-certificates` | common tooling |
| VNC stack | `xvfb`, `x11vnc` (+ minimal fonts/`fluxbox`) | virtual display + RFB server for §6 |
| Chromium system deps | `libatk-bridge2.0-0`, `libnss3`, `libgbm1`, etc. | system libraries required by cloakbrowser's Chromium binary |
| Emoji/Unicode fonts | `fonts-noto-color-emoji`, `fonts-freefont-ttf`, `fonts-unifont` | required for anti-bot canvas fingerprinting checks |

**Dependency pre-install (warm cache):** at build time the Dockerfile reads the manifests under `agent/deps/` and pre-installs them into the image (`npm ci`, `pip install -r`, `go mod download`, `cargo fetch`, `mvn dependency:go-offline`). This means jobs start with a warm dependency cache instead of downloading at runtime. The manifest set is the contract; updating a manifest + rebuilding the image refreshes the cache. Runtime installs are still possible but discouraged (slower, may need egress).

`adapter/` is the only part that changes per agent type; it implements:

```python
class AgentBrain(Protocol):
    async def run(self, ctx: JobContext, emit: ProgressEmitter) -> JobResult: ...
    async def cancel(self, job_id: str) -> None: ...
```

`JobContext` = `{ job_id, command, params, inputs_dir, output_dir, skill?, credentials_injected: bool, browser: { display, profile_dir, vnc_enabled } }` (at most one selected skill; `credentials_injected` is true when the device has loaded saved login storage-state into the browser profile before this job — see §7).

## 3. HTTP API

Bound to `0.0.0.0:<container_port>` (mapped to a fixed host port by the device). Only the device calls it; not internet-exposed.

| Method | Path | Body / Result |
|--------|------|---------------|
| GET | `/healthz` | `200 {status:"ok", busy:bool}` (liveness/readiness) |
| GET | `/status` | `{ current_job, usage:{cpu_pct, mem_mb, disk_mb}, skills:[...] }` |
| POST | `/jobs` | `{job_id, command, params, inputs_dir, output_dir, skill_id?, callback_url}` → `202 {job_id}` or `409 {code:"BUSY"}`. **At most one** `skill_id` |
| GET | `/jobs/{id}` | `{ job_id, status, percent, message, result?, error? }` |
| POST | `/jobs/{id}/cancel` | `{reason?}` → `202`; transitions to CANCELLED |
| GET | `/skills` | `[{skill_id, name, version, status}]` (`status` ∈ enabled/disabled) |
| POST | `/skills` | `{skill_id, name, version, manifest, artifact_path}` install or update (idempotent) |
| POST | `/skills/{id}/disable` | disable without removing (skill kept, not loaded) |
| POST | `/skills/{id}/enable` | re-enable a disabled skill |
| DELETE | `/skills/{id}` | remove the skill entirely |
| GET | `/vnc` | `{enabled, display, rfb_host:"127.0.0.1", rfb_port}` — info for the device's VNC bridge (loopback only, see §6) |
| POST | `/vnc/start` | start Xvfb + x11vnc + browser for the current job; `202 {rfb_port}` (idempotent) |
| POST | `/vnc/stop` | tear down the VNC stack for the current job |
| POST | `/browser/state` | inject login storage-state into the browser profile `{storage_state}` (cookies + localStorage, Playwright/Camoufox format) → `204` |
| GET | `/browser/state` | `?origin=` export current storage-state for capture/save-login → `{storage_state}` |

### Job execution flow

```
POST /jobs (busy? → 409)
  status=RUNNING; spawn executor task
  executor calls brain.run(ctx, emit)
  emit(percent, message) → push to device callback (below) + update in-memory state
  on done → status SUCCEEDED + result; on raise → FAILED + error
  finally → wipe workspace (inputs/scratch/output)
```

### Progress callback (preferred over polling)

Agent posts events to the device:

```
POST {callback_url}/jobs/{job_id}/events
  { event_seq, status, percent, message, ts }
  terminal: { event_seq, status:"SUCCEEDED"|"FAILED", result?, error?, finished_at }
```

If the callback fails, the device polls `GET /jobs/{id}`. `event_seq` is monotonic per job for idempotent ordering.

> **Progress-level only:** `message` and `result` must be user-presentable summaries. No raw logs, no chain-of-thought, no terminal/stdout streaming through this contract.

## 4. Workspace & Data Hygiene

```
/work/inputs/    # read-only, mounted by device (staged user files)
/work/scratch/   # agent temp
/work/output/    # produced artifacts (referenced in result, then wiped)
/work/profile/   # ephemeral browser profile (cookies/localStorage injected per job)
```

- On every job terminal state (success/fail/cancel), the runtime deletes the contents of `inputs`, `scratch`, `output`, **and `/work/profile` (including any injected cookies/credentials)**.
- No persistence between jobs; container restart starts clean. Injected credentials live **only** in `/work/profile` and in memory for the job's duration.
- Large output artifacts: returned via device→gateway file-pull flow (reserved, see `05-tunnel-protocol §5`); otherwise summarized in `result`.

## 5. Skills

- A skill is a declarative `manifest` (capability name, entrypoint ref, params schema, resource hints) plus an optional packaged artifact, installed into the agent by the device.
- The device pushes skills it received from the **cloud skill vault**; the agent never talks to the vault or gateway directly.
- `skills/` loads installed manifests/artifacts and exposes **enabled** skills to the `AgentBrain`.
- Lifecycle the agent must support: **install/update** (idempotent, by `skill_id`+`version`), **disable** (kept but not loaded/usable), **enable** (reload a disabled skill), **delete** (fully removed).
- Only `enabled` skills are usable in a job; a job carries **at most one** `skill_id`, which must be a currently `enabled` skill (else the job fails with `SKILL_NOT_ENABLED`).
- Skill state is reported via `/status` and `/skills`; device relays to gateway (`SKILL_STATE`).

## 6. Interactive Browser & VNC

When a job benefits from a human-in-the-loop (e.g. logging into a site, solving a challenge, taking over the browser), the customer can open a **live VNC view** of the container's headless browser from the web UI. The path is loopback-only inside the container and relayed outward exclusively through the device → gateway tunnel (the RFB server is **never** internet-exposed).

### Stack inside the container

```
camoufox / cloakbrowser (headless) ── renders to ──► Xvfb virtual display :99
                                                          ▲
                                                   x11vnc (RFB server) ── binds 127.0.0.1:5901 (loopback only)
```

- `Xvfb :99` provides a virtual framebuffer; `camoufox` is launched against `DISPLAY=:99`; `x11vnc` exports that display as an RFB stream on `127.0.0.1:<IAGENT_VNC_PORT>` (default `5901`).
- The VNC stack is started **on demand** (`POST /vnc/start`) for the current job and torn down on `POST /vnc/stop` or job terminal state. It is not running idle.
- The agent emits a one-time **RFB password** (random per session) on `/vnc/start`; the device passes it to the gateway via the trusted main tunnel so the relayed noVNC client can authenticate. The RFB port itself is bound to loopback and never published to the host network.

### Relay path (no inbound ports on device)

```
browser noVNC (RFB over WSS) ⇄ Gateway VNC relay ⇄ device session WS ⇄ device TCP↔WS bridge ⇄ 127.0.0.1:5901 (x11vnc)
```

The device acts as the `websockify`-equivalent (TCP ↔ WebSocket bridge); the gateway relays raw RFB bytes between the browser socket and the device session socket. The agent only needs to speak plain RFB on loopback. Full wire protocol: `05-tunnel-protocol §9`; gateway relay: `02-cloud-gateway §16`.

## 7. Credential Injection (Login Cookies)

Logins captured during a VNC session are stored **encrypted in the cloud gateway** (`06-data-model §1.16`) and re-injected into the container's browser when a later job is assigned, so the agent's browser starts already logged in.

- **Inject (job start):** the device calls `POST /browser/state {storage_state}` with the decrypted storage-state the gateway pushed for the referenced `credential_ids` (see `05-tunnel-protocol §10`). The agent writes it into the ephemeral `/work/profile` before `brain.run`. `JobContext.credentials_injected = true`.
- **Capture (save login):** while a VNC session is active, the user logs into a site; on "save login" the device calls `GET /browser/state?origin=<site>`; the agent exports the current cookies + localStorage; the device relays it to the gateway for encryption + storage (`CRED_CAPTURE`).
- **Hygiene:** storage-state is never written outside `/work/profile`, never logged, never returned in `/status`, and is wiped on job terminal state with the rest of the workspace. The agent never sees the encryption key or which user owns the credential.

## 8. Resource Self-Limits

- Container limits applied by the device (cpu/mem/disk). The agent also self-reports `usage` in `/status`.
- Default limits: `cpu=2, mem=4GB, disk=10GB`.
- Agent should fail fast with a clear `error` if a job exceeds workspace disk quota.

## 9. Configuration (env)

| Var | Default | Purpose |
|-----|---------|---------|
| `IAGENT_AGENT_PORT` | `8090` | container HTTP port |
| `IAGENT_AGENT_ID` | injected | identity (label-matched) |
| `IAGENT_WORK_DIR` | `/work` | workspace root |
| `IAGENT_BRAIN` | `opencode` | selects the AgentBrain adapter (default uses bundled `opencode`) |
| `IAGENT_VNC_ENABLED` | `true` | allow interactive VNC sessions for jobs |
| `IAGENT_VNC_PORT` | `5901` | loopback RFB port for x11vnc (never host-published) |
| `IAGENT_VNC_DISPLAY` | `:99` | Xvfb virtual display |
| `IAGENT_BROWSER_CMD` | `camoufox` | headless browser launcher: `camoufox` (Firefox-based) or `cloakbrowser` (Chromium-based, Playwright API) |
| `IAGENT_BROWSER_PROFILE` | `/work/profile` | ephemeral browser profile dir (wiped per job) |
| `CLOAKBROWSER_CACHE_DIR` | `/home/app/.cloakbrowser` | cloakbrowser Chromium binary cache directory |
| (LLM creds) | impl-specific | provided as secrets at create time, never logged |

## 10. Dockerfile Requirements

- **Base: `ubuntu:24.04`** (LTS) per the goal's Docker requirements (Linux, ideally Ubuntu). Non-root user `app`; `HEALTHCHECK` hitting `/healthz`.
- **Multi-stage build** to keep the final image lean despite the toolchain: a `builder` stage warms language/dependency caches (§2), the final stage copies the warmed caches + installs runtimes.
- Bundled toolchain installed at build (see §2 table): opencode, camoufox, cloakbrowser, Node 20, Python 3.12, Go 1.22, Rust stable, Temurin 21, plus the Xvfb/x11vnc VNC stack and Chromium system libraries.
- Pre-installed dependency caches from `agent/deps/` manifests (warm `npm`/`pip`/`go`/`cargo`/`maven`).
- Writable areas limited to `/work` (volume) + tmpfs `/tmp`; the browser profile and X runtime dirs live under `/work` so the rest of the root fs can stay read-only-friendly. (A full `read_only` root is best-effort given the browser; X/profile writable dirs are confined to `/work`.)
- No SSH, no internet-exposed daemons. Processes: uvicorn supervisor (always) + on-demand Xvfb/x11vnc/browser (only during a VNC-enabled job).
- The RFB port (`5901`) is **EXPOSE**d for documentation only and bound to loopback at runtime; it is **never** published to the host (`-p`) — the device reaches it via the container bridge address on loopback.
- Multi-arch build (`linux/amd64`, `linux/arm64`) so it runs under Docker Desktop on both Intel and Apple Silicon. Note: camoufox/runtime availability per arch must be verified at build (see `10-deployment §4`).
- **Image size:** the bundled toolchain makes this image large (multi-GB). Devices **pre-pull** it (`03-local-device §7`); the per-agent `disk` limit default may need raising for browser-heavy jobs (configurable, see §8).

## 11. Security

- Never internet-facing; only the device on loopback/bridge network. The RFB/VNC port is loopback-bound and reachable solely through the device's TCP↔WS bridge.
- Drops capabilities, non-root, no host network, pids limit.
- Secrets (LLM creds, injected login storage-state) passed via env/secret mount or `POST /browser/state` at create/job time; excluded from `/status` and logs. Storage-state is confined to `/work/profile` and wiped on job terminal state.
- Per-session random RFB password; the VNC stack runs only during a session and is torn down after.
- Detail in `08-auth-security.md`.

## 12. Testing

- Contract tests against the HTTP API with a **mock brain** (deterministic progress + result).
- Verify workspace wipe on success/fail/cancel, **including `/work/profile` (injected credentials) removal**.
- Verify `409 BUSY` when a second job is dispatched.
- Verify callback + polling fallback behavior.
- VNC: `POST /vnc/start` brings up Xvfb+x11vnc on loopback; an RFB handshake succeeds through a local bridge; `POST /vnc/stop` and job terminal tear it down.
- Credentials: `POST /browser/state` injection makes the browser logged-in; `GET /browser/state` round-trips storage-state; state never appears in `/status` or logs.
- Image: build smoke test asserts opencode, camoufox, node/python/go/rust/java are present and dependency caches are warm.
