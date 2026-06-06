# Local Device Deployment Guide

Deploy the IAgent local device with agent Docker containers and connect to a cloud gateway.

## Prerequisites

| Tool | Min Version | Check |
|------|-------------|-------|
| Docker | 24+ | `docker info` |
| Python | 3.11+ | `python3 --version` |
| Git | any | `git --version` |
| Go | 1.21+ (only if building gateway locally) | `go version` |
| PostgreSQL | 15+ (only if running gateway locally) | `psql --version` |

The user must be in the `docker` group:
```bash
sudo usermod -aG docker $USER
newgrp docker  # or log out/in
```

## 1. Build the Agent Image

```bash
cd agent

# Lightweight dev image (Python + stub brain, ~200MB)
docker build -f Dockerfile.dev -t iagent/agent:dev .

# VNC image (adds Xvfb, x11vnc, chromium, ~400MB)
docker build -f Dockerfile.vnc -t iagent/agent:vnc .

# Full production image (opencode, camoufox, all toolchains, ~4GB, ~60 min)
# Requires: PEP 668 fix already applied in Dockerfile
docker build -t iagent/agent:latest .

# Camofox browser image (Node.js 20 + camofox-browser API server, ~2GB, ~15 min)
# Provides REST API on port 9377 for AI agents to control an anti-detection browser
docker build -f Dockerfile.camoufox -t iagent/agent:camoufox .
```

### Image Variant Comparison

| Image | Dockerfile | Size | Contents | Use Case |
|-------|-----------|------|----------|----------|
| `iagent/agent:dev` | `Dockerfile.dev` | ~200MB | Python + stub brain | Quick testing |
| `iagent/agent:vnc` | `Dockerfile.vnc` | ~400MB | + Xvfb, x11vnc, chromium | Interactive VNC login |
| `iagent/agent:camoufox` | `Dockerfile.camoufox` | ~2GB | + Node.js, camofox-browser API (lite Camoufox) | Browser automation with anti-detection |
| `iagent/agent:latest` | `Dockerfile` | ~4GB | + all toolchains, opencode | Full production |

### Camofox Browser Pipeline

The `camoufox` variant provides a REST API server (`@askjo/camofox-browser`) on port 9377
that agents use to control an anti-detection Camoufox browser:

```bash
# Build the image (Node.js + camofox-browser pre-installed with lite Camoufox)
docker build -f Dockerfile.camoufox -t iagent/agent:camoufox .

# Run the container (camoufox API on :9377, agent API on :8090)
docker run -d --name agent-camofox -p 8090:8090 -p 9377:9377 \
  -v camoufox-cache:/home/app/.cache \
  iagent/agent:camoufox

# Verify the browser server is ready
curl http://localhost:9377/health
# {"ok":true,"engine":"camoufox","browserConnected":true}

# The browser API endpoints:
# POST /tabs              - Create a new browser tab
# GET  /tabs/:id/snapshot - Get accessibility tree + optional screenshot
# POST /tabs/:id/click    - Click element by ref
# POST /tabs/:id/type     - Type text into element
# POST /tabs/:id/navigate - Navigate to URL or search macro
# GET  /sessions/:uid/storage_state - Export cookies/localStorage
```

> **IMPORTANT — Camoufox Binary:** The `camofox-browser` npm package includes a
> **lite version of Camoufox**. There is **no need** to separately download the
> `camoufox` Python binary (`pip install camoufox`) or run `camoufox-js fetch`.
> The npm package bundles its own lightweight Camoufox engine. Installing
> `@askjo/camofox-browser` via npm is sufficient — the server handles browser
> launch internally. Do NOT run `npx camoufox-js fetch` or `pip install camoufox`
> separately; these are different packages and will conflict with the lite version.

### AGENTS.md — Agent Skill Instructions

The opencode brain reads skill instructions from `~/.claude/skills/{skill_name}/SKILL.md`
inside the agent container. The `Dockerfile.camoufox` automatically copies
`docs/reference/camoufox_browser/AGENTS.md` to `~/.claude/skills/default/SKILL.md`.

To add custom skill instructions for a specific website:

```bash
# Copy the AGENTS.md reference to the default skill
mkdir -p ~/.claude/skills/default
cp docs/reference/camoufox_browser/AGENTS.md ~/.claude/skills/default/SKILL.md

# For a named skill (e.g., "xiaohongshu"):
mkdir -p ~/.claude/skills/xiaohongshu
cp your-custom-instructions.md ~/.claude/skills/xiaohongshu/SKILL.md
```

The agent reads SKILL.md at job execution time. It contains:
- Browser API reference (create tab, navigate, snapshot, click, type)
- Search macros (@google_search, @youtube_search, etc.)
- Session management commands
- VNC login instructions for interactive authentication

### Verify the image

```bash
docker run --rm -d --name agent-test -p 8090:8090 iagent/agent:dev
sleep 3
curl -s http://localhost:8090/healthz
# {"status":"ok","busy":false}
docker rm -f agent-test
```

## 2. Install the Device

```bash
cd device

# Create venv (Ubuntu 24.04+ enforces PEP 668)
python3 -m venv venv
source venv/bin/activate

pip install -e .

# Verify
iagent-device status
# Error: not enrolled (expected on first run)
```

## 3. Configure Environment

```bash
# Required
export IAGENT_GATEWAY_URL="https://your-gateway.example.com"

# Agent image to use
export IAGENT_AGENT_IMAGE="iagent/agent:dev"  # or iagent/agent:latest

# Agent pool size (containers to pre-warm)
export IAGENT_POOL_SIZE=2

# Optional: custom data directory (default: ~/.local/share/iagent-device)
export IAGENT_DEVICE_DATA_DIR="/opt/iagent-device/data"

# Optional: Docker host override
export IAGENT_DOCKER_HOST="unix:///var/run/docker.sock"
```

For systemd deployment, place these in `/etc/default/iagent-device`:
```
IAGENT_GATEWAY_URL="https://gateway.prod.example.com"
IAGENT_AGENT_IMAGE="iagent/agent:latest"
IAGENT_POOL_SIZE=4
IAGENT_DEVICE_DATA_DIR="/var/lib/iagent-device"
```

## 4. Enroll with the Gateway

Get an enrollment code from the gateway admin:
1. Admin creates a device via `POST /api/v1/devices` (or admin UI)
2. Gateway returns an `enrollment_code`
3. Pass the code to the device:

```bash
source venv/bin/activate
iagent-device enroll "019e97bd-d3d6-7d35-bd71-4141e7a33d53"
# Enrolled as device 019e97bd-xxxx-xxxx-xxxx-xxxxxxxxxxxx
```

After enrollment, the device ID and token are stored in SQLite at `$IAGENT_DEVICE_DATA_DIR/device.db`.

## 5. Run the Device

```bash
source venv/bin/activate
iagent-device run
```

The device will:
1. Reconcile Docker containers (ensure `pool_size` agents are running)
2. Connect to the gateway via WebSocket tunnel (`wss://gateway/tunnel`)
3. Send HELLO with agent list, platform info, and capabilities
4. Start health monitoring (every 10s)
5. Listen for frames: JOB_DISPATCH, AGENT_CREATE, SKILL_DISPATCH, FILE_PUSH, VNC_OPEN, etc.

```bash
# Background with logging
nohup iagent-device run > /var/log/iagent-device.log 2>&1 &
```

## 6. Set Pool Size on Gateway

Once the device shows as **online** in the admin dashboard:

```bash
# Admin API call to trigger AGENT_CREATE frames
curl -X POST "https://gateway/api/v1/admin/devices/{device_id}/pool" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"size": 2}'
```

The gateway will:
1. Create agent records in PostgreSQL with unique UUIDs
2. Send AGENT_CREATE frames to the device via the tunnel
3. The device creates Docker containers with matching agent IDs
4. Monitors send AGENT_STATUS → agents appear as "idle" in the dashboard

## 7. Verify Everything Works

```bash
# Check device status
iagent-device status

# Check running containers
docker ps --filter 'label=iagent.pool=true'

# Check agent health
curl -s http://localhost:{agent_port}/healthz

# Check tunnel connection (in gateway logs)
grep "tunnel" /var/log/gateway.log
```

## systemd Unit (Recommended for Production)

```ini
# /etc/systemd/system/iagent-device.service
[Unit]
Description=IAgent Local Device
After=docker.service network-online.target
Wants=docker.service network-online.target

[Service]
Type=simple
User=ryandong
Group=docker
EnvironmentFile=/etc/default/iagent-device
WorkingDirectory=/opt/iagent-device
ExecStartPre=/opt/iagent-device/venv/bin/pip install -e /opt/iagent-device --quiet
ExecStart=/opt/iagent-device/venv/bin/python -m iagent_device run
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=iagent-device

[Install]
WantedBy=multi-user.target
```

Then:
```bash
sudo systemctl daemon-reload
sudo systemctl enable iagent-device
sudo systemctl start iagent-device
sudo journalctl -u iagent-device -f
```

## Data Directory Layout

```
$IAGENT_DEVICE_DATA_DIR/
├── device.db          # SQLite: device info, agents, jobs, outbox, files, skills
├── workspaces/        # Per-job workspaces (mounted into containers at /workspaces)
│   └── {job_id}/
│       └── inputs/    # Staged input files (read-only in container)
└── skills/            # Downloaded skill packages
```

## Troubleshooting

### Device won't connect to gateway
```bash
# Check gateway is reachable
curl -s $IAGENT_GATEWAY_URL/healthz

# Check device token is valid (re-enroll if expired)
iagent-device status

# Check websocket subprotocol match
# Device sends: Sec-WebSocket-Protocol: iagent.tunnel.v1
# Gateway must respond with the same protocol
```

### Agent containers keep restarting
```bash
# Check container logs
docker logs agent-{id}

# Common causes:
# - Permission denied (UID mismatch between image and host)
# - Missing environment variables
# - Disk quota exceeded
docker inspect agent-{id} | jq '.[0].State'
```

### Pool sets fail (agents never created)
```bash
# Device must be ONLINE before setting pool size
# Gateway only sends AGENT_CREATE to connected devices

# Check gateway logs for "failed to send AGENT_CREATE"
# If device is offline, start the device first, then set pool
```

### Docker permission on Ubuntu
```bash
# If docker commands require sudo:
sudo usermod -aG docker $USER
newgrp docker

# If still failing, check socket permissions:
ls -la /var/run/docker.sock
# Should be: srw-rw---- root docker
```

## Network Requirements

| Direction | Protocol | Port | Purpose |
|-----------|----------|------|---------|
| Device → Gateway | HTTPS | 443 | Enrollment (POST /api/v1/devices/enroll) |
| Device → Gateway | WSS | 443 | Tunnel (WebSocket upgrade at /tunnel) |
| Device → Docker | Unix socket | — | Container management |
| Device → Agent | HTTP | 42000+ | Health checks, job dispatch |
| Agent → Device | HTTP | dynamic | Progress callbacks (POST /jobs/{id}/events) |

The device initiates ALL outbound connections. No inbound ports needed.
