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

- **Two roles**: **admin (operator)** manages devices + the agent pool + the whole skill lifecycle + skill visibility; **user (customer)** owns jobs/files and sends commands. *Agents are temporarily allocated to a customer's job and released when the job completes. Customers do not own or see devices.*
- **Safe gateway**: the only public surface; authenticates users, authorizes actions, isolates tenants.
- **Multi-channel**: web channel is built now; Feishu / QQ / others are left as an adapter interface (see `07-api.md`).
- **Multi-agent + pooled agents**: one Docker container per agent. Agents live in a **pool** managed by the admin. When a customer posts a job, one or more agents are temporarily allocated from the pool; they are released back after the job completes.
- **Admin-managed device fleet**: admins enroll/operate local devices; a device may host agents from multiple customers.
- **Cloud skill vault**: skills are stored centrally on the gateway. **Admins** install/disable/update/delete skills across **all** devices and decide **which skills each customer (or organization/group) can see**; **customers** only select which visible skills their temporarily allocated agents use.
- **Organizations (groups)**: a customer can be single or belong to a group; admins set the available skills for a whole group at once.
- **One skill per job**: a customer selects **at most one** skill to execute a given job.
- **Pre-built agent image (Ubuntu)**: each agent container ships ready-to-run with a default agent (`opencode`), a headless stealth browser (`camoufox`), language runtimes (Node/Python/Go/Rust/Java), and warmed dependency caches. Devices **pre-pull** the image.
- **Interactive browser control (VNC)**: a customer can take **live control** of the agent's headless browser from the web UI (relayed over the tunnel — no inbound device port) — e.g. to log into a site by hand.
- **Encrypted login vault**: logins captured during a VNC session are stored **encrypted** in the gateway and re-injected into the agent's browser on later jobs, so it starts already signed in; wiped from the agent after each job.
- **Web UI**: command interface + file upload + skill selection + saved-login selection + job control (send/cancel/status) + live browser (VNC) + result display, plus an **admin console** for devices, agent pool, skill vault, fleet rollout, and visibility. **No terminal access, no raw agent internals — progress-level information only.**
- **User registration & authentication.**

## 4. Technology Stack

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| Cloud Gateway | **Go** (1.22+), `net/http`, `gorilla/websocket`, `chi` router | High-concurrency, simple static binary, great fit for the reverse tunnel hub. |
| Local Device | **Python** (3.11+), `FastAPI`, `uvicorn`, `websockets`, `docker` (docker-py) | Native Docker control + AI ecosystem. |
| Agent Container | **Ubuntu 24.04** image, **Python** supervisor exposing a fixed HTTP API; bundles `opencode` + `camoufox` (headless browser) + Node/Python/Go/Rust/Java + Xvfb/x11vnc; LLM/framework not bound by spec | Generic, swappable agent that is ready-to-run and supports interactive VNC. |
| Frontend | **React 18 + TypeScript + Vite**, **Tailwind CSS**, **shadcn/ui**, **Lucide** icons | Modern, accessible, fast. |
| Cloud DB | **PostgreSQL 15+** | Central source of truth for users, devices, agents, jobs, files. |
| Local DB | **SQLite** (WAL mode) | Lightweight per-device state; no server to run. |
| Transport | **JSON over WebSocket (WSS)** for tunnel; **REST/HTTPS** for web↔gateway and device↔agent | Simple, debuggable, controllable. |
| Auth | **JWT** (access + refresh), **Argon2id** password hashing | Stateless API auth + strong password storage. |

> **Cross-platform requirement:** all device/agent code and tooling must run on both **Windows and macOS**. Use `pathlib`, avoid shell-specific assumptions, and document Docker Desktop requirements.

## 5. Component Responsibilities (summary)

| Component | Owns | Does NOT do |
|-----------|------|-------------|
| **Cloud Gateway** | User auth + roles, web API, tunnel hub, central DB, agent pool + allocation, job routing, file relay/staging, skill vault + fleet dispatch + visibility, **VNC session relay** (pairs browser↔device sockets), **encrypted credential vault** (saved logins, inject per job) | Run agents, hold long-term user files; parse/store RFB bytes; hold cookie plaintext at rest |
| **Local Device** | Tunnel client, Docker lifecycle (pool of agents), agent health/recovery, local job state, file staging & cleanup, **agent-image pre-pull**, **VNC bridge** (TCP↔WS to the container's loopback RFB), **credential pass-through** (stream cookies G↔agent); admin-operated, hosts the agent pool | Authenticate end users, expose public ports/RFB, decide skill policy, persist cookies |
| **Agent Container** | Execute one job at a time, report progress/result, install/enable/disable skills; run the bundled headless browser + on-demand VNC; accept injected login cookies; released back to pool after job done | Persist user data/credentials after job completion, belong to any customer permanently, expose VNC to the internet |
| **Web UI** | Customer: command UX, uploads, skill + saved-login selection, job control, live browser (VNC), results. Admin: device fleet + agent pool + skill vault + fleet rollout + visibility | Show terminals or raw agent logs |

## 6. Repository Layout (target)

```
IAgent/
├── docs/
│   ├── braionstorm/goal.md
│   └── spec/                 # ← these documents
├── gateway/                  # Go cloud gateway
│   ├── cmd/gateway/
│   ├── internal/{api,tunnel,auth,store,relay,pool,model}/
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
| **Device** | Local Python service that manages an agent pool on a private machine. |
| **Agent** | A single AI worker = one Docker container with a fixed HTTP API; lives in a pool, temporarily allocated to a job. |
| **Job** | A unit of work submitted by the user; the system allocates one or more agents from the pool to execute it, then releases them on completion. If no agent is available, the job queues in a tiered FIFO order. |
| **Admin / Operator** | Privileged user who manages the device fleet and the entire skill lifecycle + visibility. |
| **User / Customer** | End user who owns jobs/files, sends commands, and selects visible skills. Agents are temporarily allocated per job and released after. Has a `tier` (free/pro/enterprise) affecting queue priority. Does not own devices. |
| **Organization / Group** | A group of customers; admins can grant skill visibility to a whole org at once. A user belongs to at most one. |
| **Skill** | A reusable capability/config stored in the cloud vault and installed into agents. |
| **Skill Vault** | Central admin-owned catalog + versioned skill artifacts on the gateway, dispatched to devices. |
| **Skill visibility** | Admin setting (`public`/`restricted` + grants) deciding which customers can see a skill. |
| **Agent image** | Ubuntu-based Docker image bundling `opencode`, `camoufox`, language runtimes + warmed deps, and the Xvfb/x11vnc stack; pre-pulled by devices. |
| **opencode** | The default bundled agent/brain (`opencode-ai`). |
| **Camoufox** | The bundled headless stealth browser the agent drives and that VNC renders. |
| **VNC session** | A live, relayed view/control of an agent's headless browser from the web UI (noVNC ↔ gateway ↔ device ↔ container RFB). |
| **Credential vault** | Encrypted store of a customer's saved website logins (storage-state), injected into the agent browser per job. |
| **Tunnel** | Persistent reverse WebSocket from device → gateway (control). A separate on-demand binary socket carries interactive VNC. |
| **Channel** | An input/output surface (web now; Feishu/QQ later). |

## 8. Reading Order

`00-overview` → `01-architecture` → `05-tunnel-protocol` → `06-data-model` → `07-api` → `02-cloud-gateway` → `03-local-device` → `04-agent-container` → `08-auth-security` → `09-web-ui` → `10-deployment`.
