[中文版](README.zh.md)

# IAgent

A friendly web interface for controlling remote AI agents that run inside Docker containers on local devices, brokered through a public cloud gateway.

## How It Works

```
                         public internet                 private network (no public IP)
                              │                                  │
[Browser / Web UI] ─HTTPS/WSS─┤                                  │
                              ▼                                  │
                     ┌─────────────────┐   reverse WSS tunnel    │   ┌──────────────────┐
                     │  Cloud Gateway  │◄────────(device dials out)───┤   Local Device    │
                     │   (Go service)  │                          │   │  (Python service) │
                     │  + PostgreSQL   │                          │   │  + SQLite         │
                     └─────────────────┘                          │   └────────┬─────────┘
                                                                   │            │ Docker API
                                                                   │   ┌────────┴─────────┐
                                                                   │   │  Agent Container  │
                                                                   │   │ (HTTP API, Python)│
                                                                   │   └──────────────────┘
```

A user submits a job via the web UI. The **Cloud Gateway** picks an idle agent from the pool, routes the command over the reverse tunnel to a **Local Device**, which dispatches it to a Dockerized **Agent Container**. Results flow back the same way. Agents are released to the pool when the job completes.

## Key Features

- **Safe Gateway** — public Go service: TLS, auth, tenant isolation. The only internet-facing component.
- **Agent Pool** — agents are pooled resources, temporarily allocated per job, released on completion.
- **Tiered Job Queue** — enterprise > pro > free, with TTL and per-user cap.
- **Reverse Tunnel** — devices dial out over WSS, no inbound ports or public IP needed.
- **Cloud Skill Vault** — admin manages skills centrally; customers select from visible skills.
- **Multi-channel** — web built; Feishu / QQ adapters ready as stubs.
- **Organizations** — group customers; grant skill visibility per org.
- **File Relay** — chunked push over tunnel with SHA-256 integrity.

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Cloud Gateway | **Go 1.22+**, chi, gorilla/websocket, pgx, golang-jwt, Argon2id |
| Local Device | **Python 3.11+**, FastAPI, uvicorn, websockets, docker-py |
| Agent Container | **Python** runtime, fixed HTTP API |
| Frontend | **React 18 + TypeScript + Vite**, Tailwind CSS, shadcn/ui |
| Cloud DB | **PostgreSQL 15+** |
| Local DB | **SQLite** (WAL mode) |
| Auth | **JWT** (access + rotating refresh), **Argon2id** hashing |

## Quick Start

### Prerequisites

- Go 1.22+
- PostgreSQL 15+
- Docker (for agent containers)

### Gateway

```bash
cd gateway
cp .env.example .env  # edit with your settings
go run ./cmd/gateway
```

Required env vars: `IAGENT_DB_URL`, `IAGENT_JWT_SECRET` (min 32 chars).

## Repository Layout

```
IAgent/
├── docs/
│   ├── braionstorm/goal.md          # project vision
│   └── spec/                        # detailed specs
├── gateway/                         # Go cloud gateway
│   ├── cmd/gateway/main.go
│   ├── internal/{api,tunnel,auth,store,relay,pool,model}/
│   └── migrations/
├── device/                          # Python local device service
├── agent/                           # Agent container image + runtime
├── web/                             # React frontend
└── deploy/                          # compose files, env templates
```

## Spec Reading Order

`00-overview` → `01-architecture` → `05-tunnel-protocol` → `06-data-model` → `07-api` → `02-cloud-gateway` → `03-local-device` → `04-agent-container` → `08-auth-security` → `09-web-ui` → `10-deployment`

## License

TBD
