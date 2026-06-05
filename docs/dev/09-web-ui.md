# 09-web-ui — Dev Progress

| Field | Value |
|-------|-------|
| **Spec** | `docs/spec/09-web-ui.md` |
| **Status** | Implemented (audit fixes + i18n + UUID search + tooltips + skill compose + session persistence) |
| **Last Updated** | 2026-06-05 |
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
| i18n translations | `web/src/i18n/en.json`, `web/src/i18n/index.ts` | Done |
| Type declarations | `web/src/types/novnc.d.ts` | Done |

## Key Design Decisions

- **i18next + react-i18next** — all user-facing strings externalized to `src/i18n/en.json` with namespaced keys. Language detection via browser navigator. Adding new languages requires only a new JSON file.
- **Downloadable artifacts** — job results with a "Download Result" button that exports the result JSON as a downloadable `.json` file via client-side blob/URL.createObjectURL. No server-side download endpoint needed.
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
- [ ] Fleet Rollout page: per-agent expandable rows + Retry button (per spec update 2026-06-05 — `SKILL_RETRY` frame defined).
- [ ] Skill install-status visibility on customer-facing pages requires backend API extension.
- [ ] Collapsible sidebar animation requires Tailwind keyframes (not configured yet).

## Recent Changes (2026-06-05)

- Skill composition: command prefixed with skill prompt + output dir instructions
- UUID search bars on User Tiers, Organizations, and Visibility pages
- Tooltips on Device Fleet action icons (rename, rotate token, decommission)
- Member search with visual boundary in org members panel
- Session persistence via localStorage (survives page refresh)
- Skill vault: publish sends correct artifact field, latest_version updated on publish
- Status badge clickable toggle (active/deprecated) in Skill Vault
- Delete skill button in Skill Vault
