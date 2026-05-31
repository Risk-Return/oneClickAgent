# IAgent — Development Specifications

Specs for **IAgent**: a friendly web UI to control remote AI agents running in Docker containers on private local devices, brokered by a public cloud gateway.

Derived from `../braionstorm/goal.md`.

## Decisions (locked)

| Topic | Choice |
|-------|--------|
| Cloud Gateway | **Go** (public edge, tunnel hub, web API) |
| Local Device & Agent | **Python** |
| Frontend | **React + TypeScript + Tailwind + shadcn/ui** |
| Databases | **PostgreSQL** (cloud) + **SQLite** (local device) |
| Tunnel | **Self-built reverse WebSocket** (device dials out, no public IP needed) |
| Agent form | **Generic agent + fixed HTTP API** (LLM/framework swappable, not bound) |
| Roles & ownership | **Admin (operator)** owns the **device fleet + agent pool** + entire **skill lifecycle** + **skill visibility**. **User (customer)** owns **jobs/files**, does *not* own/see devices or the agent pool; agents are **temporarily allocated per job** and released on completion |
| Skills | **Cloud skill vault** (admin). Admin installs/disables/updates/deletes skills across **all** devices and sets **visibility** (`public`/`restricted` + grants to a **user or an organization/group**); customer only **selects** visible+installed skills per agent. A job runs with **at most one** skill |
| Organizations | Customers can be **single or grouped** into orgs; admin grants available skills to a whole org at once |
| Queue | **Tiered FIFO** (enterprise > pro > free), configurable TTL (1h default), per-user cap (10 default), `QUEUE_TIMEOUT` and `QUEUE_FULL` errors |
| Agent image | **Ubuntu 24.04** Docker image bundling default agent **opencode**, headless browser **camoufox**, runtimes (Node/Python/Go/Rust/Java) + warmed dependency caches + **Xvfb/x11vnc**; multi-arch, **pre-pulled** by devices |
| Interactive browser (VNC) | Customer can **live-control** the agent's headless browser via **noVNC**, relayed over a dedicated on-demand binary socket (device dials out, no inbound port); RFB stays loopback-bound in the container |
| Login credential vault | Website logins (cookies + storage-state) captured in a VNC session, **encrypted at rest** (AES-256-GCM, gateway-only key), and **re-injected** into the agent browser per job; never persisted on device/agent, wiped after the job |
| Channels | **Web** built now; Feishu/QQ/others via adapter interface (stubs) |
| Cross-platform | Device/agent toolchain runs on **Windows + macOS** (and Linux); the agent image runs as a Linux container under Docker Desktop |

## Documents

| # | File | Scope |
|---|------|-------|
| 00 | [00-overview.md](./00-overview.md) | Goals, topology, tech stack, glossary, repo layout |
| 01 | [01-architecture.md](./01-architecture.md) | Components, data flow, sequences, state machines, recovery |
| 02 | [02-cloud-gateway.md](./02-cloud-gateway.md) | Go gateway: modules, tunnel hub, API, relay, config |
| 03 | [03-local-device.md](./03-local-device.md) | Python device: tunnel client, Docker mgmt, files, CLI |
| 04 | [04-agent-container.md](./04-agent-container.md) | Generic agent image + HTTP API contract + hygiene |
| 05 | [05-tunnel-protocol.md](./05-tunnel-protocol.md) | Reverse WebSocket framing, message types, files, acks |
| 06 | [06-data-model.md](./06-data-model.md) | PostgreSQL + SQLite schemas, retention, migrations |
| 07 | [07-api.md](./07-api.md) | REST + WS API, channel adapter interface, device↔agent API |
| 08 | [08-auth-security.md](./08-auth-security.md) | Auth, tenant isolation, hardening, threat model |
| 09 | [09-web-ui.md](./09-web-ui.md) | Frontend stack, screens, realtime, UX, testing |
| 10 | [10-deployment.md](./10-deployment.md) | Build, deploy, ops, CI/CD, E2E verification |

## Suggested reading order

`00 → 01 → 05 → 06 → 07 → 02 → 03 → 04 → 08 → 09 → 10`

## Status

Draft v1 — ready for review. Open implementation questions are flagged inline (e.g., multi-instance tunnel registry, large result-artifact pull flow, non-web channel account linking).
