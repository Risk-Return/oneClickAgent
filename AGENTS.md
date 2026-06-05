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

## Docs Structure

| Directory | Content |
|-----------|---------|
| `docs/braionstorm/` | Vision and goals (`goal.md`) |
| `docs/spec/` | Design specifications (00–10, locked decisions in README) |
| `docs/audit/` | Implementation audits against spec |
| `docs/dev/` | Development progress records per module |

Reading order: `goal → 00-overview → 01-architecture → 05-tunnel-protocol → 06-data-model → 07-api → 02-cloud-gateway → 03-local-device → 04-agent-container → 08-auth-security → 09-web-ui → 10-deployment`
