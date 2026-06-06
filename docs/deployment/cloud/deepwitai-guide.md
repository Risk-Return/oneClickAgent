# deepwitai.cn — Production Deployment Guide

Connect a local device to the IAgent cloud gateway at `https://deepwitai.cn/aiproduct`.

> The gateway runs behind an existing nginx reverse proxy at path prefix `/aiproduct/`. All API, WebSocket, and tunnel endpoints are under this prefix.

## Endpoints

| Purpose | Endpoint |
|---------|----------|
| Web UI (admin/customer) | `https://deepwitai.cn/aiproduct/` |
| REST API | `https://deepwitai.cn/aiproduct/api/v1/` |
| Device tunnel (WSS) | `wss://deepwitai.cn/aiproduct/tunnel` |
| Realtime events (WSS) | `wss://deepwitai.cn/aiproduct/ws` |
| VNC session relay (WSS) | `wss://deepwitai.cn/aiproduct/session/{id}` |
| VNC binary relay (WSS) | `wss://deepwitai.cn/aiproduct/ws/vnc/{id}` |
| Gateway health | `https://deepwitai.cn/aiproduct/healthz` |

## 1. Obtain Enrollment Code

1. Log into the admin portal: **https://deepwitai.cn/aiproduct/**
2. Navigate to **Device Fleet** in the sidebar
3. Click **Add Device** → enter a name and description
4. Copy the one-time **enrollment code** from the dialog
5. Share the code with the device operator (or use it on the same machine)

## 2. Install Device Software

```bash
cd device

# Create virtual environment
python3 -m venv venv
source venv/bin/activate

# Install the device package
pip install -e .
```

## 3. Enroll

```bash
source venv/bin/activate

# Set the gateway URL (with /aiproduct prefix)
export IAGENT_GATEWAY_URL="https://deepwitai.cn/aiproduct"

# Enroll using the code from admin portal
iagent-device enroll <ENROLLMENT_CODE>

# Expected output:
# Device enrolled: 019e97bd-xxxx-xxxx-xxxx-xxxxxxxxxxxx
```

The enroll command POSTs to `https://deepwitai.cn/aiproduct/api/v1/devices/enroll`. On success, the device ID and authentication token are stored in the local SQLite database.

## 4. Configure Environment

Create `/etc/default/iagent-device`:

```ini
# Gateway — must include /aiproduct prefix
IAGENT_GATEWAY_URL="https://deepwitai.cn/aiproduct"

# Agent image (build from agent/ directory, or pull from registry)
IAGENT_AGENT_IMAGE="iagent/agent:latest"

# Pre-provision 4 idle agent containers
IAGENT_POOL_SIZE=4

# Data directory
IAGENT_DEVICE_DATA_DIR="/var/lib/iagent-device"
```

## 5. Build Agent Image

```bash
cd agent

# Production image (~4 GB, ~60 min first build)
docker build -t iagent/agent:latest .
```

## 6. Start Device

```bash
source venv/bin/activate
iagent-device run
```

The device will:
1. Pull the agent image (if not cached)
2. Create `pool_size` idle agent containers
3. Open a WSS tunnel to `wss://deepwitai.cn/aiproduct/tunnel`
4. Send HELLO with agent list, platform info, and resources
5. Appear as **online** in the admin dashboard

## 7. systemd Service (Recommended)

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

## 8. Set Agent Pool Size

Once the device shows as online in the admin dashboard, the pool size can be set:

1. Go to **Device Fleet** → find the device
2. Click the **Layers** icon → set pool size (e.g., 4)
3. Gateway sends `AGENT_CREATE` frames via the tunnel
4. Device creates the Docker containers
5. Agents appear as **idle** when ready

## 9. Verify

```bash
# Device status
iagent-device status

# Gateway reachable
curl -s https://deepwitai.cn/aiproduct/healthz
# {"status":"ok"}

# Running containers
docker ps --filter 'label=iagent.pool=true'

# Agent health (use port from docker ps)
curl -s http://localhost:<port>/healthz
# {"status":"ok","busy":false}
```

## 10. Test Credentials

For testing the deployment, use these accounts (register via the web UI at `/aiproduct/auth/register` or use pre-existing ones):

| Role | Email | Purpose |
|------|-------|---------|
| Admin | `test@deepwit.ai` | Manage devices, skills, visibility |
| Customer | `cust@deepwit.ai` | Submit jobs, upload files |

## 11. End-to-End Smoke Test

1. **Admin**: Register a skill in the vault (Skill Vault page), publish a version, set visibility to public
2. **Admin**: Go to Fleet Rollout → click a skill → click **Install** (rocket icon)
3. **Admin**: Wait for rollout status to show `installed` on the device row
4. **Customer**: Log in → submit a job with a simple command (e.g., "echo hello")
5. **Customer**: Observe job progress → result appears with status `succeeded`
6. **Admin**: Check Device Fleet → the allocated agent should be `busy` during the job, then `idle` after

## Network Notes

- The device only makes **outbound** connections. No firewall ports need to be opened on the device.
- The tunnel uses WSS (WebSocket over TLS) on port 443 — standard HTTPS port.
- If the device is behind a corporate proxy, set `HTTPS_PROXY` and `WSS_PROXY` environment variables.
- Agent containers communicate with the device over localhost HTTP — no external network needed.

## Troubleshooting

### Device not enrolling

```bash
# Check gateway reachable
curl -s https://deepwitai.cn/aiproduct/healthz

# Check enrollment endpoint
curl -s -X POST https://deepwitai.cn/aiproduct/api/v1/devices/enroll \
  -H "Content-Type: application/json" \
  -d '{"enrollment_code":"YOUR_CODE"}'
```

### Device offline in dashboard

- Check the device is running: `iagent-device status`
- Check tunnel: `journalctl -u iagent-device -f | grep tunnel`
- The gateway URL **must** include `/aiproduct`: `https://deepwitai.cn/aiproduct` (not `https://deepwitai.cn`)

### Agent containers not being created

- Ensure the device is **online** in the dashboard before setting pool size
- Check Docker is running: `docker info`
- Check image exists: `docker images iagent/agent`

### Wrong gateway URL

The tunnel client constructs the WebSocket URL from `IAGENT_GATEWAY_URL`:
- `https://deepwitai.cn/aiproduct` → `wss://deepwitai.cn/aiproduct/tunnel`
- `https://deepwitai.cn` → `wss://deepwitai.cn/tunnel` (WRONG — missing prefix)

Always include the `/aiproduct` path prefix.
