# 09 — Web UI

Default channel. A friendly, modern interface for controlling agents. **No terminal access, no raw agent internals — progress-level information only.**

## 1. Stack

- **React 18 + TypeScript + Vite**.
- **Tailwind CSS** + **shadcn/ui** components + **Lucide** icons.
- Data fetching: **TanStack Query** (REST) + a thin WS client for realtime.
- Routing: **React Router**. Forms: **react-hook-form** + **zod**.
- State: server state via Query; minimal global UI state via Zustand/Context.

```
web/
└── src/
    ├── api/            # REST client, ws client, zod schemas
    ├── auth/           # token mgmt, guards
    ├── pages/          # route screens
    ├── components/     # shared UI (shadcn-based)
    ├── features/       # agents, jobs, files, skills, devices
    └── store/          # ui state
```

## 2. Core Principles

- **Progress, not internals**: show status, percent, and human-readable messages. Never render terminals, stdout, stack traces, or chain-of-thought.
- **Action-first**: prominent, obvious controls for the 3 job actions (send / cancel / status).
- **Responsive & accessible**: keyboard-navigable, ARIA labels, mobile-friendly, light/dark themes.
- **Optimistic + realtime**: immediate feedback on submit; live updates via WS.

## 3. Screens

The app has a **customer experience** (default) and an **admin console** (gated by `role=admin`). Customers never see device or vault management.

### 3.1 Auth
- **Register / Login** pages (email, username, password). Validation via zod. Friendly errors.
- Token handling per `08-auth-security §2` (access in memory, refresh in HttpOnly cookie).
- After login, route by role: customers → customer dashboard; admins additionally get the **Admin** section.

### 3.2 Dashboard (customer)
- Overview cards: agent count/status, recent jobs, quick "New Job". **No device info** (customers don't see devices).

### 3.3 Agents (customer)
- List of agents with status badge (RUNNING/UNHEALTHY/STOPPED), tags, live resource usage (cpu/mem/disk bars). **No device shown.**
- Agent detail: status, selected skills, controls (start/stop/restart), recent jobs.
- Create agent (name, optional tags). **No device choice** — the platform places it automatically. Default 1 agent per user.

### 3.4 Command / Job
The central workspace:
- **Command interface**: a textarea/structured form to enter the instruction + optional params.
- **File upload**: drag-and-drop, multi-file, progress bars, attaches `file_ids` to the job.
- **Skill selector**: choose **one** skill to run this job (single-select / radio, or none) — a job uses **at most one** skill. Only enabled+visible skills are listed.
- **Submit (send job)** button → creates job, switches to live view.
- **Live job view**: status pill, progress bar (percent), streaming progress messages (text only), elapsed time.
- **Cancel job** button (enabled while non-terminal).
- **Result display**: progress-level result rendered as formatted text/structured summary; downloadable output artifacts if provided.

### 3.5 Jobs History
- Paginated list with filters (agent, status, date). Click → job detail (progress timeline + result).

### 3.6 Skills (customer)
- Shows **only skills made visible to the customer** (`GET /skills`). Each entry shows name, description, and a read-only manifest summary.
- On an agent, the customer toggles **enabled/disabled** among visible skills that are **installed** on the agent's host device. Skills that are visible but not yet installed appear as unavailable with a hint; non-visible skills are never shown.
- Customers **cannot** install/update/delete skills — that is admin-only.

### 3.7 Admin Console (`role=admin` only)
A distinct section, hidden entirely from customers:
- **Device fleet**: list all devices + online status, last seen, resources, hosted agents. "Add device" flow → one-time enrollment code + cross-platform setup instructions (Windows/macOS). Rotate token, rename, decommission.
- **Skill vault**: browse catalog + versions; create entries; publish/deprecate/delete versions (upload manifest + artifact).
- **Fleet rollout**: install/disable/update/delete a skill across **all** devices; per-device rollout status badges (`installing`/`installed`/`disabled`/`updating`/`error`) live via `skill.status`.
- **Organizations (groups)**: create/rename/delete orgs; add/remove customer members. View an org's members + granted skills.
- **Visibility**: set each skill `public`/`restricted` and grant/revoke to **individual customers or whole organizations** (granting an org makes the skill visible to every member).

### 3.8 Settings
- Profile (username/email), password change, sessions/logout, theme.

## 4. Realtime Integration

- Open WS to `/ws` after login; subscribe to topics for the current view (`job:{id}`, `agent:{id}`, `device:{id}`).
- Map events → cache updates:
  - `job.progress` → update job percent/message.
  - `job.result` → mark terminal, render result, toast.
  - `agent.status` / `device.status` → update badges/usage.
  - `skill.status` → update device-wide skill install/update progress badges (admin views).
- Reconnect with backoff; on reconnect, re-fetch active resources to reconcile.

## 5. Job Control UX Mapping

| UI action | API |
|-----------|-----|
| Send job (with ≤ 1 skill) | `POST /agents/{id}/jobs` `{command, file_ids?, skill_id?}` |
| Cancel job | `POST /jobs/{id}/cancel` |
| Query status | WS `job:{id}` (live) + `GET /jobs/{id}` (refresh) |
| Upload file | `POST /files` then reference `file_ids` on submit |
| List visible skills (customer) | `GET /skills` |
| Enable skill on agent (customer) | `POST /agents/{id}/skills` |
| Disable skill on agent (customer) | `DELETE /agents/{id}/skills/{skill_id}` |
| Install skill fleet-wide (admin) | `POST /admin/skills/{id}/install` |
| Disable/update/delete fleet skill (admin) | `POST /admin/skills/{id}/disable\|update` · `DELETE /admin/skills/{id}/install` |
| Publish vault skill version (admin) | `POST /admin/skills/{id}/versions` |
| Set visibility / grant to user or org (admin) | `PATCH /admin/skills/{id}/visibility` · `POST /admin/skills/{id}/grants {principal_type, principal_id}` |
| Manage organization + members (admin) | `POST /admin/orgs` · `POST /admin/orgs/{id}/members` |
| Manage device (admin) | `POST /devices` · `POST /devices/{id}/rotate-token` |

## 6. State & Error Handling

- Loading skeletons for lists/detail.
- Empty states with clear CTAs (e.g., "No agents yet — create one").
- Error surfaces: inline form errors, toast for transient failures, full-page for fatal.
- Specific handling: `DEVICE_OFFLINE` on submit → friendly banner with "device offline" guidance.
- Token expiry → silent refresh; on refresh failure → redirect to login.

## 7. Visual Design

- Clean, calm layout; generous spacing; rounded cards; subtle shadows.
- Status colors: green (running/ok), amber (queued/unhealthy), red (failed), gray (stopped/offline).
- Progress bars + concise status text as the focal point of the job view.
- Light/dark mode via Tailwind + CSS variables (shadcn theme).

## 8. Accessibility & i18n

- WCAG AA contrast, focus rings, semantic landmarks, `aria-live` for progress updates.
- Copy externalized for future localization (web UI strings; spec docs remain English).

## 9. Testing

- Component tests (Vitest + Testing Library).
- E2E (Playwright): register → enroll device (mock) → create agent → submit job → watch progress → see result → cancel path.
- Mock WS server for realtime tests.
