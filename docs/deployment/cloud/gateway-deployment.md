# Cloud Gateway Deployment

Deploy the IAgent cloud gateway, PostgreSQL, and web UI on a Linux server with nginx reverse proxy.

## Prerequisites

| Tool | Min Version | Check |
|------|-------------|-------|
| Go | 1.25 | `go version` |
| PostgreSQL | 15 | `psql --version` |
| nginx | any | `nginx -v` |
| Node.js | 18+ | `node --version` |
| git | any | `git --version` |

## 1. Clone & Build

```bash
git clone https://github.com/Risk-Return/oneClickAgent.git
cd oneClickAgent
```

### Build gateway

```bash
cd gateway
go build -o bin/gateway ./cmd/gateway
```

### Build web UI

The web UI is served as static files by nginx. The build-time env vars `VITE_API_PREFIX` and `VITE_BASE` set the URL prefix under which the app lives.

```bash
cd web
npm install
VITE_API_PREFIX=/aiproduct VITE_BASE=/aiproduct/ npx vite build
# output: web/dist/
```

## 2. Database

```bash
sudo -u postgres psql <<SQL
CREATE DATABASE iagent;
CREATE USER iagent WITH PASSWORD 'generate-a-strong-password';
GRANT ALL PRIVILEGES ON DATABASE iagent TO iagent;
\c iagent
GRANT ALL ON SCHEMA public TO iagent;
SQL
```

Run migrations:

```bash
for f in gateway/migrations/*.up.sql; do
  psql "postgresql://iagent:password@localhost:5432/iagent?sslmode=disable" -f "$f"
done
```

### Test database (for e2e tests)

```bash
sudo -u postgres psql -c "CREATE DATABASE iagent_e2e"
sudo -u postgres psql -d iagent_e2e -c "GRANT ALL PRIVILEGES ON DATABASE iagent_e2e TO iagent"
sudo -u postgres psql -d iagent_e2e -c "ALTER USER iagent WITH SUPERUSER"
psql "postgresql://iagent:password@localhost:5432/iagent_e2e?sslmode=disable" \
  -c "CREATE EXTENSION IF NOT EXISTS citext"
```

## 3. Generate Secrets

```bash
# JWT secret (32+ chars)
openssl rand -base64 32

# Credential vault key (32 bytes, base64)
openssl rand -base64 32
```

## 4. Start Gateway

Create a startup script (`/opt/iagent/run-gateway.sh`):

```bash
#!/bin/bash
export IAGENT_DB_URL="postgresql://iagent:password@localhost:5432/iagent?sslmode=disable"
export IAGENT_JWT_SECRET="your-jwt-secret-at-least-32-chars"
export IAGENT_FILE_STORE="local:/data/iagent/files"
export IAGENT_HTTP_ADDR=":42080"
export IAGENT_ENV="production"
export IAGENT_LOG_LEVEL="info"
export IAGENT_LOG_FORMAT="json"
export IAGENT_CORS_ORIGINS="https://your-domain.com"
export IAGENT_CRED_KEY="your-base64-32-byte-key"
export IAGENT_QUEUE_TTL="1h"
export IAGENT_MAX_QUEUED_PER_USER="10"

exec /opt/iagent/gateway/bin/gateway
```

```bash
chmod +x /opt/iagent/run-gateway.sh
# Run in background
setsid /opt/iagent/run-gateway.sh > /var/log/iagent-gateway.log 2>&1 &

# Verify
curl -s http://localhost:42080/healthz
# {"status":"ok"}
```

### systemd unit (recommended)

```ini
# /etc/systemd/system/iagent-gateway.service
[Unit]
Description=IAgent Cloud Gateway
After=network.target postgresql.service

[Service]
Type=simple
EnvironmentFile=/opt/iagent/gateway.env
ExecStart=/opt/iagent/gateway/bin/gateway
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

## 5. nginx Reverse Proxy

The gateway handles WebSocket upgrades and long-lived connections. nginx must be configured accordingly.

```nginx
# SSL certificate
ssl_certificate     /etc/letsencrypt/live/your-domain.com/fullchain.pem;
ssl_certificate_key /etc/letsencrypt/live/your-domain.com/privkey.pem;

# Redirect /aiproduct → /aiproduct/
location = /aiproduct { return 301 /aiproduct/; }

# SPA static assets (cached)
location /aiproduct/assets/ {
    alias /opt/iagent/web/dist/assets/;
    expires 1y;
    add_header Cache-Control "public, immutable";
}

# Health & metrics (before SPA catch-all)
location = /aiproduct/healthz  { proxy_pass http://127.0.0.1:42080/healthz; }
location = /aiproduct/readyz   { proxy_pass http://127.0.0.1:42080/readyz; }
location = /aiproduct/metrics  { proxy_pass http://127.0.0.1:42080/metrics; }

# SPA entry point
location /aiproduct/ {
    alias /opt/iagent/web/dist/;
    try_files $uri /aiproduct/index.html;
}

# REST API
location /aiproduct/api/ {
    proxy_pass http://127.0.0.1:42080/api/;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_read_timeout 300s;
    proxy_send_timeout 300s;
}

# WebSocket — realtime events
location /aiproduct/ws {
    proxy_pass http://127.0.0.1:42080/ws;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 86400s;
}

# WebSocket — device tunnel
location /aiproduct/tunnel {
    proxy_pass http://127.0.0.1:42080/tunnel;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 86400s;
}

# WebSocket — VNC session relay
location /aiproduct/session/ {
    proxy_pass http://127.0.0.1:42080/session/;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 86400s;
}

# WebSocket — VNC binary relay
location /aiproduct/ws/vnc/ {
    proxy_pass http://127.0.0.1:42080/ws/vnc/;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 86400s;
}
```

**Important ordering rules:**
- Exact-match locations (`= /aiproduct/healthz` etc.) come first
- Prefix locations with WebSocket upgrades (`/aiproduct/ws`, `/aiproduct/tunnel`) come before the SPA catch-all
- The SPA catch-all `location /aiproduct/` must be last among the `/aiproduct/` locations

For HTTP→HTTPS redirect on the same domain, add these to the port 80 server block:

```nginx
server {
    listen 80;
    server_name your-domain.com;
    location = /aiproduct { return 301 https://$host/aiproduct/; }
    location /aiproduct/ { return 301 https://$host$request_uri; }
}
```

Reload nginx:

```bash
nginx -t && systemctl reload nginx
```

## 6. Verify

```bash
# Health
curl -s https://your-domain.com/aiproduct/healthz

# Web UI
curl -s -o /dev/null -w "%{http_code}" https://your-domain.com/aiproduct/

# Register first admin
curl -X POST https://your-domain.com/aiproduct/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","username":"admin","password":"YourStrongPass1!"}'
```

## 7. Upgrading

```bash
# Pull latest code
cd oneClickAgent && git pull

# Rebuild gateway
cd gateway && go build -o bin/gateway ./cmd/gateway

# Rebuild web UI
cd web && VITE_API_PREFIX=/aiproduct VITE_BASE=/aiproduct/ npx vite build

# Restart gateway
systemctl restart iagent-gateway

# Reload nginx (only if config changed)
systemctl reload nginx
```

## Environment Variables Reference

| Var | Required | Default | Purpose |
|-----|----------|---------|---------|
| `IAGENT_DB_URL` | yes | — | PostgreSQL connection string |
| `IAGENT_JWT_SECRET` | yes | — | JWT signing key (32+ chars) |
| `IAGENT_FILE_STORE` | yes | — | File storage path (`local:/path`) |
| `IAGENT_HTTP_ADDR` | no | `:8080` | Gateway listen address |
| `IAGENT_CRED_KEY` | no | — | Credential vault encryption key (32 bytes, base64) |
| `IAGENT_ENV` | no | `development` | `development` or `production` |
| `IAGENT_LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |
| `IAGENT_LOG_FORMAT` | no | `json` | `json` or `text` |
| `IAGENT_CORS_ORIGINS` | no | — | Comma-separated allowed origins |
| `IAGENT_QUEUE_TTL` | no | `1h` | Max time a job can wait in queue |
| `IAGENT_MAX_QUEUED_PER_USER` | no | `10` | Max queued jobs per user |
