# 08 — Auth & Security — Dev Progress

| Field | Value |
|-------|-------|
| **Spec** | `docs/spec/08-auth-security.md` |
| **Status** | Implemented |
| **Last Updated** | 2026-06-03 |
| **Imports** | `go vet ./...` OK; `ruff check .` OK; `mypy .` OK; `go test ./internal/auth/... ./internal/httpapi/...` PASS |

## Changes Implemented

### 1. Fixed Refresh Token RevokeFamily (Spec §2 — Token Theft Detection)

- **Migration**: `gateway/migrations/004_add_token_family.up.sql` adds `family` column to `refresh_tokens` + index on `token_hash`
- **Model**: `model/types.go:357` — `RefreshToken.Family` now mapped to DB column `family` (was `db:"-"`)
- **Store**: `store/tokens.go` — `Create` stores `family`; `GetByHash` reads `family`; `RevokeFamily` uses `WHERE family=$1` (previously used broken `id::text LIKE` pattern that never matched)
- The rotating-refresh token theft detection now works correctly

### 2. Security Headers Middleware (Spec §12)

- **File**: `httpapi/middleware.go:251-265`
- Sets on every response: `Strict-Transport-Security`, `Content-Security-Policy`, `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Cross-Origin-Opener-Policy`, `Cache-Control`
- Applied globally in router at `router.go:60`

### 3. CSRF Protection Middleware (Spec §9)

- **File**: `httpapi/middleware.go:269-307`
- Validates `Origin`/`Referer` headers for all state-changing methods (POST, PATCH, PUT, DELETE)
- Bypasses: GET/HEAD/OPTIONS/TRACE, wildcard origins, and requests with no Origin/Referer (non-browser clients)
- Applied after CORS in router at `router.go:62`

### 4. Auth-Specific Rate Limiting (Spec §9)

- **File**: `httpapi/middleware.go:311-353`
- Per-IP rate limiter applied exclusively to auth endpoints (login/register/refresh)
- Default: 10 requests per minute (configurable via `IAGENT_RATE_LIMIT_AUTH_PER_MIN`)
- Applied in router at `router.go:67`

### 5. Audit Logging in Auth Handlers (Spec §4.3, §12)

- **File**: `httpapi/auth_handler.go` — added `audit.Log` calls to:
  - `handleRegister` — `"auth.register"` (line 91)
  - `handleLogin` — `"auth.login"` (line 148)
  - `handleRefresh` — `"auth.token_refresh"` (line 218)
  - `handleLogout` — `"auth.logout"` (line 232)
- **File**: `httpapi/devices_handler.go` — added `audit.Log` calls to:
  - `handleDeviceEnroll` — `"device.enroll"` (line 117)
  - `handleRotateDeviceToken` — `"device.token_rotated"` (line 215)

### 6. File Name Sanitization (Spec §7)

- **File**: `relay/relay.go:52-55`
- `StageFile` now sanitizes uploaded filenames using `filepath.Base(filepath.Clean(fileName))`
- Fallback to `"upload"` if the result is `"."` or `"/"`

### 7. Device Tunnel Close on Token Rotation (Spec §3)

- **File**: `tunnel/hub.go:427-433` — new `Hub.CloseDevice(deviceID, code, reason)` method
- **File**: `httpapi/devices_handler.go:214-216` — `handleRotateDeviceToken` calls `deps.Hub.CloseDevice(deviceID, 4005, "token_rotated")`

### 8. Pre-existing Fixes

- **`store/interfaces.go:59`** — fixed `UpdateResult` parameter type from `*string` to `*json.RawMessage` (mismatched with implementation)
- **`pool/allocator.go`** — removed unused `encoding/json` import

## Key Design Decisions

- Security headers and CSRF middleware are applied globally (before routes), not per-route, since they apply to all responses
- Auth rate limiting is per-IP only (not per-account) since the body-parsing approach would require buffering; per-account brute-force protection relies on the limited per-IP budget
- Audit logging is best-effort (non-blocking) — `deps.Audit.Log` errors are logged but don't fail the request
- Token family is stored in DB alongside the token for efficient revocation queries (previously it was in-memory only with a broken SQL query)

## Known Gaps / TODOs

- [ ] HIBP k-anon password check (optional per spec §2)
- [ ] Device WSS certificate pinning (optional per spec §5)
- [ ] OS keystore for device token storage (`keyring`) — currently uses `0600` file only (spec §3)
- [ ] Frontend `TokenManager.ts` and `AuthGuard.tsx` are stubs with no real implementation (spec §2 transport requirements for HttpOnly cookie + in-memory access token)
- [ ] Postgres connection-level encryption (TLS) is not enforced in config validation (spec §8)
