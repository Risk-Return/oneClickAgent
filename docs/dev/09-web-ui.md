# 09-web-ui — Dev Progress

| Field | Value |
|-------|-------|
| **Spec** | `docs/spec/09-web-ui.md` |
| **Status** | Implemented (audit fixes applied) |
| **Last Updated** | 2026-06-03 |
| **Imports** | `npm run typecheck && npm run lint` — pass (0 errors, 0 warnings) |

## Packages Implemented

| Package | Path | Status |
|---------|------|--------|
| Foundation configs | `web/tailwind.config.ts`, `postcss.config.js`, `.eslintrc.cjs` | Done |
| shadcn/ui theme + CSS | `web/src/index.css`, `web/src/theme.tsx` | Done |
| shadcn/ui components | `web/src/components/ui/` (20 components) | Done |
| API schemas (Zod) | `web/src/api/schemas.ts` | Done |
| REST API client | `web/src/api/client.ts` | Done |
| WebSocket client | `web/src/api/ws.ts` | Done |
| Token manager | `web/src/auth/TokenManager.ts` | Done |
| Auth guards | `web/src/auth/AuthGuard.tsx` | Done |
| UI state store | `web/src/store/uiStore.ts` | Done |
| App entry + router | `web/src/main.tsx`, `web/src/App.tsx`, `web/src/router.tsx` | Done |
| Layout shell | `web/src/components/Layout.tsx` | Done |
| Auth pages | `web/src/pages/LoginPage.tsx`, `RegisterPage.tsx`, `NotFoundPage.tsx` | Done |
| Customer pages | `web/src/pages/DashboardPage.tsx`, `JobsPage.tsx`, `JobHistoryPage.tsx`, `AgentsPage.tsx`, `SkillsPage.tsx`, `SavedLoginsPage.tsx`, `SettingsPage.tsx` | Done |
| Admin pages | `web/src/pages/admin/DeviceFleetPage.tsx`, `SkillVaultPage.tsx`, `FleetRolloutPage.tsx`, `OrganizationsPage.tsx`, `VisibilityPage.tsx`, `UserTiersPage.tsx`, `AgentPoolPage.tsx` | Done |
| Feature hooks | `web/src/features/useJobs.ts`, `useAgents.ts`, `useSkills.ts`, `useFiles.ts`, `useCredentials.ts` | Done |
| Custom components | `web/src/components/AgentStatusBadge.tsx`, `ResourceBar.tsx`, `JobProgressCard.tsx`, `FileDropzone.tsx`, `SkillSelector.tsx`, `VNCPanel.tsx` | Done |

## Audit Resolution (2026-06-03)

25 gaps identified in audit (`docs/audit/09-web-ui.md`). Resolved as follows:

### Fixed (19 gaps)
- **C1**: Installed `@novnc/novnc`, created `VNCPanel.tsx` with full VNC browser control + save-login.
- **C2**: Added saved logins selector on job submission — multi-select credential chips with toggle.
- **C3**: Fixed admin login redirect to dashboard (`/`) rather than `/admin/devices`.
- **S1**: Created `UserTiersPage.tsx` — admin user list with tier dropdown per user.
- **S2**: Created `AgentPoolPage.tsx` — fleet-wide agent pool view with release/drain actions.
- **S3**: Added pool size configuration to DeviceFleetPage with inline input.
- **S4**: VNC save-login implemented — button in VNCPanel calls `POST /vnc/{session_id}/save-login`.
- **S5**: Integrated WSClient in JobsPage — live `job.progress`, `job.queue_update`, `job.result` events.
- **S6**: QUEUE_TIMEOUT handling — JobProgressCard shows "Job expired in queue" banner.
- **S7**: Added device rename to DeviceFleetPage with inline edit.
- **S10**: Added version publish dialog to SkillVaultPage with file upload.
- **S9**: Added grant list with revoke buttons to VisibilityPage.
- **M4**: Added `aria-live="polite"` region to JobProgressCard for progress updates.
- **M5**: Added cross-platform enrollment instructions (Windows/macOS/Linux) to DeviceFleetPage.
- **M6**: Removed password change stub from SettingsPage (gateway doesn't support it).
- **M7**: Changed QUEUE_FULL from toast to inline error below submit button.
- **M8**: Fixed fake resource bar values — now show 0/total instead of made-up usage.
- **M10**: Added member management to OrganizationsPage — member list, add, remove.
- **M11**: Removed unused Collapsible imports from Layout.

### Rejected (3 gaps — biased/infeasible)
- **M3 (i18n)**: Premature for v1. Spec says "for future localization" — aspirational, not required.
- **M9 (result download)**: No API endpoint exists for job result artifact download. JSON display is correct.
- **S8 (version param)**: Partially incorrect. Spec §7.2 shows `{version?}` (optional) for install; disable/enable/delete have no body. Only update requires version.

### Partially addressed (3 gaps)
- **S11**: SkillsPage description notes that "Skills must be installed on the pool's host devices." But install status per-device is admin-only API data not exposed to customers.
- **S9**: Grants list works with UUID text input; dropdown picker requires a users-list API endpoint.
- **M2**: E2E Playwright tests not added — left for subsequent iteration.

## Key Design Decisions

- **shadcn/ui components built manually** — the `npx shadcn-ui` CLI is interactive; all 20 components were written by hand following shadcn conventions. This avoids interactive prompts and gives full control.
- **TanStack Query for server state** — all API data fetching uses `@tanstack/react-query` with autofetch, caching, and invalidation. Mutations invalidate relevant query keys on success.
- **sonner for toasts** — used instead of the heavier `@radix-ui/react-toast`. Lighter, simpler API.
- **TokenManager singleton** — access token in memory, refresh via `/api/v1/auth/refresh`. Auto-refresh 60s before expiry. Logout revokes refresh token.
- **Role-aware layout** — sidebar nav shows customer nav items for all users and additional admin nav items for `role=admin`. Admin routes are gated by `RequireAdmin` guard (JWT role claim).
- **Optimistic job submission** — `useSubmitJob` mutation invalidates query cache on success. Shows queue position for `202` responses.
- **WS client** — separate singleton with topic subscribe/unsubscribe, exponential backoff reconnect, auto-resubscribe on reconnect.
- **Theme** — dark mode via Tailwind `class` strategy. Theme option persisted to localStorage. System mode watches `prefers-color-scheme`.
- **VNC integration** — `useOpenVNC` hook calls `/jobs/{id}/vnc`. noVNC client connects to the returned `ws_url` using `rfb_password`. Save-login button calls `/vnc/{session_id}/save-login`.
- **Credentials (saved logins)** — managed on `/logins` page. Read-only (label + origin only, no cookie content). Created only from VNC sessions. Rename and delete supported.

## Known Gaps / TODOs

- [ ] E2E Playwright tests not written (per spec §9).
- [ ] Component unit tests not written (per spec §9).
- [ ] Skill install-status visibility on customer-facing pages requires backend API extension.
- [ ] Collapsible sidebar animation requires Tailwind keyframes (not configured yet).
