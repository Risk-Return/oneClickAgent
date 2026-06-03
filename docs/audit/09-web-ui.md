# 09-web-ui — Implementation Audit

> Audited against: `docs/spec/09-web-ui.md`, `docs/spec/08-auth-security.md`, `docs/spec/07-api.md`
> Dev record: `docs/dev/09-web-ui.md`
> Date: 2026-06-03

## Summary

| Category | Count |
|----------|-------|
| Critical gaps | 3 |
| Significant gaps | 11 |
| Minor gaps | 11 |

The frontend is structurally complete — all pages, hooks, and UI components are scaffolded — but **3 critical gaps break core flows** (VNC browser control, credential injection at job submit, admin login routing). **11 significant gaps** leave key admin features (user tier management, agent pool view, pool size config) absent and realtime WebSocket integration disconnected from the jobs UI. The dev record's self-assessment of "Implemented" overstates readiness; the VNC integration, WS live updates, and credential-attachment flow are all stubs.

---

## 1. Critical Gaps

### C1: noVNC browser control is not implemented — "Open Browser" is a dead button
- **File:** `web/src/pages/JobsPage.tsx:114-118`
- **Severity:** Critical
- The "Open Browser" button (line 115-117) has no `onClick` handler. `@novnc/novnc` is not listed in `package.json`. The `useOpenVNC` mutation exists in `web/src/features/useCredentials.ts:50-57` but is never imported or called. No VNC canvas/panel/modal exists anywhere. Without this integration, the interactive browser control core feature (§00-overview §3.6, §01-architecture §3.6) is completely non-functional.
- **Spec ref:** `09-web-ui §3.4.1` — "Open Browser → POST /jobs/{id}/vnc → opens a noVNC canvas in a panel/modal, connected to wss://<gateway>/ws/vnc/{session_id}"

### C2: No saved logins selector on job submission — credential_ids never populated
- **File:** `web/src/pages/JobsPage.tsx:28-51` (handleSubmit + mutation)
- **Severity:** Critical
- Spec §3.4 requires a "Saved logins selector: optionally attach one or more saved logins (credential_ids) so the agent's browser starts already signed in." The `SubmitJobRequestSchema` includes `credential_ids` (`web/src/api/schemas.ts:129`), but the JobsPage never renders a saved logins selector, never fetches user credentials for selection, and never populates `credential_ids` in the submit mutation. The entire encrypted credential vault feature (§01-architecture §3.6: "Customer attached credential_ids") has no UI surface.
- **Spec ref:** `09-web-ui §3.4` — "Saved logins selector: optionally attach one or more saved logins (credential_ids) so the agent's browser starts already signed in."

### C3: Admin login routes to `/admin/devices`, skipping the customer dashboard
- **File:** `web/src/pages/LoginPage.tsx:35`
- **Severity:** Critical
- `navigate(role === "admin" ? "/admin/devices" : "/", { replace: true })` — admins are sent directly to the device fleet page, bypassing the dashboard entirely. Spec §3.1 states: "After login, route by role: customers → customer dashboard; admins additionally get the Admin section." The word "additionally" means admins should see the dashboard first, with admin nav items available in the sidebar. The Layout component (`web/src/components/Layout.tsx:66`) correctly merges admin nav for admin users, but the login redirect undermines it.
- **Spec ref:** `09-web-ui §3.1` — "After login, route by role: customers → customer dashboard; admins additionally get the Admin section."

---

## 2. Significant Gaps

### S1: No user management (tiers) admin page
- **File:** Missing entirely
- **Severity:** Significant
- Spec §3.8 requires "User management (tiers): view customer list with tier (free/pro/enterprise). Set tier to control queue priority." No route in `router.tsx`, no page component, no feature hook for `GET /admin/users` or `PATCH /admin/users/{id}/tier`. The `UserTier` zod schema exists at `web/src/api/schemas.ts:3` but no UI consumes it.
- **Spec ref:** `09-web-ui §3.8` — "User management (tiers): view customer list with tier (free/pro/enterprise). Set tier to control queue priority."

### S2: No agent pool fleet-wide admin view
- **File:** Hooks exist at `web/src/features/useAgents.ts:21-48`, no page component
- **Severity:** Significant
- `useAdminAgents`, `useReleaseAgent`, and `useDrainAgent` hooks are implemented, but no UI page exists to display the fleet-wide agent list with status (idle/busy/unhealthy/failed), current job, and drain/release controls. The dev record notes this as a known gap: "the admin agents table view was merged into DeviceFleet" — but DeviceFleet shows devices only, not individual agents.
- **Spec ref:** `09-web-ui §3.8` — "Agent pool (fleet-wide): view all agents across the fleet, their status (idle/busy/unhealthy/failed), current job if busy. Drain or force-release stuck agents."

### S3: No pool size configuration on device page
- **File:** `web/src/pages/admin/DeviceFleetPage.tsx` — no pool size controls
- **Severity:** Significant
- Spec §3.8 requires "Configure pool size per device." The DeviceFleetPage shows device names, status, platform, and last-seen with rotate-token/decommission actions, but has no pool size adjustment UI. The `POST /admin/devices/{id}/pool` endpoint has no corresponding frontend mutation.
- **Spec ref:** `09-web-ui §3.8` — "Configure pool size per device."

### S4: No VNC save-login functionality
- **File:** Missing entirely
- **Severity:** Significant
- Spec §3.4.1 requires a "Save login" button during a VNC session that calls `POST /vnc/{session_id}/save-login {label}`. No mutation hook, no UI button, no toast confirmation exists anywhere. The credential vault create path (VNC session → save) is completely unimplemented on the frontend.
- **Spec ref:** `09-web-ui §3.4.1` — "Save login button → POST /vnc/{session_id}/save-login {label} captures the current site's cookies."

### S5: JobsPage does not use WebSocket for realtime updates
- **File:** `web/src/pages/JobsPage.tsx` — no WS client import or subscribe calls
- **Severity:** Significant
- The WSClient (`web/src/api/ws.ts`) is fully implemented with topic subscribe/unsubscribe and exponential backoff, and spec §4 requires subscribing to `job:{id}` and `job.queue_update` topics for live progress/queue updates. However, JobsPage never imports or calls `getWSClient()`, never subscribes to any topic, and relies entirely on polling (`refetchInterval: 3000` in `web/src/features/useJobs.ts:18-21`). While polling works, the spec mandates live WS updates for progress, queue position, and results.
- **Spec ref:** `09-web-ui §4` — "Open WS to /ws after login; subscribe to topics for the current view (job:{id}, agent:{id}, device:{id})."

### S6: No QUEUE_TIMEOUT handling
- **File:** Missing in `web/src/pages/JobsPage.tsx` and `web/src/features/useJobs.ts`
- **Severity:** Significant
- Spec §6 requires `QUEUE_TIMEOUT` → "job card shows 'Job expired in queue' with option to resubmit." The error code is not handled anywhere. Neither the API client nor the mutation `onError` handler nor the JobProgressCard checks for this state.
- **Spec ref:** `09-web-ui §6` — "QUEUE_TIMEOUT → job card shows 'Job expired in queue' with option to resubmit."

### S7: No device rename on fleet page
- **File:** `web/src/pages/admin/DeviceFleetPage.tsx` — only rotate-token and decommission actions
- **Severity:** Significant
- Spec §3.8 lists admin device actions: "Rotate token, rename, decommission." The page has rotate-token (line 158-160) and decommission/delete (line 162-163), but no inline rename or dialog for editing device name. The `PATCH /devices/{id}` endpoint has no corresponding mutation.
- **Spec ref:** `09-web-ui §3.8` — "Rotate token, rename, decommission."

### S8: Fleet install doesn't pass version
- **File:** `web/src/features/useSkills.ts:55` and `65`, `77`, `88`
- **Severity:** Significant
- Spec §3.5 requires `POST /admin/skills/{id}/install {version}`. The `useInstallSkillFleet` mutation (`useSkills.ts:55`) sends `apiClient.post(\`/admin/skills/${skillId}/install\`)` with no body — the `version` field is never passed. The same issue applies to `useDisableSkillFleet` (line 66), `useEnableSkillFleet` (line 77), and `useDeleteSkillFleet` (line 88), all of which send no body but the spec indicates they require a version parameter.
- **Spec ref:** `09-web-ui §3.5` — "Admin fleet-wide install: POST /admin/skills/{id}/install {version}"

### S9: Skill visibility page doesn't show current grants
- **File:** `web/src/pages/admin/VisibilityPage.tsx`
- **Severity:** Significant
- The VisibilityPage lets admins toggle public/restricted and grant access to a user/org by entering a UUID into a text field. However, spec §3.8 requires "grant/revoke to individual customers or whole organizations." The page has no UI to:
  - View existing grants for a skill
  - Revoke a grant (`DELETE /admin/skills/{id}/grants/{grant_id}`)
  - Select from a list of actual users/orgs (currently a free-text UUID input)
- **Spec ref:** `09-web-ui §3.8` — "Visibility: set each skill public/restricted and grant/revoke to individual customers or whole organizations."

### S10: Publish skill version UI missing
- **File:** `web/src/pages/admin/SkillVaultPage.tsx` — no version publish dialog
- **Severity:** Significant
- Spec §3.8 requires "Publish/deprecate/delete versions (upload manifest + artifact)." The `usePublishSkillVersion` hook exists (`web/src/features/useSkills.ts:40-49`) but is never consumed by any page component. SkillVaultPage only has "Create Skill Entry" and "Install fleet" buttons — there is no UI for uploading version artifacts, deprecating versions, or managing versions.
- **Spec ref:** `09-web-ui §3.8` — "Publish/deprecate/delete versions (upload manifest + artifact)."

### S11: Skills page doesn't show installed vs not-installed distinction
- **File:** `web/src/pages/SkillsPage.tsx:7-8`, `web/src/pages/JobsPage.tsx:165`
- **Severity:** Significant
- Spec §3.6 says "Skills visible but not installed appear as unavailable with a hint." The `SkillSelector` component (`web/src/components/SkillSelector.tsx:28`) has an `installedSkillIds` prop designed for this purpose, but it's never populated when called from JobsPage (line 165: no `installedSkillIds` prop passed). As a result, all visible skills appear as installed/selectable. The SkillsPage similarly shows all visible skills identically, with no installed/unavailable indicator.
- **Spec ref:** `09-web-ui §3.6` — "Skills visible but not installed appear as unavailable with a hint."

---

## 3. Minor Gaps

### M1: Zero component unit tests
- **File:** No test files found (`web/src/**/*.test.{ts,tsx}` — empty)
- **Severity:** Minor
- Spec §9 requires "Component tests (Vitest + Testing Library)." Vitest is configured in `vite.config.ts` (line 1: `/// <reference types="vitest" />`) and `@testing-library/react` is in devDependencies, but zero test files exist. The dev record acknowledges this.
- **Spec ref:** `09-web-ui §9` — "Component tests (Vitest + Testing Library)."

### M2: Zero E2E Playwright tests
- **File:** No Playwright config or test files found
- **Severity:** Minor
- Spec §9 requires three E2E test flows: register→enroll→submit→progress→result→release, cancel path, and VNC flow. Playwright is not listed in `package.json` devDependencies. The dev record acknowledges this.
- **Spec ref:** `09-web-ui §9` — "E2E (Playwright): register → enroll device (mock) → admin configures pool → submit job..."

### M3: No i18n setup
- **File:** No i18n imports found in any source file
- **Severity:** Minor
- Spec §8 requires "Copy externalized for future localization (web UI strings; spec docs remain English)." No i18n library (`react-i18next`, `next-intl`, etc.) is installed, and all user-facing strings are hardcoded English. The standard pattern of wrapping strings in `t()` calls is absent.
- **Spec ref:** `09-web-ui §8` — "Copy externalized for future localization."

### M4: No `aria-live` for progress updates
- **File:** `web/src/components/JobProgressCard.tsx` — entire file, no ARIA live region
- **Severity:** Minor
- Spec §8 requires "`aria-live` for progress updates." The JobProgressCard renders progress via `<Progress>` and static text but has no `aria-live="polite"` region to announce progress changes to screen readers. The Progress component from Radix does not inherently include ARIA live announcements.
- **Spec ref:** `09-web-ui §8` — "WCAG AA contrast, focus rings, semantic landmarks, `aria-live` for progress updates."

### M5: No cross-platform setup instructions during device enrollment
- **File:** `web/src/pages/admin/DeviceFleetPage.tsx:43`
- **Severity:** Minor
- Spec §3.8 requires "'Add device' flow → one-time enrollment code + cross-platform setup instructions (Windows/macOS)." The dialog shows only the enrollment code via a toast: `toast.success('Device created. Enrollment code: ${result.enrollment_code}')`. No instructions for running the device agent on Windows, macOS, or Linux are provided.
- **Spec ref:** `09-web-ui §3.8` — "one-time enrollment code + cross-platform setup instructions (Windows/macOS)."

### M6: Password change is a stub
- **File:** `web/src/pages/SettingsPage.tsx:40`
- **Severity:** Minor
- The password change handler shows `toast.success("Password changed (API not yet implemented)")` and never calls `apiClient`. This is acknowledged in the dev record as a known gap ("Password change API endpoint not implemented in gateway"). The form UI exists but the mutation is a no-op.
- **Spec ref:** `09-web-ui §3.9` — "password change"

### M7: QUEUE_FULL uses toast instead of inline error
- **File:** `web/src/features/useJobs.ts:40-41`
- **Severity:** Minor
- Spec §6 requires `QUEUE_FULL` → "inline error: 'You have 10 jobs in queue. Cancel one or wait.'" The mutation onError handler shows `toast.error(...)`, not an inline error below the submit button. Toasts are transient and easily missed; inline errors persist and provide better UX for blocking conditions.
- **Spec ref:** `09-web-ui §6` — "QUEUE_FULL (429) → inline error: 'You have 10 jobs in queue. Cancel one or wait.'"

### M8: Resource bars show fake/static usage values, not live metrics
- **File:** `web/src/pages/AgentsPage.tsx:61-62`
- **Severity:** Minor
- The ResourceBar components compute fake values: `agent.limits?.cpu / 2 * 100` (line 61) and `agent.limits?.mem_mb * 0.3` (line 62). Spec §3.3 says "live resource usage (cpu/mem/disk bars)." The Agent zod schema (`web/src/api/schemas.ts:77-92`) has no live `usage` field, so the frontend cannot display live metrics — but the fake static calculations are misleading and should show "No live data" or use WS subscription to `agent.status` events with real usage data.
- **Spec ref:** `09-web-ui §3.3` — "live resource usage (cpu/mem/disk bars)"

### M9: No result download artifacts
- **File:** `web/src/pages/JobsPage.tsx:95-106`
- **Severity:** Minor
- Spec §3.4 says "downloadable output artifacts if provided." The result is displayed as raw JSON (`JSON.stringify(job.result, null, 2)`) inside a `<pre>` block (line 101-103) with no download button for output artifacts or structured result export.
- **Spec ref:** `09-web-ui §3.4` — "downloadable output artifacts if provided."

### M10: Organizations page doesn't show members or manage membership
- **File:** `web/src/pages/admin/OrganizationsPage.tsx`
- **Severity:** Minor
- Spec §3.8 requires "View an org's members + granted skills" and "add/remove customer members." The OrganizationsPage only creates and deletes orgs; there is no detail view to list members, add members (`POST /admin/orgs/{id}/members`), remove members, or view the org's granted skills.
- **Spec ref:** `09-web-ui §3.8` — "Organizations (groups): create/rename/delete orgs; add/remove customer members. View an org's members + granted skills."

### M11: Collapsible sidebar animation missing
- **File:** `web/src/components/Layout.tsx:77-81`
- **Severity:** Minor (dev record self-reported)
- The sidebar uses `transition-all duration-300` for width but spec's shadcn/ui pattern expects `collapsible-up`/`collapsible-down` keyframes. The Collapsible component from shadcn is imported but not used for the sidebar. The dev record notes: "Collapsible sidebar animation requires Tailwind keyframes — not configured yet."

---

## 4. What's Solidly Implemented

| # | Feature | Evidence | Spec § |
|---|---------|----------|--------|
| 1 | **Stack compliance** — React 18 + TypeScript + Vite + Tailwind + shadcn/ui + Lucide icons, all confirmed in `package.json` | `web/package.json:1-65` | §1 |
| 2 | **TanStack Query for data fetching** — QueryClientProvider wrapping app, all data fetching via `useQuery`/`useMutation` | `web/src/App.tsx:8-16`, all feature hooks | §1 |
| 3 | **react-router-dom routing** — `createBrowserRouter` with nested layouts, protected routes | `web/src/router.tsx:1-63` | §1 |
| 4 | **react-hook-form + zod validation** — Login and Register pages use form resolver pattern | `web/src/pages/LoginPage.tsx:19-26`, `RegisterPage.tsx:19-26` | §1 |
| 5 | **zustand for UI state** — theme + sidebar state with localStorage persistence | `web/src/store/uiStore.ts:1-22` | §1 |
| 6 | **20 shadcn/ui components** — button, input, card, badge, table, dialog, select, tabs, progress, dropdown-menu, popover, switch, collapsible, scroll-area, skeleton, separator, label, textarea, tooltip, alert-dialog, avatar | `web/src/components/ui/` (20 files) | §1 |
| 7 | **Auth pages** — Login (email+password, form validation, error display), Register (email+username+password), link between both | `web/src/pages/LoginPage.tsx`, `RegisterPage.tsx` | §3.1 |
| 8 | **TokenManager** — access token in memory, refresh token auto-refresh 60s before expiry, deduped concurrent refreshes, JWT role/sub decoding | `web/src/auth/TokenManager.ts:1-169` | §3.1, 08-auth §2 |
| 9 | **Auth guards** — RequireAuth (redirects to /login), RequireAdmin (redirects to / if not admin) | `web/src/auth/AuthGuard.tsx:1-25` | §3.1 |
| 10 | **Role-aware layout** — sidebar renders customer nav for all users, admin nav only if `role=admin` | `web/src/components/Layout.tsx:73, 93` | §3.8 |
| 11 | **Dashboard** — overview cards (active agents, recent jobs, succeeded, failed), recent jobs list, quick actions ("New Command", "View Agents") | `web/src/pages/DashboardPage.tsx:1-128` | §3.2 |
| 12 | **Command/Job page** — textarea command input, file dropzone, skill selector, submit button, live job view with progress bar and status badge | `web/src/pages/JobsPage.tsx:1-188` | §3.4 |
| 13 | **JobProgressCard** — status badge, elapsed time, queue position ("#N in queue ~X min wait"), progress bar, progress message | `web/src/components/JobProgressCard.tsx:1-58` | §3.4 |
| 14 | **Cancel job** — button visible for non-terminal jobs, calls `POST /jobs/{id}/cancel` | `web/src/pages/JobsPage.tsx:109-113` | §3.4, §5 |
| 15 | **FileDropzone** — drag-and-drop, multi-file, progress state, file ID badges with remove button | `web/src/components/FileDropzone.tsx:1-113` | §3.4, §5 |
| 16 | **SkillSelector** — single-select (at most one), "None" option, unavailable state styling, keyboard-accessible | `web/src/components/SkillSelector.tsx:1-53` | §3.4, §3.6 |
| 17 | **Job History** — paginated table with command, status badge, relative timestamps (date-fns), link to job detail | `web/src/pages/JobHistoryPage.tsx:1-79` | §3.5 |
| 18 | **Agents page** — shows allocated agents with name, status badge, tags, resource bars, link to running job | `web/src/pages/AgentsPage.tsx:1-77` | §3.3 |
| 19 | **Skills page** — shows visible skills with name, key, visibility badge, version, description | `web/src/pages/SkillsPage.tsx:1-62` | §3.6 |
| 20 | **SavedLogins page** — list with label, origin, last-used timestamp, inline rename, delete with confirmation dialog, explanation copy exactly matching spec | `web/src/pages/SavedLoginsPage.tsx:1-156` | §3.7 |
| 21 | **DeviceFleet page** — device table (name, status, platform, last-seen), "Add Device" dialog, rotate-token, decommission | `web/src/pages/admin/DeviceFleetPage.tsx:1-176` | §3.8 |
| 22 | **SkillVault page** — catalog table (key, name, visibility, version, status), "New Skill" dialog, "Install fleet" per-skill button | `web/src/pages/admin/SkillVaultPage.tsx:1-166` | §3.8 |
| 23 | **FleetRollout page** — per-skill enable/disable/delete fleet-wide actions | `web/src/pages/admin/FleetRolloutPage.tsx:1-92` | §3.8 |
| 24 | **Organizations page** — create/delete orgs, org table with name/description/created-date | `web/src/pages/admin/OrganizationsPage.tsx:1-160` | §3.8 |
| 25 | **Visibility page** — public/restricted toggle per skill, grant dialog with principal_type (user/org) + principal_id UUID | `web/src/pages/admin/VisibilityPage.tsx:1-203` | §3.8 |
| 26 | **Settings page** — profile display (email, username), appearance theme toggle (light/dark/system), logout | `web/src/pages/SettingsPage.tsx:1-167` | §3.9 |
| 27 | **Light/dark theme** — CSS variables for both modes, Tailwind `class` strategy, system preference watcher | `web/src/index.css:1-60`, `web/src/theme.tsx:1-29` | §7 |
| 28 | **WSClient** — singleton, topic subscribe/unsubscribe, exponential backoff reconnect (`min(1s*2^n, 30s)`), auto-resubscribe on reconnect, token auth on connect | `web/src/api/ws.ts:1-131` | §4 |
| 29 | **API client** — Bearer token injection, automatic 401 → refresh + retry, multipart upload with retry, unified error class | `web/src/api/client.ts:1-132` | §6 |
| 30 | **Zod schemas** — complete type coverage: User, Auth, Device, Agent, Job, File, Skill, SkillVersion, DeviceSkill, SkillGrant, Organization, VNC, Credential, WS messages | `web/src/api/schemas.ts:1-266` | §1 |
| 31 | **Feature hooks** — useJobs (list, detail, submit, cancel, result), useAgents (customer, admin, release, drain), useSkills (visible, admin, create, publish, install/disable/enable/delete fleet), useFiles, useCredentials (list, update, delete, openVNC) | `web/src/features/useJobs.ts`, `useAgents.ts`, `useSkills.ts`, `useFiles.ts`, `useCredentials.ts` | §1, §5 |
| 32 | **Loading skeletons** — all list/detail views have skeleton loading states | DashboardPage, JobHistoryPage, AgentsPage, SkillsPage, etc. | §6 |
| 33 | **Empty states** — icons + descriptive text + CTA buttons for agents, skills, logins, devices, etc. | AgentsPage:37-43, SkillsPage:31-36, SavedLoginsPage:85-89 | §6 |
| 34 | **Toast notifications** — sonner for success/error feedback across all mutations | All feature hooks, SettingsPage | §6 |
| 35 | **Status colors** — green (running/ok), amber (queued/unhealthy), red (failed), gray (stopped/offline) as spec §7 prescribes | `web/src/components/AgentStatusBadge.tsx:6-34` | §7 |
| 36 | **Focus rings** — `focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2` on all interactive shadcn/ui components | button, input, textarea, select, switch, tabs, dialog-close | §8 |
| 37 | **Vite dev proxy** — `/api`, `/ws`, `/tunnel` proxied to `localhost:8080` with WS support | `web/vite.config.ts:9-23` | §1 |

---

## 5. Fix Priority

| Priority | ID | Gap | Impact |
|----------|----|-----|--------|
| P0 | C1 | noVNC browser control not implemented | Interactive browser is a core feature — dead button, no canvas renderer |
| P0 | C2 | No saved logins selector on job submit | Credential vault injection path is completely broken from UI side |
| P0 | C3 | Admin login skips dashboard | Violates spec routing contract; admins lose dashboard overview |
| P1 | S1 | No user tier management page | Admins cannot view or set customer tiers for queue priority |
| P1 | S2 | No agent pool fleet-wide view | Admins cannot see or manage individual agents (drain/release) |
| P1 | S4 | No VNC save-login functionality | Credential vault create path (VNC → vault) has no UI surface |
| P1 | S5 | JobsPage doesn't use WebSocket | Queue position and progress updates rely on polling, not live WS |
| P1 | S8 | Fleet install/disable/enable/delete don't pass version | API calls may fail if the gateway requires version parameter |
| P2 | S3 | No pool size config on device page | Admins cannot adjust agent pool size per device from UI |
| P2 | S6 | No QUEUE_TIMEOUT handling | Expired queue jobs have no user-facing recovery path |
| P2 | S7 | No device rename | Device management is incomplete per spec |
| P2 | S9 | Skill visibility grants cannot be viewed or revoked | Admin can grant but has no visibility into existing grants |
| P2 | S10 | Publish skill version UI missing | Admin can create skill entries but cannot upload version artifacts |
| P2 | S11 | Skills page doesn't show installed status | Customers can't distinguish available vs selectable skills |
| P3 | M1 | No component unit tests | Technical debt — no regression protection |
| P3 | M2 | No E2E Playwright tests | No end-to-end validation of critical flows |
| P3 | M3 | No i18n setup | Harder to add later; refactoring all hardcoded strings is costly |
| P3 | M4 | No `aria-live` for progress | Screen reader users get no progress announcements |
| P3 | M6 | Password change is a stub | Settings → password change does nothing |
| P3 | M7 | QUEUE_FULL uses toast, not inline error | Transient toast may be missed vs persistent inline error |
| P3 | M8 | Resource bars show fake static usage | Misleading UI — shows invented numbers instead of "No live data" |
| P3 | M10 | Organizations page doesn't manage members | Org CRUD exists but membership management is absent |

---

## 6. Dev Record Accuracy

The dev record at `docs/dev/09-web-ui.md` claims status "Implemented" and lists 20 packages as "Done." This audit finds:

- **The "Done" assessment overstates readiness.** While all pages and hooks are structurally complete, the 3 critical gaps mean core features (VNC browser control, credential injection) are non-functional from the UI perspective. The "Open Browser" button is a dead element with no handler.
- **The 6 known gaps in the dev record are accurate** but incomplete — this audit found 25 gaps vs the 6 documented.
- **Key undocumented gaps**: missing admin user-tier page (S1), missing agent pool view (S2), missing WS integration in JobsPage (S5), fleet install missing version param (S8), no credential selector on job submit (C2), no device rename (S7), no QUEUE_TIMEOUT handling (S6), no VNC save-login (S4), no grant revoke/view (S9).
- The dev record mentions "noVNC npm package not yet installed" as a known gap, but doesn't mention that the button itself has no click handler and the mutation is never imported — the gap is deeper than just the missing dependency.
- The dev record says "Agent pool admin management page — currently only in hooks" — this is accurate.

