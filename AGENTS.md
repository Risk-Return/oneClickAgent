# AGENTS.md — IAgent Development Guide

## Project Overview

**IAgent** (oneClickAgent) — a friendly web UI for controlling remote AI agents running in Docker containers on private local devices, brokered through a public cloud gateway.

### Architecture

```
Browser (React + TS + Vite + Tailwind + shadcn/ui)
    │ HTTPS/WSS
    ▼
Cloud Gateway (Go 1.25 + chi + gorilla/websocket + PostgreSQL 15)
    │ Reverse WSS tunnel (device dials out)
    ▼
Local Device (Python 3.11 + websockets + docker-py + SQLite WAL)
    │ Docker API
    ▼
Agent Container (Ubuntu 24.04, Python, HTTP API, opencode + camoufox + VNC)
```

### Repository Layout

| Directory | Technology | Purpose |
|-----------|-----------|---------|
| `gateway/` | Go 1.25 | Cloud gateway — public edge, tunnel hub, web API |
| `device/` | Python 3.11+ | Local device — Docker mgmt, reverse tunnel client, job dispatch |
| `agent/` | Python 3.11+, Docker | Agent container image + runtime |
| `web/` | React 18 + TypeScript + Vite | Web frontend (customer + admin UI) |
| `deploy/` | Docker Compose, systemd | Deployment configs and env templates |
| `docs/` | Markdown | Specifications, audits, dev records |

---

## Commands

### Gateway (Go)

```bash
cd gateway && go build -o bin/gateway ./cmd/gateway   # Build
cd gateway && go test ./...                            # Run all tests
cd gateway && go vet ./...                             # Lint/vet
```

### Device (Python)

```bash
cd device && pip install -e .                          # Install (editable)
cd device && pip install -e ".[dev]"                   # Install with dev deps
cd device && ruff check . && mypy .                    # Lint + typecheck
cd device && python -m pytest                          # Run tests
```

### Agent (Python)

```bash
cd agent && pip install -e ".[dev]"                    # Install with dev deps
cd agent && ruff check . && mypy .                     # Lint + typecheck
cd agent && python -m pytest                           # Run tests
cd agent && docker build -t iagent/agent:dev .         # Build container image
```

### Web (React/TypeScript)

```bash
cd web && npm install                                  # Install deps
cd web && npm run lint                                 # ESLint
cd web && npm run typecheck                            # tsc --noEmit
cd web && npm test                                     # vitest run
cd web && npm run dev                                  # Vite dev server
```

### Top-level

```bash
make gateway    # Build Go gateway
make device     # Install device Py package
make agent      # Build agent Docker image
make web        # npm install + build frontend
make dev-up     # Start cloud dev stack (PostgreSQL + Gateway via docker compose)
```

---

## Workflow: Audit

When auditing a module's implementation against its spec, follow these steps:

1. **Understand the big picture.** Read these three files first (in parallel):
   - `docs/braionstorm/goal.md` — project vision, key features, requirements
   - `docs/spec/00-overview.md` — goals, topology, tech stack, glossary
   - `docs/spec/01-architecture.md` — components, data flow, state machines, recovery

2. **Read the module's dev spec.** Find the relevant spec file under `docs/spec/`:
   - `02-cloud-gateway.md` — Go gateway spec
   - `03-local-device.md` — Python device spec
   - `04-agent-container.md` — Agent container spec
   - `05-tunnel-protocol.md` — Tunnel protocol spec
   - `06-data-model.md` — DB schemas
   - `07-api.md` — REST/WS API spec
   - `08-auth-security.md` — Auth & security spec
   - `09-web-ui.md` — Frontend spec

3. **Read the dev record** for that module under `docs/dev/` (if it exists).

4. **Verify against source code.** Read the actual implementation files in the relevant directory (`gateway/`, `device/`, `agent/`, `web/`). Do not trust dev records at face value.

5. **Categorize gaps** — Critical (breaks core flow), Significant (partially incomplete), Minor (suboptimal). Every gap must cite a source file + line number and the exact spec section.

6. **Write audit** to `docs/audit/<module>.md`. Follow the format in existing audits (`docs/audit/03-local-device.md` as the most recent example).

See also: `.opencode/agents/auditor.md` for the full auditor subagent workflow.

---

## Workflow: Testing

All tests and simulations must be conducted at production level. Do not simulate paths that would not be deployed in production.

### Unit / module-level tests

- Use mock data injected into the execution chain. Set up logging **before** execution, observe logs to diagnose issues.
- For Go: use the standard `testing` package. Tests live alongside source files as `*_test.go`.
- For Python: use `pytest` + `pytest-asyncio`. Dev dependencies include `ruff` and `mypy`.
- For frontend: use Vitest + `@testing-library/react`.

### Cross-module / integration tests

- Build an execution chain that **exactly follows production code paths** (no shortcuts).
- Instrument key steps with logging. Observe logs at each step to trace the full flow and diagnose issues.
- Example for device↔gateway: launch the gateway, start the device tunnel client, submit a job through the API, and trace frames through the tunnel codec → hub → dispatcher chain.

### Pre-commit checks

Before considering any work complete, run the project's lint and typecheck commands for the affected module(s):
- Go: `go vet ./...`
- Python: `ruff check . && mypy .`
- Frontend: `npm run lint && npm run typecheck`

### Database separation (production vs test)

| Purpose | Database | Connection |
|---------|----------|------------|
| **Production / live deployment** | `iagent` | `postgresql://iagent:...@localhost:5432/iagent?sslmode=disable` |
| **E2E tests** | `iagent_e2e` | `postgresql://iagent:...@localhost:5432/iagent_e2e?sslmode=disable` |

- E2E tests run `truncateAll` which **deletes all rows** in every table.
- **Never** run e2e tests against the `iagent` production database.
- The `ONE_CLICK_DSN` env var overrides the e2e database URL. Always point it at `iagent_e2e` (or a throwaway DB), never at `iagent`.
- The `iagent` user has SUPERUSER on `iagent_e2e` (needed for `CREATE EXTENSION citext` during migrations).

```bash
# Run e2e tests safely
ONE_CLICK_DSN="postgresql://iagent:...@localhost:5432/iagent_e2e?sslmode=disable" \
  go test -v -count=1 ./gateway/e2e/

# Or use the default (already points to iagent_e2e)
go test -v -count=1 ./gateway/e2e/
```

---

## Key Design Principles

- **Gateway is the only public surface.** All traffic goes through it. Devices have no inbound ports.
- **Agent pool model:** Agents are pooled resources, temporarily allocated per job, released on completion. Customers do not own agents or devices.
- **Admin vs Customer:** Admins manage the device fleet, agent pool, skill vault, and visibility. Customers own jobs/files and select visible skills.
- **At most one skill per job.**
- **Reverse WebSocket tunnel** for control traffic (JSON frames). Separate binary socket for VNC RFB relay.
- **Idempotent frames:** Every tunnel frame has a `msg_id`; receivers ACK; handlers are idempotent by `(job_id, event_seq)`.
- **Cross-platform:** Device/agent code runs on Windows, macOS, and Linux. Use `pathlib`, avoid shell-specific assumptions.
- **PostgreSQL** is cloud source of truth. **SQLite** (WAL mode) is device-local state.

---

## File Naming & Code Conventions

- **Go:** Package-per-concern under `gateway/internal/`. File names lowercase, underscores for multi-word (e.g., `file_relay.go`). Tests in `*_test.go`.
- **Python:** Package under `device/iagent_device/` and `agent/iagent_agent/`. Modules lowercase with underscores. Use `pydantic` models for data structures.
- **TypeScript/React:** Components in `web/src/components/`, pages in `web/src/pages/`, hooks in `web/src/features/`. Use path alias `@/` → `./src/`.
- **No comments unless necessary.** Code should be self-documenting.
- **No emojis in code or documentation unless explicitly requested.**

---

## Cloud-Side Build & Restart (Production Server)

The production cloud server runs nginx + gateway natively on this machine (`deepwitai.cn`). The Vite dev server is not used in production — the built web dist is served by nginx and the gateway.

### Gateway Build & Restart

```bash
# Build
cd gateway && go build -o bin/gateway ./cmd/gateway && go vet ./...

# Restart (kill existing, start new with production env)
fuser -k 42080/tcp; sleep 1
nohup env \
  IAGENT_CORS_ORIGINS='*' \
  IAGENT_CRED_KEY='AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=' \
  IAGENT_DB_URL='postgres://iagent:iagent_dev_password@localhost:5432/iagent?sslmode=disable' \
  IAGENT_ENV='development' \
  IAGENT_FILE_STORE='local:/tmp/iagent-files' \
  IAGENT_HTTP_ADDR=':42080' \
  IAGENT_JWT_SECRET='dev-jwt-secret-at-least-32-characters-long!!' \
  IAGENT_LOG_FORMAT='text' \
  IAGENT_LOG_LEVEL='debug' \
  IAGENT_WEB_DIST_DIR='/root/projects/oneClickAgent/web/dist' \
  /root/projects/oneClickAgent/gateway/bin/gateway \
  > /tmp/gateway.log 2>&1 &

# Verify
curl -s -o /dev/null -w "%{http_code}" http://localhost:42080/healthz   # expect 200
```

### Web UI Build & Deploy

```bash
# Install deps (if new packages added)
cd web && npm install

# Build for production (served under /aiproduct/ subpath by nginx)
cd web && VITE_BASE=/aiproduct/ VITE_API_PREFIX=/aiproduct npx vite build

# Reload nginx to pick up new dist assets
nginx -s reload

# Verify assets are reachable
dist_js=$(grep -oP '/aiproduct/assets/index-[^.]+\.js' web/dist/index.html)
curl -s -o /dev/null -w "%{http_code}" "https://deepwitai.cn${dist_js}"   # expect 200
```

### Quick Full Rebuild

```bash
cd /root/projects/oneClickAgent
# Gateway
cd gateway && go build -o bin/gateway ./cmd/gateway && go vet ./...
# Web
cd ../web && VITE_BASE=/aiproduct/ VITE_API_PREFIX=/aiproduct npx vite build
# Restart gateway
fuser -k 42080/tcp; sleep 1
nohup env IAGENT_CORS_ORIGINS='*' IAGENT_CRED_KEY='AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=' IAGENT_DB_URL='postgres://iagent:iagent_dev_password@localhost:5432/iagent?sslmode=disable' IAGENT_ENV='development' IAGENT_FILE_STORE='local:/tmp/iagent-files' IAGENT_HTTP_ADDR=':42080' IAGENT_JWT_SECRET='dev-jwt-secret-at-least-32-characters-long!!' IAGENT_LOG_FORMAT='text' IAGENT_LOG_LEVEL='debug' IAGENT_WEB_DIST_DIR='/root/projects/oneClickAgent/web/dist' gateway/bin/gateway > /tmp/gateway.log 2>&1 &
nginx -s reload
```

---

## Cloud-Side Diagnostics

### Database

| Item | Value |
|------|-------|
| Host | `localhost:5432` |
| Production DB | `iagent` |
| E2E test DB | `iagent_e2e` |
| User | `iagent` |
| Password | `iagent_dev_password` |
| Encoding | `UTF8` (must be UTF8, not SQL_ASCII — jsonb columns reject non-ASCII) |

**Connect:**
```bash
PGPASSWORD=iagent_dev_password psql -h localhost -U iagent -d iagent
```

### Database Tables

```bash
# List all tables
PGPASSWORD=iagent_dev_password psql -h localhost -U iagent -d iagent -c "\dt"
```

Core tables: `users`, `devices`, `agents`, `jobs`, `files`, `job_files`, `skills`, `skill_versions`, `device_skills`, `agent_skills`, `skill_grants`, `browser_credentials`, `job_credentials`, `vnc_sessions`, `refresh_tokens`, `audit_log`, `organizations`

### Common Diagnostic Queries

```bash
# Active and recent jobs
PGPASSWORD=iagent_dev_password psql -h localhost -U iagent -d iagent -c "
SELECT j.id, j.status, j.agent_id, j.device_id, j.error_code,
       j.created_at, j.started_at, j.finished_at
FROM jobs j ORDER BY j.created_at DESC LIMIT 10;"

# Agent pool status
PGPASSWORD=iagent_dev_password psql -h localhost -U iagent -d iagent -c "
SELECT id, device_id, user_id, name, status, job_id, created_at
FROM agents ORDER BY created_at DESC;"

# Device online status
PGPASSWORD=iagent_dev_password psql -h localhost -U iagent -d iagent -c "
SELECT id, name, status FROM devices;"

# Users
PGPASSWORD=iagent_dev_password psql -h localhost -U iagent -d iagent -c "
SELECT id, email, role, tier FROM users;"

# Job output files
PGPASSWORD=iagent_dev_password psql -h localhost -U iagent -d iagent -c "
SELECT f.id, f.name, f.size, jf.role, jf.job_id
FROM files f JOIN job_files jf ON f.id = jf.file_id
WHERE jf.role = 'output' ORDER BY f.created_at DESC LIMIT 10;"
```

### Log Files

| Log | Path | Content |
|-----|------|---------|
| Gateway stdout/stderr | `/tmp/gateway.log` | Startup, HTTP requests, tunnel events, errors |
| Nginx access log | `/var/log/nginx/access.log` | All HTTP requests through nginx |
| Nginx error log | `/var/log/nginx/error.log` | Nginx errors |

Gateway log format: structured JSON lines with `time`, `level`, `source`, `msg`, and context fields.

**Common log queries:**
```bash
# Device connection events
grep "device registered\|device unregistered\|read error\|abnormal" /tmp/gateway.log

# Job completion (agent release)
grep "agent released to pool" /tmp/gateway.log

# AGENT_CREATE delivery failures
grep "AGENT_CREATE not delivered" /tmp/gateway.log

# Frame retransmissions (unacked frames)
grep "retransmit" /tmp/gateway.log

# Job submissions
grep "POST.*/jobs " /tmp/gateway.log

# HTTP errors
grep "status=4[0-9][0-9]\|status=5[0-9][0-9]" /tmp/gateway.log

# Specific job activity (by job ID)
grep "019ead2b" /tmp/gateway.log
```

### Nginx Configuration

| File | Purpose |
|------|---------|
| `/etc/nginx/conf.d/aibi.conf` | Main nginx config (all routes) |
| `/etc/nginx/nginx.conf` | Global nginx config |

**IAgent routes under `/aiproduct/`:**

| Route | Target |
|-------|--------|
| `/aiproduct/` | SPA static files from `web/dist/` |
| `/aiproduct/assets/` | Long-cached JS/CSS from `web/dist/assets/` |
| `/aiproduct/api/` | Proxied to `http://127.0.0.1:42080/api/` |
| `/aiproduct/ws` | Proxied to `http://127.0.0.1:42080/ws` (WebSocket) |
| `/aiproduct/tunnel` | Proxied to `http://127.0.0.1:42080/tunnel` (device WSS) |

Reload after changes: `nginx -s reload`

### Migrations

Apply manually from `gateway/migrations/`:

```bash
for f in gateway/migrations/*.up.sql; do
  PGPASSWORD=iagent_dev_password psql -h localhost -U iagent -d iagent -f "$f"
done
```

### Key Directories

| Path | Purpose |
|------|---------|
| `gateway/bin/gateway` | Gateway binary |
| `web/dist/` | Built web frontend (served by nginx) |
| `gateway/migrations/` | SQL migration files |
| `docs/deployment/issues/` | Issue investigation reports |
| `/tmp/iagent-files/` | Staged file storage (`IAGENT_FILE_STORE`) |

---

## Docs Structure

| Directory | Content |
|-----------|---------|
| `docs/braionstorm/` | Vision and goals (`goal.md`) |
| `docs/spec/` | Design specifications (00–10, locked decisions in README) |
| `docs/audit/` | Implementation audits against spec |
| `docs/dev/` | Development progress records per module |

Reading order: `goal → 00-overview → 01-architecture → 05-tunnel-protocol → 06-data-model → 07-api → 02-cloud-gateway → 03-local-device → 04-agent-container → 08-auth-security → 09-web-ui → 10-deployment`
