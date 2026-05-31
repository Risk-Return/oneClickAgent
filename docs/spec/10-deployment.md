# 10 — Deployment & Operations

How to build, configure, and run each component. Two installations from the goal:

- **Cloud Gateway installation**: manages the device fleet, agents, tunnels, user data, skill vault, web UI. Public.
- **Local Device installation**: admin-operated; manages agents (containers), tunnels, local DB. Private.

> **Roles:** an **admin (operator)** deploys/enrolls local devices and manages the skill vault; **customers** only register agents and run jobs via the web UI. Device deployment below is an admin task.

> **Cross-platform requirement:** the device + agent toolchain must run on **Windows and macOS** (Docker Desktop) as well as Linux. The gateway is typically Linux but builds for all three.

## 1. Build Artifacts

| Component | Artifact | Toolchain |
|-----------|----------|-----------|
| Gateway | static Go binary + container image | `go build`, multi-arch image |
| Device | Python package + CLI (`iagent-device`) | `pyproject.toml`, `pipx`/venv |
| Agent | Docker image `iagent/agent` (**Ubuntu 24.04** base, multi-arch, multi-GB) | `docker buildx` amd64+arm64 |
| Web | static bundle (served by gateway or CDN) | `vite build` |

## 2. Cloud Gateway Deployment

### Topology
```
[Internet] → [TLS LB / reverse proxy] → [gateway:8080] → [PostgreSQL]
                                              └→ [file staging (local disk or S3)]
```

- TLS terminated at LB or in-gateway. WSS (`/tunnel`, `/ws`) must support long-lived upgrades (disable proxy buffering / set generous idle timeouts).
- Stateless app; scale by adding instances. v1 = single instance for tunnel affinity; multi-instance requires a shared device registry (Redis) — interface reserved.

### docker-compose (reference, `deploy/cloud/`)
```yaml
services:
  postgres:
    image: postgres:15
    environment: [POSTGRES_DB=iagent, POSTGRES_USER=iagent, POSTGRES_PASSWORD=...]
    volumes: [pgdata:/var/lib/postgresql/data]
  gateway:
    image: iagent/gateway:latest
    environment:
      IAGENT_HTTP_ADDR: ":8080"
      IAGENT_DB_URL: "postgres://iagent:...@postgres:5432/iagent"
      IAGENT_JWT_SECRET: "${JWT_SECRET}"
      IAGENT_FILE_STORE: "local:/data/files"
    ports: ["8080:8080"]
    depends_on: [postgres]
    volumes: [files:/data/files]
volumes: { pgdata: {}, files: {} }
```

- Run migrations on startup or as a one-shot job (`golang-migrate`).
- Reverse proxy (nginx/Caddy/Traefik) for TLS + HTTP/WS routing in front.

## 3. Local Device Deployment (admin task)

Performed by an **admin/operator**, not customers. The admin first registers the device in the admin console to obtain an enrollment code.

### Prerequisites
- Docker installed and running (Docker Desktop on Win/macOS, Engine on Linux).
- Network egress to the gateway over 443 (WSS). No inbound ports required.

### Install & run
```
pipx install iagent-device         # or: pip install in a venv
iagent-device enroll --gateway https://gw.example.com --code <ENROLLMENT_CODE>
iagent-device run                  # foreground; or install as a service
```

### Run as a background service
- **macOS**: `launchd` plist (`~/Library/LaunchAgents/com.iagent.device.plist`).
- **Windows**: Windows Service via `nssm` or a Scheduled Task at logon.
- **Linux**: `systemd` unit.

### Data locations (platform-aware via `platformdirs`)
- macOS: `~/Library/Application Support/iagent-device/`
- Windows: `%LOCALAPPDATA%\iagent-device\`
- Linux: `~/.local/share/iagent-device/`

Contains SQLite DB + per-job workspaces (auto-cleaned).

## 4. Agent Image

Built/published as `iagent/agent:<version>` (multi-arch). The image is **Ubuntu 24.04**-based and ships a full toolchain, so it is large (multi-GB); devices **pre-pull** it before serving jobs.

### Contents (per `04-agent-container §2/§10`)

- Default agent **opencode** (`npm i -g opencode-ai@latest`) and headless browser **camoufox** (`npx @askjo/camofox-browser`).
- Language runtimes: **Node 20, Python 3.12, Go 1.22, Rust stable, Temurin 21 JDK** + `build-essential`, `git`, `curl`.
- VNC stack: **Xvfb + x11vnc** (+ minimal fonts/fluxbox) for interactive browser sessions.
- **Warm dependency caches** built from `agent/deps/` manifests (`package.json`, `requirements.txt`, `go.mod`, `Cargo.toml`, `pom.xml`).

### Build & publish

```
docker buildx build --platform linux/amd64,linux/arm64 \
  -t iagent/agent:<version> -t iagent/agent:latest agent/ --push
```

- **Multi-stage** build (builder warms caches → slim final stage). Verify each runtime + opencode + camoufox are present via an image smoke test in CI.
- **arch caveat**: confirm camoufox + runtime availability on `arm64`; if a dependency is amd64-only, publish an amd64-only tag and document it.
- Layer ordering: runtimes/toolchain first (rarely change), dependency caches next, app last — maximizes layer reuse and keeps rebuilds fast.

### Runtime

- Device creates containers with labels `iagent.agent_id`, fixed host **HTTP** port, resource limits, hardening flags (see `08-auth-security §6`). The **RFB port is loopback-only and never published** (`-p`).
- Disk: the bundled toolchain + browser is sizeable; the per-agent `disk` limit default may need raising for browser-heavy jobs (configurable).
- Custom agent types = alternate images implementing the same HTTP contract (`04-agent-container.md`).

## 5. Configuration Summary

| Component | Source | Key vars |
|-----------|--------|----------|
| Gateway | env / `.env` | `IAGENT_DB_URL`, `IAGENT_JWT_SECRET`, `IAGENT_FILE_STORE`, `IAGENT_HTTP_ADDR`, `IAGENT_QUEUE_TTL`, `IAGENT_MAX_QUEUED_PER_USER`, `IAGENT_VNC_IDLE_TTL`, `IAGENT_VNC_MAX_TTL`, `IAGENT_VNC_MAX_SESSIONS_PER_USER`, **`IAGENT_CRED_KEY`** (or `IAGENT_CRED_KMS`) |
| Device | env / config file | `IAGENT_GATEWAY_URL`, `IAGENT_AGENT_IMAGE`, `IAGENT_PREPULL_IMAGE`, `IAGENT_PORT_RANGE`, `IAGENT_MAX_RESTARTS`, `IAGENT_SESSION_DIAL_TIMEOUT_S` |
| Agent | env at create | `IAGENT_AGENT_PORT`, `IAGENT_AGENT_ID`, `IAGENT_BRAIN`, `IAGENT_VNC_ENABLED`, `IAGENT_VNC_PORT`, `IAGENT_BROWSER_CMD`, LLM secrets |
| Web | build-time env | `VITE_API_BASE`, `VITE_WS_BASE` |

> **Credential vault key required:** the gateway will refuse to start (or to serve credential routes) without `IAGENT_CRED_KEY`/`IAGENT_CRED_KMS`. Generate a 32-byte key: `openssl rand -base64 32`. Store via secret manager; never commit.

> **WSS for sessions:** the reverse proxy must also pass through `wss://.../session/{id}` and `wss://.../ws/vnc/{id}` (long-lived, binary upgrades) in addition to `/tunnel` and `/ws`.

Secrets via environment/secret manager; never committed.

## 6. Environments

- **dev**: gateway + postgres via compose; device run locally pointing at it; self-signed TLS allowed with explicit opt-in; agent image built locally.
- **staging/prod**: managed PostgreSQL, real TLS certs, image registry, monitoring enabled.

## 7. Observability & Ops

- **Gateway**: `/healthz`, `/readyz`, `/metrics` (Prometheus, internal only); structured logs.
- **Device**: local logs (diagnostic, not exposed to users via gateway), `iagent-device status`.
- **Agent**: `HEALTHCHECK` + `/healthz`; device aggregates health.
- Alerts: device offline, agent restart-loop, gateway error rate, DB saturation.
- Backups: PostgreSQL automated backups; device SQLite is reconstructable (not critical to back up).

## 8. CI/CD

| Stage | Action |
|-------|--------|
| Lint | `golangci-lint`, `ruff`/`mypy`, `eslint`/`tsc` |
| Test | Go unit+integration (testcontainers PG), Python unit (mock Docker) + integration (real Docker), web Vitest |
| Cross-platform | device/agent tests on Windows + macOS + Linux runners |
| Security | `govulncheck`, `pip-audit`, `npm audit`, Trivy image scan |
| Build | Go binary + multi-arch images (`buildx`), web bundle, Python wheel |
| Deploy | push images, run migrations, rollout gateway; publish device package + agent image |

## 9. Release & Versioning

- Semantic versioning per component.
- Tunnel protocol versioned independently (`iagent.tunnel.v1`); gateway may support N and N-1 during rollout.
- Backward-compatible DB migrations; forward-only with paired up/down.

## 10. Rollback

- Gateway: redeploy previous image; migrations designed to be backward-compatible within a minor series.
- Device: `pipx install iagent-device==<prev>`; reconciles Docker on restart.
- Agent: devices pin image tag; revert by changing `IAGENT_AGENT_IMAGE`.

## 11. End-to-End Verification (per goal's E2E step)

1. Bring up gateway + postgres (compose); seed an **admin** user.
2. **As admin**: register a device → get enrollment code; enroll + run `iagent-device` on a machine with Docker → device shows ONLINE; configure pool size (e.g., 2 agents).
3. **As admin**: publish a skill version to the vault → install it fleet-wide → confirm rollout `installed`; set it `public` (or grant a customer). Set a customer's tier to `pro`.
4. **As customer**: register, log in → see pool agents auto-created on device (status IDLE). Submit a job with an uploaded file → agent auto-allocated → observe live progress → receive result.
5. Verify file workspace is wiped after job completion.
6. Cancel a job mid-run → status CANCELLED.
7. **Queue test**: submit N+1 jobs (where N = pool size). Verify the extra job enters QUEUED state with queue_position. Verify tier ordering: enterprise jobs dequeued before pro, pro before free. Verify QUEUE_TIMEOUT after TTL expiry.
8. **Queue cap test**: submit beyond per-user queue cap → verify 429 QUEUE_FULL.
9. Kill the tunnel → confirm reconnect + buffered result delivery.
10. Verify a customer cannot see devices or other customers' agents/skills (authz).
11. **Agent image**: confirm the device **pre-pulled** the Ubuntu image and that a container reports opencode + camoufox + node/python/go/rust/java present (`GET /status`).
12. **VNC**: on a running job, **as customer** open Browser Control → noVNC connects and renders the headless browser → interact (move mouse / type) → confirm the RFB port is loopback-only (not published on the host).
13. **Save login**: log into a test site in the VNC view → "Save login" → verify an encrypted row in `browser_credentials` (ciphertext, no plaintext) → entry appears in Saved Logins.
14. **Inject**: submit a new job attaching that `credential_id` → confirm the agent browser starts already signed in → confirm `/work/profile` (cookies) is wiped on job terminal.
15. **Session teardown**: verify the VNC session auto-closes on job terminal / idle / max-TTL, and the agent VNC stack is torn down.
