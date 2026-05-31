# 09 — Web UI

Default channel. A friendly, modern interface for controlling agents. **No terminal access, no raw agent internals — progress-level information only.**

## 1. Stack

- **React 18 + TypeScript + Vite**.
- **Tailwind CSS** + **shadcn/ui** components + **Lucide** icons.
- Data fetching: **TanStack Query** (REST) + a thin WS client for realtime.
- Routing: **React Router**. Forms: **react-hook-form** + **zod**.
- State: server state via Query; minimal global UI state via Zustand/Context.
- **Interactive browser**: **noVNC** (`@novnc/novnc`) rendering the agent's headless browser over the `/ws/vnc/{session_id}` WebSocket (`07-api §9.1`).

```
web/
└── src/
    ├── api/            # REST client, ws client, zod schemas
    ├── auth/           # token mgmt, guards
    ├── pages/          # route screens
    ├── components/     # shared UI (shadcn-based)
    ├── features/       # agents, jobs, files, skills, devices, credentials, vnc
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
- Shows agents **currently allocated to the customer's active jobs** — these are "active agents" that are temporarily in use.
- Each shows which job it is running, status (BUSY), live resource usage (cpu/mem/disk bars), and the agent's tags. **No device shown.**
- Agent detail: current job status, selected skills for this job. No controls (start/stop/restart — those are admin pool ops).
- **Customers never create agents.** Agents are pooled and auto-allocated per job. After the job completes, the agent disappears from the customer's view.

### 3.4 Command / Job
The central workspace:
- **Command interface**: a textarea/structured form to enter the instruction + optional params.
- **File upload**: drag-and-drop, multi-file, progress bars, attaches `file_ids` to the job.
- **Skill selector**: choose **one** skill to run this job (single-select / radio, or none) — a job uses **at most one** skill. Only visible skills that are installed on the pool are listed.
- **Saved logins selector**: optionally attach one or more saved logins (`credential_ids`) so the agent's browser starts already signed in. Shows the caller's vault entries (label + origin); never shows cookie contents.
- **Submit (send job)** button → creates job, system allocates an agent from the pool, switches to live view.
- **Live job view**: status pill, progress bar (percent), streaming progress messages (text only), elapsed time.
  - **Queued state**: if all agents are busy, the job shows an "In Queue" state with position badge ("#3 in queue") and estimated wait time. A cancel button is available while queued.
  - **Running state**: standard progress bar, status, messages, plus an **"Open Browser"** button to launch the live VNC view (§3.4.1).
- **Cancel job** button (enabled while non-terminal — queued or running).
- **Result display**: progress-level result rendered as formatted text/structured summary; downloadable output artifacts if provided.
- **Queue full error**: if user has reached the max queued jobs cap (10 by default), the submit button shows a clear error: "Too many queued jobs — wait or cancel one."

#### 3.4.1 Browser Control (live VNC)
Available while a job is **running** (and the agent has VNC enabled):
- **Open Browser** → `POST /jobs/{id}/vnc` → opens a **noVNC** canvas in a panel/modal, connected to `wss://<gateway>/ws/vnc/{session_id}` and authenticated with the returned single-use `rfb_password`.
- The user can **see and control** the agent's headless browser live (mouse/keyboard) — e.g. log into a website, solve a challenge, take over.
- **Save login** button → `POST /vnc/{session_id}/save-login {label}` captures the current site's cookies into the encrypted vault for reuse on future jobs (§3.7). A confirmation toast shows the saved label/origin; cookie contents are never displayed.
- **Connection status** indicator (connecting / live / closed); session auto-closes on job end, idle, or max-duration with a clear notice. **Close** button ends the session (`DELETE /vnc/{session_id}`).

### 3.5 Jobs History
- Paginated list with filters (agent, status, date). Click → job detail (progress timeline + result).

### 3.6 Skills (customer)
- Shows **only skills made visible to the customer** (`GET /skills`). Each entry shows name, description, and a read-only manifest summary.
- The customer selects a skill when submitting a job (at most one). Skills must be **installed** on the pool's host devices to be selectable. Skills visible but not installed appear as unavailable with a hint; non-visible skills are never shown.
- Customers **cannot** install/update/delete skills — that is admin-only.

### 3.7 Saved Logins (customer)
Manage the encrypted login-cookie vault (`07-api §5.2`):
- List saved logins (`GET /credentials`) showing **label**, **origin**, last-used, created — **never** cookie contents.
- **Rename** (`PATCH /credentials/{id}`) and **delete** (`DELETE /credentials/{id}`).
- Logins are **created only from a live VNC session** ("Save login", §3.4.1) — there is no manual cookie upload, by design (cookie content never transits the client).
- Clear explanation copy: "Saved logins are encrypted and reused to sign the agent's browser into a site for you. They are wiped from the agent after each job."

### 3.8 Admin Console (`role=admin` only)
A distinct section, hidden entirely from customers:
- **Device fleet**: list all devices + online status, last seen, resources, hosted agent pool. "Add device" flow → one-time enrollment code + cross-platform setup instructions (Windows/macOS). Configure pool size per device. Rotate token, rename, decommission.
- **Agent pool (fleet-wide)**: view all agents across the fleet, their status (idle/busy/unhealthy/failed), current job if busy. Drain or force-release stuck agents.
- **Skill vault**: browse catalog + versions; create entries; publish/deprecate/delete versions (upload manifest + artifact).
- **Fleet rollout**: install/disable/update/delete a skill across **all** devices; per-device rollout status badges (`installing`/`installed`/`disabled`/`updating`/`error`) live via `skill.status`.
- **User management (tiers)**: view customer list with tier (free/pro/enterprise). Set tier to control queue priority.
- **Organizations (groups)**: create/rename/delete orgs; add/remove customer members. View an org's members + granted skills.
- **Visibility**: set each skill `public`/`restricted` and grant/revoke to **individual customers or whole organizations** (granting an org makes the skill visible to every member).

### 3.9 Settings
- Profile (username/email), password change, sessions/logout, theme.

## 4. Realtime Integration

- Open WS to `/ws` after login; subscribe to topics for the current view (`job:{id}`, `agent:{id}`, `device:{id}`).
- Map events → cache updates:
  - `job.progress` → update job percent/message.
  - `job.queue_update` → update queue position and estimated wait.
  - `job.result` → mark terminal, render result, toast.
  - `agent.status` / `device.status` → update badges/usage.
  - `skill.status` → update device-wide skill install/update progress badges (admin views).
- **VNC**: the noVNC client opens its own binary WebSocket to `/ws/vnc/{session_id}` (separate from the JSON realtime WS); it carries raw RFB and is closed when the panel closes or the job ends.
- Reconnect with backoff; on reconnect, re-fetch active resources to reconcile.

## 5. Job Control UX Mapping

| UI action | API |
|-----------|-----|
| Send job (with ≤ 1 skill) | `POST /jobs` `{command, file_ids?, skill_id?}` — returns `201` (immediate) or `202` (queued with position) |
| View queue position | WS `job.queue_update` event with `{queue_position, estimated_wait_seconds}` |
| Cancel job | `POST /jobs/{id}/cancel` |
| Query status | WS `job:{id}` (live) + `GET /jobs/{id}` (refresh) |
| Upload file | `POST /files` then reference `file_ids` on submit |
| List visible skills (customer) | `GET /skills` |
| Attach saved logins to a job (customer) | `POST /jobs {credential_ids}` |
| Open live browser (VNC) | `POST /jobs/{id}/vnc` then connect noVNC to `/ws/vnc/{session_id}` |
| Save a login from VNC session | `POST /vnc/{session_id}/save-login {label}` |
| Manage saved logins (customer) | `GET/PATCH/DELETE /credentials` |
| List active agents (customer) | `GET /agents` (only those allocated to the caller's active jobs) |
| Install skill fleet-wide (admin) | `POST /admin/skills/{id}/install` |
| Disable/update/delete fleet skill (admin) | `POST /admin/skills/{id}/disable\|update` · `DELETE /admin/skills/{id}/install` |
| Publish vault skill version (admin) | `POST /admin/skills/{id}/versions` |
| Set visibility / grant to user or org (admin) | `PATCH /admin/skills/{id}/visibility` · `POST /admin/skills/{id}/grants {principal_type, principal_id}` |
| Manage organization + members (admin) | `POST /admin/orgs` · `POST /admin/orgs/{id}/members` |
| Manage device (admin) | `POST /devices` · `POST /devices/{id}/rotate-token` |
| Set pool size / view agents (admin) | `POST /admin/devices/{id}/pool` · `GET /admin/agents` |
| Set user tier (admin) | `PATCH /admin/users/{id}/tier` `{tier:"free"|"pro"|"enterprise"}` |

## 6. State & Error Handling

- Loading skeletons for lists/detail.
- Empty states with clear CTAs (e.g., "No agents yet — create one").
- Error surfaces: inline form errors, toast for transient failures, full-page for fatal.
- Specific handling: `DEVICE_OFFLINE` on submit → friendly banner with "device offline" guidance.
- `QUEUE_FULL` (429) → inline error: "You have 10 jobs in queue. Cancel one or wait."
- `QUEUE_TIMEOUT` → job card shows "Job expired in queue" with option to resubmit.
- Queue position updates: WS event `job.queue_update` with `{queue_position, estimated_wait_seconds}` so the UI refreshes position without polling.
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
- E2E (Playwright): register → enroll device (mock) → admin configures pool → submit job → agent auto-allocated → watch progress → see result → agent released → cancel path.
- VNC flow (Playwright + mock relay): open Browser Control on a running job → noVNC connects → "Save login" → entry appears in Saved Logins → attach it to a new job.
- Mock WS server for realtime tests (both the JSON `/ws` and the binary `/ws/vnc/{id}` channels).
