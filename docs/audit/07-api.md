# 07-api — Implementation Audit

> Audited against: docs/spec/07-api.md, docs/spec/00-overview.md, docs/spec/01-architecture.md
> Dev record: None found (docs/dev/07-api.md does not exist)
> Date: 2026-06-03

## Summary

| Category | Count |
|----------|-------|
| Critical gaps | 1 |
| Significant gaps | 5 |
| Minor gaps | 5 |

## 1. Critical Gaps

- **`checkAgentAccess` is a security stub — always permits access**
  - **File:** `gateway/internal/httpapi/agents_handler.go:246-248`
  - **Severity:** Critical
  - The function unconditionally returns `nil` (no error), meaning **any authenticated user can access any agent** — view its details, enable/disable skills on it — regardless of ownership. The spec §4 (Customer visibility) says customers see "only the agents **currently allocated to the caller's active jobs**." These customer-facing endpoints rely on `checkAgentAccess` for tenant scoping:
    - `GET /api/v1/agents/{agentID}` — `handleGetAgent` calls `checkAgentAccess` at line 43
    - `POST /api/v1/agents/{agentID}/skills` — `handleEnableAgentSkill` at line 73
    - `DELETE /api/v1/agents/{agentID}/skills/{skillID}` — `handleDisableAgentSkill` at line 140
  - Admin routes are already protected by `requireAdminMiddleware`. Customer routes need `checkAgentAccess` to validate that the agent's `user_id` matches the authenticated user (or that the user is admin). Currently it simply passes.
  - **Spec ref:** `07-api.md` §4 — "Customers see only the agents currently allocated to their active jobs" + `08-auth-security.md` tenant scoping requirement.

## 2. Significant Gaps

- **No `Idempotency-Key` header handling on HTTP mutating POSTs**
  - **File:** No middleware or handler processes it. Searched entire `gateway/` for `Idempotency` — only found tunnel-layer `msg_id` dedup in `gateway/internal/tunnel/device_conn.go:33-34,239`, not HTTP-layer.
  - **Severity:** Significant
  - Spec §1 (Conventions) states: "mutating POSTs accept `Idempotency-Key` header." The tunnel protocol has per-frame idempotency by `msg_id`, but the HTTP API layer does not. Duplicate POST requests (e.g., job submission, device creation) could result in double operations with no safety net.
  - Affected endpoints: `POST /api/v1/jobs`, `POST /api/v1/files`, `POST /api/v1/devices`, `POST /api/v1/admin/skills`, `POST /api/v1/admin/orgs`, all mutating endpoints.
  - **Spec ref:** `07-api.md` §1 — "Idempotency: mutating POSTs accept `Idempotency-Key` header."

- **Error responses missing `request_id` field**
  - **File:** `gateway/internal/httpapi/middleware.go:274-283` (writeError function)
  - **Severity:** Significant
  - Spec §1 (Conventions) dictates error shape: `{ "error": { "code": "...", "message": "...", "request_id": "01J…" } }`. The `writeError` function constructs `APIError` with only `Code` and `Message`. The chi `RequestID` middleware generates request IDs (line 31) but they are never included in the error response body — only logged.
  - This makes it impossible for clients to correlate error responses with their requests for support/debugging.
  - **Spec ref:** `07-api.md` §1 — Error shape includes `request_id`.

- **Missing `GET /admin/skills` (list vault catalog) route**
  - **File:** `gateway/internal/httpapi/router.go` — no route for listing all admin skills
  - **Severity:** Significant
  - Spec §7.1 says `GET /admin/skills` — "list full vault catalog (all skills + versions + visibility)." The `Vault.ListSkills` method exists in `gateway/internal/skillvault/vault.go:54`, and `store.SkillStore.ListSkills` exists in `gateway/internal/store/skills.go:45`, but neither is wired to any HTTP route. The router only has `GET /api/v1/admin/skills/{skillID}` (individual). An admin cannot browse the full skill catalog without knowing skill IDs.
  - **Spec ref:** `07-api.md` §7.1 — `GET /admin/skills` "list full vault catalog"

- **Missing `GET /admin/orgs/{id}/members` route**
  - **File:** `gateway/internal/httpapi/router.go:168-177` — org group has POST and DELETE for members but no GET
  - **Severity:** Significant
  - Spec §8 says `GET /admin/orgs/{id}/members` — "list member users." The router registers `POST /api/v1/admin/orgs/{orgID}/members` (add) and `DELETE /api/v1/admin/orgs/{orgID}/members/{userID}` (remove) but no handler to list members. The store layer likely has the necessary query; a handler (`handleListOrgMembers`) is missing.
  - **Spec ref:** `07-api.md` §8 — `GET /admin/orgs/{id}/members` "list member users"

- **WebSocket handler does not check `Authorization` header**
  - **File:** `gateway/internal/httpapi/ws_handler.go:26-34`
  - **Severity:** Significant
  - Spec §9 says the WS endpoint accepts `?token=<access_jwt>` **or** `Authorization` header. The handler only reads `r.URL.Query().Get("token")` at line 26. It also attempts to read `Sec-WebSocket-Protocol` header (line 29) as a fallback, but this is not an auth mechanism — it's for subprotocol negotiation. The `Authorization: Bearer <token>` header, which is the primary auth mechanism for REST endpoints and explicitly called out in §9, is never consumed for WS auth.
  - **Spec ref:** `07-api.md` §9 — "`?token=<access_jwt>` or `Authorization` header"

- **`GET /admin/skills` is missing AND `GET /admin/skills/{id}/grants` DELETE has two competing implementations**
  - **File:** `gateway/internal/httpapi/router.go:163-164`
  - **Severity:** Significant
  - Two DELETE routes exist for grants: `DELETE /api/v1/admin/skills/{skillID}/grants` (body-based, handled by `handleDeleteSkillGrant`) and `DELETE /api/v1/admin/skills/{skillID}/grants/{principal_type}/{principal_id}` (path-based, handled by `handleDeleteSkillGrantPath`). The spec §7.3 only defines the path-based form. The body-based form is an undocumented alternate path that could cause confusion. Both work, but the dual implementation is architectural drift.
  - **Spec ref:** `07-api.md` §7.3 — `DELETE /admin/skills/{id}/grants/{principal_type}/{principal_id}`

## 3. Minor Gaps

- **Logout returns 200 instead of 204**
  - **File:** `gateway/internal/httpapi/auth_handler.go:233`
  - **Severity:** Minor
  - Spec §2 says `POST /auth/logout` returns `204` (no content). The handler returns `200` with JSON body `{"message": "logged out"}`. Functionally harmless but violates the spec response code contract. Frontend clients expecting 204 may ignore the body anyway.
  - **Spec ref:** `07-api.md` §2 — `POST /auth/logout` → `204` (revokes refresh)

- **Pagination response uses `data` instead of `items`**
  - **File:** `gateway/internal/model/types.go:472-476` (`PaginatedResponse` struct)
  - **Severity:** Minor
  - Spec §1 (Conventions) dictates pagination response shape: `{ "items": [...], "next_cursor": "…|null" }`. The `PaginatedResponse` struct uses the field name `data` (not `items`). Frontend code ported from the spec would break.
  - Also, `next_cursor` is typed as `*UUID` (not an opaque string as the spec suggests with `"…|null"`).
  - **Spec ref:** `07-api.md` §1 — Pagination response shape

- **Cursor parsed as UUID, not opaque**
  - **File:** `gateway/internal/httpapi/devices_handler.go:231-240` (`parseCursor`)
  - **Severity:** Minor
  - Spec §1 says `cursor=<opaque>`. `parseCursor` calls `model.ParseUUID(cursorStr)`, converting the cursor to a UUID. If the cursor were an opaque string (e.g., base64-encoded), this would fail silently and return `nil`, causing every request to fetch from offset 0. In practice this works because the current implementation uses UUIDs as cursors, but it's not spec-compliant for true opaque cursors.
  - **Spec ref:** `07-api.md` §1 — `cursor=<opaque>`

- **WS subscribe uses `topic` (singular) instead of `topics` (array)**
  - **File:** `gateway/internal/httpapi/ws_handler.go:106-110`
  - **Severity:** Minor
  - Spec §9 says client→server message: `{ "type": "subscribe", "topics": ["job:01J…", "agent:01J…"] }`. The handler reads `wsMsg["topic"]` (singular string), allowing only one topic per subscribe message. Clients built to the spec sending an array of topics will silently fail — no topic extracted, no subscription created.
  - **Spec ref:** `07-api.md` §9 — Subscribe message format with `topics` array

- **`GET /api/v1/files` (list files) not in spec**
  - **File:** `gateway/internal/httpapi/router.go:139`
  - **Severity:** Minor
  - The spec §6 only defines `POST /files`, `GET /files/{id}`, and `DELETE /files/{id}` — no list/batch endpoint. The router adds `GET /api/v1/files` (listing). This is a feature addition, not a gap. However, it should be considered for spec update or acknowledged as deliberate extension.
  - **Spec ref:** `07-api.md` §6 — No `GET /files` (list) defined

## 4. What's Solidly Implemented

| Feature | Package/File | Status |
|---------|-------------|--------|
| Auth endpoints (register, login, refresh, logout, me) | `httpapi/auth_handler.go` | ✅ All 5 endpoints, Argon2id hashing, JWT issuing, refresh token rotation + family revoke |
| Auth middleware (JWT verification, context injection) | `httpapi/middleware.go:130-163` | ✅ Bearer token extraction, verify, inject user_id/role/claims into context |
| Admin RBAC middleware | `httpapi/middleware.go:166-175` | ✅ `requireAdminMiddleware` blocking non-admins with 403 |
| Tenant scoping middleware (jobs + files) | `httpapi/middleware.go:181-232` | ✅ Extracts UUID from path, validates ownership, admins bypass |
| Devices CRUD (admin-only) | `httpapi/devices_handler.go` | ✅ All 7 spec endpoints: list, create, get, update, delete, rotate-token, enroll |
| Device enrollment (public) | `httpapi/devices_handler.go:82-117` | ✅ Enrollment code validation, token generation, hash storage |
| Agent pool management (admin) | `httpapi/agents_handler.go` + `router.go` | ✅ All 6 admin endpoints: list, get, release, drain, delete, pool-size |
| Agent customer visibility | `httpapi/agents_handler.go:10-50` | ✅ Routes exist, tenant scoping depends on fix to `checkAgentAccess` |
| Job lifecycle (submit, list, get, cancel, result) | `httpapi/jobs_handler.go` | ✅ All 5 spec endpoints, queue position, estimated wait, agent allocation |
| File upload + management | `httpapi/files_handler.go` | ✅ Multipart upload, SHA256, staging, user-scoped list/get/delete |
| Skill vault (admin CRUD + versions) | `httpapi/skills_handler.go` | ✅ Create, get, update, delete, publish version — all admin routes |
| Skill fleet management (install/disable/enable/update/delete on all devices) | `httpapi/skills_handler.go` | ✅ All 6 fleet endpoints dispatch to all devices |
| Skill visibility + grants | `httpapi/skills_handler.go:260-374` | ✅ visibility PATCH, grant POST/DELETE (both path and body), grant listing |
| Skill customer visibility | `httpapi/skills_handler.go:10-57` | ✅ Resolves public ∪ user grants ∪ org grants |
| Organizations CRUD + membership | `httpapi/admin_handler.go` | ✅ Create, list, get, update, delete org; add/remove members |
| User tier management | `httpapi/admin_handler.go:170-195` | ✅ PATCH tier, validation against free/pro/enterprise |
| Credential vault endpoints | `httpapi/vnc_handler.go:127-231` | ✅ List, get (metadata only), update label, delete — all tenant-scoped |
| VNC session lifecycle | `httpapi/vnc_handler.go:15-125` | ✅ Open (creates session + VNC_OPEN frame), close (VNC_CLOSE frame), status, save-login |
| VNC WebSocket relay endpoints | `httpapi/vnc_handler.go:289-362` | ✅ Browser socket (JWT auth + user ownership check), device socket (session token + hash) |
| VNC relay engine | `vncrelay/relay.go` | ✅ Session pairing (browser↔device), binary byte pump, idle/max TTL reaper, per-user concurrency cap |
| Credential vault encryption | `credvault/vault.go` | ✅ AES-256-GCM encrypt/decrypt, KMS support, SHA256 integrity |
| WebSocket realtime (web) | `httpapi/ws_handler.go` | ✅ JWT auth, pub/sub broker integration, subscribe/unsubscribe/ping/pong |
| Tunnel WebSocket endpoint | `httpapi/tunnel_handler.go` | ✅ Device token auth, token hash verification, subprotocol negotiation, read/write pumps |
| Health/readiness/metrics | `httpapi/auth_handler.go:249-265` + `router.go:76-80` | ✅ `/healthz`, `/readyz` (DB ping), `/metrics` (Prometheus) |
| Channel adapter interface | `channel/adapter.go` | ✅ `Adapter` interface with ParseInbound, SendOutbound, Authenticate; `StubAdapter` for unsupported channels |
| Error shape (uniform) | `middleware.go:274-283` | ✅ Consistent `APIError{Code, Message}` — but missing `request_id` (see gap) |
| CORS middleware | `router.go:200-212` | ✅ Configurable origins, creds, exposed headers |
| Rate limiting middleware | `middleware.go:52-104` | ✅ Per-IP sliding window, configurable per-second cap |
| UUIDv7 for all IDs | `model/types.go:16-21` | ✅ `NewUUID()` uses `uuid.NewV7()` |
| Job state machine | `model/types.go:68-86` | ✅ All 7 statuses, IsTerminal(), IsActive() |
| Agent pool allocation | `pool/allocator.go` + `jobs_handler.go:64` | ✅ Tiered FIFO, queue position, expiry TTL, per-user cap |
| Configuration | `config/config.go` | ✅ All env vars from spec (TTLs, limits, VNC config, cred vault config) |
| `IAGENT_QUEUE_TTL`, `IAGENT_MAX_QUEUED_PER_USER` enforcement | `jobs_handler.go:66-68` + `pool/allocator.go` | ✅ Queue full returns 429 QUEUE_FULL |
| One-skill-per-job constraint | `jobs_handler.go:31-43` | ✅ Skill visibility validated against vault (public ∪ grants) |
| Credential injection on job submit | `jobs_handler.go:97-120` | ✅ CRED_PUSH frame sent over tunnel, credential→job linking, last_used_at touch |

## 5. Fix Priority

| Priority | Gap | Impact |
|----------|-----|--------|
| P0 | `checkAgentAccess` is a no-op stub | Any customer can view/manipulate any agent. Security bypass. |
| P1 | No `Idempotency-Key` header handling | Duplicate POSTs (job submission, device creation) can double-execute. |
| P1 | Error responses missing `request_id` | Clients cannot correlate errors with requests for debugging. |
| P2 | Missing `GET /admin/skills` route | Admins cannot browse the vault catalog; must know skill IDs. |
| P2 | Missing `GET /admin/orgs/{id}/members` route | Admins cannot view org membership lists. |
| P2 | WS auth ignores `Authorization` header | Clients using Bearer header auth for WS will get 401. |
| P3 | Logout returns 200 instead of 204 | Minor spec deviation; functionally harmless. |
| P3 | Pagination uses `data` not `items` | Frontend ported from spec needs field rename. |
| P3 | Cursor parsed as UUID not opaque | Works with current UUID cursors but limits future opaque cursor use. |
| P3 | WS subscribe uses `topic` not `topics` array | Clients can only subscribe to one topic per message. |
