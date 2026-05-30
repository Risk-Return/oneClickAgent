# 00 — Project Overview

> **IAgent** — a friendly web interface for controlling remote AI agents that run inside Docker containers on local devices, brokered through a public cloud gateway.

## 1. Purpose

IAgent lets a user drive an AI agent that lives on a private machine (no public IP) entirely from a browser. The user sends jobs, uploads files, manages skills, and watches progress — without ever touching a terminal or seeing low-level agent internals.

A public **Cloud Gateway** acts as the only internet-facing component. Each **Local Device** dials *out* to the gateway over a persistent reverse tunnel, so the device needs no inbound ports and no public IP. The device runs one Docker container per **Agent**.

## 2. High-Level Topology

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

Canonical request flow (web channel):

```
[User Input] → [Cloud Gateway] → [Tunnel] → [Local Device] → [Agent Container]
            → [Result] → [Local Device] → [Tunnel] → [Cloud Gateway] → [User Output]
```

## 3. Core Features (from goal)

- **Two roles**: **admin (operator)** manages devices + the whole skill lifecycle + skill visibility; **user (customer)** owns agents/jobs/files, sends commands, and selects from visible skills. *Customers do not own or see devices.*
- **Safe gateway**: the only public surface; authenticates users, authorizes actions, isolates tenants.
- **Multi-channel**: web channel is built now; Feishu / QQ / others are left as an adapter interface (see `07-api.md`).
- **Multi-agent**: one Docker container per agent; a customer registers agents (default 1, can scale up); the platform places each on an admin-managed device.
- **Admin-managed device fleet**: admins enroll/operate local devices; a device may host agents from multiple customers.
- **Cloud skill vault**: skills are stored centrally on the gateway. **Admins** install/disable/update/delete skills across **all** devices and decide **which skills each customer (or organization/group) can see**; **customers** only select which visible skills their agent uses.
- **Organizations (groups)**: a customer can be single or belong to a group; admins set the available skills for a whole group at once.
- **One skill per job**: a customer selects **at most one** skill to execute a given job.
- **Web UI**: command interface + file upload + skill selection + job control (send/cancel/status) + result display, plus an **admin console** for devices, skill vault, fleet rollout, and visibility. **No terminal access, no raw agent internals — progress-level information only.**
- **User registration & authentication.**

## 4. Technology Stack

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| Cloud Gateway | **Go** (1.22+), `net/http`, `gorilla/websocket`, `chi` router | High-concurrency, simple static binary, great fit for the reverse tunnel hub. |
| Local Device | **Python** (3.11+), `FastAPI`, `uvicorn`, `websockets`, `docker` (docker-py) | Native Docker control + AI ecosystem. |
| Agent Container | **Python** runtime exposing a fixed HTTP API; LLM/framework not bound by spec | Generic, swappable agent implementation. |
| Frontend | **React 18 + TypeScript + Vite**, **Tailwind CSS**, **shadcn/ui**, **Lucide** icons | Modern, accessible, fast. |
| Cloud DB | **PostgreSQL 15+** | Central source of truth for users, devices, agents, jobs, files. |
| Local DB | **SQLite** (WAL mode) | Lightweight per-device state; no server to run. |
| Transport | **JSON over WebSocket (WSS)** for tunnel; **REST/HTTPS** for web↔gateway and device↔agent | Simple, debuggable, controllable. |
| Auth | **JWT** (access + refresh), **Argon2id** password hashing | Stateless API auth + strong password storage. |

> **Cross-platform requirement:** all device/agent code and tooling must run on both **Windows and macOS**. Use `pathlib`, avoid shell-specific assumptions, and document Docker Desktop requirements.

## 5. Component Responsibilities (summary)

| Component | Owns | Does NOT do |
|-----------|------|-------------|
| **Cloud Gateway** | User auth + roles, web API, tunnel hub, central DB, agent scheduling, job routing, file relay/staging, skill vault + fleet dispatch + visibility | Run agents, hold long-term user files after delivery |
| **Local Device** | Tunnel client, Docker lifecycle, agent health/recovery, local job state, file staging & cleanup; admin-operated, hosts multiple customers' agents | Authenticate end users, expose public ports, decide skill policy |
| **Agent Container** | Execute one job at a time, report progress/result, install/enable/disable skills it is told to | Persist user data after job completion |
| **Web UI** | Customer: command UX, uploads, skill selection, job control, results. Admin: device fleet + skill vault + fleet rollout + visibility | Show terminals or raw agent logs |

## 6. Repository Layout (target)

```
IAgent/
├── docs/
│   ├── braionstorm/goal.md
│   └── spec/                 # ← these documents
├── gateway/                  # Go cloud gateway
│   ├── cmd/gateway/
│   ├── internal/{api,tunnel,auth,store,relay,model}/
│   └── migrations/
├── device/                   # Python local device service
│   ├── iagent_device/{tunnel,docker,jobs,store,files}/
│   └── pyproject.toml
├── agent/                    # Agent container image + runtime
│   ├── iagent_agent/{api,runtime,skills}/
│   └── Dockerfile
├── web/                      # React frontend
│   └── src/{pages,components,api,store}/
└── deploy/                   # compose files, env templates, scripts
```

## 7. Glossary

| Term | Meaning |
|------|---------|
| **Gateway** | Public Go service; the safe edge. |
| **Device** | Local Python service that manages agents on a private machine. |
| **Agent** | A single AI worker = one Docker container with a fixed HTTP API. |
| **Job** | A unit of work submitted by the user and executed by one agent. |
| **Admin / Operator** | Privileged user who manages the device fleet and the entire skill lifecycle + visibility. |
| **User / Customer** | End user who owns agents/jobs/files, sends commands, and selects visible skills. Does not own devices. |
| **Organization / Group** | A group of customers; admins can grant skill visibility to a whole org at once. A user belongs to at most one. |
| **Skill** | A reusable capability/config stored in the cloud vault and installed into agents. |
| **Skill Vault** | Central admin-owned catalog + versioned skill artifacts on the gateway, dispatched to devices. |
| **Skill visibility** | Admin setting (`public`/`restricted` + grants) deciding which customers can see a skill. |
| **Tunnel** | Persistent reverse WebSocket from device → gateway. |
| **Channel** | An input/output surface (web now; Feishu/QQ later). |

## 8. Reading Order

`00-overview` → `01-architecture` → `05-tunnel-protocol` → `06-data-model` → `07-api` → `02-cloud-gateway` → `03-local-device` → `04-agent-container` → `08-auth-security` → `09-web-ui` → `10-deployment`.
