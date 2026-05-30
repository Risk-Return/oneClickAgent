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
| Roles & ownership | **Admin (operator)** owns the **device fleet** + entire **skill lifecycle** + **skill visibility**. **User (customer)** owns **agents/jobs/files**, does *not* own/see devices; platform schedules agent placement |
| Skills | **Cloud skill vault** (admin). Admin installs/disables/updates/deletes skills across **all** devices and sets **visibility** (`public`/`restricted` + grants to a **user or an organization/group**); customer only **selects** visible+installed skills per agent. A job runs with **at most one** skill |
| Organizations | Customers can be **single or grouped** into orgs; admin grants available skills to a whole org at once |
| Auth | **JWT** (access + refresh) + **Argon2id** password hashing |
| Channels | **Web** built now; Feishu/QQ/others via adapter interface (stubs) |
| Cross-platform | Device/agent toolchain runs on **Windows + macOS** (and Linux) |

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
