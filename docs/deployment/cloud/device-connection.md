# Device Connection Guide

Connect a local machine running the IAgent device service to a cloud gateway. The device manages a pool of Docker agent containers and communicates exclusively through an outbound WebSocket tunnel.

## Architecture

```
[Agent Containers] ← HTTP → [Device] → WSS tunnel → [Cloud Gateway] → [Web UI]
    (Docker)           (Python)     (port 443)       (Go + Postgres)
```

- The device **dials out** — no inbound ports needed.
- One tunnel per device. All frames (jobs, skills, files, VNC) flow through this tunnel.

## Prerequisites

| Requirement | Check |
|-------------|-------|
| Docker installed + running | `docker info` |
| Python 3.11+ | `python3 --version` |
| Outbound HTTPS/WSS to gateway | `curl -s https://gateway.example.com/healthz` |
| User in `docker` group | `groups \| grep docker` |

## 1. Obtain Enrollment Code

An admin must first register the device through the gateway. The admin portal generates an enrollment code.

**Admin flow:**
1. Log into `https://gateway.example.com/admin`
2. Navigate to **Device Fleet**
3. Click **Add Device** → enter a name and description
4. Copy the enrollment code shown in the dialog

The enrollment code is a one-time token that links the device to its record in the cloud database.

## 2. Install the Device

```bash
cd device

# Create virtual environment
python3 -m venv venv
source venv/bin/activate

# Install the device package
pip install -e .

# Verify
iagent-device status
# Expected: "Not enrolled" (on first run)
```

## 3. Enroll

```bash
source venv/bin/activate

# Set the gateway URL
export IAGENT_GATEWAY_URL="https://your-gateway.example.com"

# Enroll with the code from the admin portal
iagent-device enroll <ENROLLMENT_CODE>

# Expected output:
# Device enrolled: 019e97bd-xxxx-xxxx-xxxx-xxxxxxxxxxxx
```

After enrollment, the device ID and token are persisted in SQLite at `$IAGENT_DEVICE_DATA_DIR/device.db`.

## 4. Configure

Set required environment variables:

```bash
# Gateway URL (required — same as enrollment URL)
export IAGENT_GATEWAY_URL="https://your-gateway.example.com"

# Agent Docker image to run
export IAGENT_AGENT_IMAGE="iagent/agent:latest"

# Number of pre-provisioned agent containers
export IAGENT_POOL_SIZE=4

# Optional: custom data directory
export IAGENT_DEVICE_DATA_DIR="/var/lib/iagent-device"

# Optional: Docker host (defaults to platform default)
export IAGENT_DOCKER_HOST="unix:///var/run/docker.sock"
```

For production, persist these in `/etc/default/iagent-device`:

```
IAGENT_GATEWAY_URL="https://your-gateway.example.com"
IAGENT_AGENT_IMAGE="iagent/agent:latest"
IAGENT_POOL_SIZE=4
IAGENT_DEVICE_DATA_DIR="/var/lib/iagent-device"
```

## 5. Build Agent Image

The device uses Docker agent containers. Build the image first:

```bash
cd agent

# Production image (opencode, camoufox, all toolchains — ~4 GB, ~60 min build)
docker build -t iagent/agent:latest .

# Or dev image (stub brain, lightweight — ~200 MB)
docker build -f Dockerfile.dev -t iagent/agent:dev .
```

Verify the image:

```bash
docker run --rm -d --name agent-test -p 8090:8090 iagent/agent:dev
sleep 3
curl -s http://localhost:8090/healthz
# {"status":"ok","busy":false}
docker rm -f agent-test
```

## 6. Run the Device

```bash
source venv/bin/activate

# Foreground (for testing)
iagent-device run

# Background
nohup iagent-device run > /var/log/iagent-device.log 2>&1 &
```

The device will:
1. Pull the agent image (if `IAGENT_PREPULL_IMAGE=true`)
2. Reconcile Docker containers — create `pool_size` idle agents, remove surplus
3. Open a WSS tunnel to the gateway at `{gateway_url}/tunnel`
4. Send HELLO with agent list, platform info, resource stats
5. Start heartbeat loop (PING every 15s)
6. Listen for frames: JOB_DISPATCH, SKILL_DISPATCH, FILE_PUSH, VNC_OPEN, etc.

On the admin dashboard, the device should appear as **online** within seconds.

## 7. Set Pool Size

Once the device is online, the admin can set the agent pool size through the gateway:

```bash
# Admin API call
curl -X POST "https://gateway.example.com/api/v1/admin/devices/{device_id}/pool" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"size": 4}'
```

This sends `AGENT_CREATE` frames to the device. The device creates the containers and reports them back via `AGENT_STATUS`. When agents show as **idle**, they're ready to accept jobs.

## 8. systemd Unit (Linux)

```ini
# /etc/systemd/system/iagent-device.service
[Unit]
Description=IAgent Local Device
After=docker.service network-online.target
Wants=docker.service network-online.target

[Service]
Type=simple
User=iagent
Group=docker
EnvironmentFile=/etc/default/iagent-device
WorkingDirectory=/opt/iagent-device
ExecStart=/opt/iagent-device/venv/bin/python -m iagent_device run
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable iagent-device
sudo systemctl start iagent-device
sudo journalctl -u iagent-device -f
```

## 9. Verify

```bash
# Device status
iagent-device status

# Running containers
docker ps --filter 'label=iagent.pool=true'

# Agent health (port from docker ps)
curl -s http://localhost:42000/healthz

# Check device online in admin dashboard
curl -s https://gateway.example.com/api/v1/devices \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Network Requirements

| Direction | Protocol | Port | Purpose |
|-----------|----------|------|---------|
| Device → Gateway | HTTPS | 443 | Enrollment (`POST /api/v1/devices/enroll`) |
| Device → Gateway | WSS | 443 | Tunnel (WebSocket at `/tunnel`) |
| Device → Docker | Unix socket | — | Container management |
| Device → Agent | HTTP | 42000-42999 | Health checks, job dispatch |

## Troubleshooting

### Device won't connect

```bash
# Check gateway reachable
curl -s $IAGENT_GATEWAY_URL/healthz

# Check device registered
iagent-device status

# Re-enroll if token expired
iagent-device enroll <NEW_CODE>
```

### Agent containers restarting

```bash
# Check container logs
docker logs agent-<id>

# Common causes:
# - Permission denied (UID mismatch)
# - Missing environment variables
# - Disk space exhausted
docker inspect agent-<id> | jq '.[0].State'
```

### Device offline in dashboard

```bash
# Check tunnel connection
ss -tnp | grep 443

# Check gateway logs for connection errors
journalctl -u iagent-device -f
```

### Pool size not applied

- Device must be **online** before setting pool size.
- Gateway only sends `AGENT_CREATE` to connected devices.
- Check gateway logs for "failed to send AGENT_CREATE".

## Data Directory Layout

```
$IAGENT_DEVICE_DATA_DIR/
├── device.db          # SQLite: device info, agents, jobs, outbox, files, skills
├── workspaces/        # Per-job workspaces (mounted into containers at /workspaces)
│   └── {job_id}/
│       ├── inputs/    # Staged input files
│       └── output/    # Agent-generated results (relayed to cloud)
└── skills/            # Downloaded skill packages
```
