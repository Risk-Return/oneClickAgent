# 08-auth-security — Implementation Audit

> Audited against: `docs/spec/08-auth-security.md`, `docs/spec/06-data-model.md`, `docs/spec/00-overview.md`, `docs/spec/01-architecture.md`
> Dev record: `docs/dev/08-auth-security.md`
> Date: 2026-06-03

## Summary

| Category | Count |
|----------|-------|
| Critical gaps | 1 |
| Significant gaps | 7 |
| Minor gaps | 3 |

---

## 1. Critical Gaps

### 1.1 Frontend auth stubs — no token storage or route guarding

- **File:** `web/src/auth/TokenManager.ts:1-3`, `web/src/auth/AuthGuard.tsx:1-2`
- **Severity:** Critical

The only two frontend auth files are literal stubs containing only comment descriptions. The spec requires the web frontend to store the **access token in memory** (never `localStorage`) and the **refresh token in an `HttpOnly`, `Secure`, `SameSite=Strict` cookie**. It also requires silent refresh and token theft detection on refresh reuse. Without this implementation, the entire browser-based authenticated flow is non-functional — users cannot log in from a browser, refresh tokens are exposed in `localStorage` or session memory, and route guards do not exist.

This gap is acknowledged in the dev record's known-gaps section but its severity is understated — it breaks the web auth flow entirely. Every authenticated browser request path depends on this working.

**Spec ref (spec §2):**
> "Transport: tokens only over HTTPS/WSS. Web stores refresh in HttpOnly, Secure, SameSite=Strict cookie; access token in memory (not localStorage)."

---

## 2. Significant Gaps

### 2.1 Logout revokes ALL refresh tokens, not just the presented one

- **File:** `gateway/internal/httpapi/auth_handler.go:238-249`
- **Severity:** Significant

The `handleLogout` handler calls `deps.Tokens.RevokeAllForUser(r.Context(), userID)` at line 241. This revokes every refresh token the user holds across all devices/sessions. The spec says to revoke only the **presented** token:

```go
func (deps *Dependencies) handleLogout() http.HandlerFunc {
    // No request body parsed — no refresh_token read from the request.
    // Revokes ALL tokens instead of just the one presented.
    go func() {
        userID := getUserID(r)
        deps.Tokens.RevokeAllForUser(r.Context(), userID)  // ← line 241
        ...
    }
}
```

The handler should read the refresh token from the request body, hash it, and revoke only that one token (and optionally its family for theft detection). The current behavior means a user logging out from one browser logs them out everywhere, which breaks multi-device session semantics.

**Spec ref (spec §2):**
> "Logout: revoke presented refresh token."

### 2.2 Admin actions lack audit logging

- **Files:**
  - `gateway/internal/httpapi/devices_handler.go:11-43` (handleCreateDevice)
  - `gateway/internal/httpapi/devices_handler.go:46-80` (handleUpdateDevice)
  - `gateway/internal/httpapi/devices_handler.go:141-162` (handleDeleteDevice)
  - `gateway/internal/httpapi/devices_handler.go:181-207` (handleSetPoolSize)
  - Skill handler files (create/install/disable/update/delete/visibility/grants)
  - `gateway/internal/httpapi/vnc_handler.go` (admin credential endpoints)
  - Org management handlers
  - User tier update handler
- **Severity:** Significant

The spec requires that **all** admin actions write to `audit_log`. Currently, audit logging exists only in:
- `auth_handler.go` — register, login, refresh, logout
- `devices_handler.go` — enroll, token rotation

**Not audited:** device creation, update, deletion; pool size changes; all skill vault/fleet ops (create, publish, install, disable, enable, update, delete, visibility, grants); organization create/update/delete/member add/remove; user tier changes; agent drain/release; credential management.

This means the audit log is incomplete — most administrative mutations leave no trace.

**Spec ref (spec §4.3):**
> "All admin skill/visibility/org/device/agent-pool actions are written to audit_log with actor=admin user_id."

**Spec ref (spec §8):**
> "Audit log for sensitive actions (login, token rotation, device/agent create/delete, job submit/cancel)."

### 2.3 Credential vault KMS envelope encryption is a stub

- **File:** `gateway/internal/credvault/vault.go:43, 48, 66-70, 81-88`
- **Severity:** Significant

The spec requires either direct key encryption (`IAGENT_CRED_KEY`, a base64 AES-256 key) or KMS envelope encryption (`IAGENT_CRED_KMS`). The config accepts both env vars (`config.go:49-50`), and the vault constructor accepts a `kmsKey` argument (`vault.go:48`) and stores it in the `kmsID` field:

```go
type Vault struct {
    dataKey []byte
    keyID   string
    kmsID   string   // ← stored but never used
}
```

The `IsConfigured()` method returns `true` when `kmsID != ""` even when `dataKey` is empty (`vault.go:69`). But `Encrypt()` and `Decrypt()` both check only `len(v.dataKey) == 0` and return `ErrKeyNotConfigured` if true (`vault.go:85-87, 129-131`). The KMS path is never invoked.

This means: if an operator configures `IAGENT_CRED_KMS` without `IAGENT_CRED_KEY`, the vault appears configured but all encrypt/decrypt operations fail. KMS envelope encryption — unwrapping a data key via KMS on startup — is entirely unimplemented.

**Spec ref (spec §13.2):**
> "Key management: the data key is provided via IAGENT_CRED_KEY (base64 AES-256) or, preferably, envelope-encrypted via KMS (IAGENT_CRED_KMS). Keys live outside the database; rotation tracked by key_id."

**Spec ref (spec §10):**
> "JWT signing key: gateway env / KMS, supports key rollover (kid)"

### 2.4 PostgreSQL connection TLS not enforced

- **File:** `gateway/internal/config/config.go:83-85`
- **Severity:** Significant

The config validates that `IAGENT_DB_URL` is non-empty but never checks that the connection string includes TLS parameters (`sslmode=require`, `sslmode=verify-full`, etc.). An insecure `DBURL` with no TLS passes validation silently:

```go
if c.DBURL == "" {
    errs = append(errs, "IAGENT_DB_URL is required")
}
// No TLS check — a plaintext connection string is accepted.
```

The spec requires encryption in transit for all connections. In production, a missing TLS enforcement would expose DB traffic (including password hashes, token hashes, and encrypted credential blobs) on the wire.

**Spec ref (spec §8):**
> "TLS in transit everywhere. At rest: encrypt DB volumes/disk; secrets in a manager (env/Vault), never in VCS."

### 2.5 No per-user job submission rate limiting

- **File:** `gateway/internal/httpapi/middleware.go:51-104, 315-368`
- **Severity:** Significant

Two rate limiters exist:
1. `rateLimitMiddleware` (line 51) — global per-IP-per-second, applied to all routes
2. `authRateLimitMiddleware` (line 316) — per-IP-per-minute, applied to auth endpoints only

Neither enforces per-user job submission rate limits. A malicious user could spam job submissions from multiple IPs or behind a NAT/proxy. The spec explicitly lists this as a required rate limit:

**Spec ref (spec §9):**
> "Rate limits: login/register per-IP+account; job submit per-user; WS subscribe per-connection."

### 2.6 No WS subscription rate limiting per-connection

- **File:** No implementation found — absent from `gateway/internal/httpapi/`
- **Severity:** Significant

The spec requires per-connection rate limiting on WebSocket subscriptions. No such limit is implemented. A malicious client could subscribe to many topics, exhausting server resources.

**Spec ref (spec §9):**
> "Rate limits: login/register per-IP+account; job submit per-user; WS subscribe per-connection."

### 2.7 Device token stored in SQLite, no OS keystore

- **File:** `device/iagent_device/store/repositories.py:14-18`; `device/iagent_device/config.py:55-56`
- **Severity:** Significant

The spec requires the device token to be stored in the OS keystore (`keyring`) when available, falling back to a `0600` file:

```python
# repositories.py:14
def save_device(self, device_id: str, token: str, gateway_url: str, name: str = ""):
    self.conn.execute(
        "INSERT OR REPLACE INTO device_info (device_id, name, token, gateway_url, enrolled_at) ...",
        (device_id, name, token, gateway_url, ...),
    )
```

The token is stored in plaintext in the `device_info` SQLite table. The `token_path` property exists on `config.py:55-56` but is never used for token storage. There is no `keyring` import or integration anywhere in the device codebase. On multi-user systems, any process with read access to the SQLite file can extract the device token.

**Spec ref (spec §3):**
> "Storage on device: OS keystore (keyring) when available; otherwise a 0600 file. Never logged."

---

## 3. Minor Gaps

### 3.1 HIBP k-anon password breach check not implemented (optional)

- **File:** Not present in `gateway/internal/auth/password.go`
- **Severity:** Minor

The spec describes this as optional: "block known-breached (optional HIBP k-anon)." The `ValidatePassword` function checks length and character mix but does not call any HIBP API. This is correctly listed as a known gap in the dev record.

**Spec ref (spec §2):**
> "Password policy: min length 12, block known-breached (optional HIBP k-anon), rate-limited attempts."

### 3.2 Device WSS certificate pinning not implemented (optional)

- **File:** Not present in `device/iagent_device/tunnel/client.py`
- **Severity:** Minor

The spec describes this as optional: "pinning optional in hardened mode." The tunnel client opens a WSS connection with no certificate pinning or fingerprint verification. This is correctly listed as a known gap in the dev record.

**Spec ref (spec §5):**
> "Device verifies gateway certificate (pinning optional in hardened mode)."

### 3.3 credvault.CredentialStore interface uses `interface{}` context

- **File:** `gateway/internal/credvault/vault.go:174-179`
- **Severity:** Minor

The `CredentialStore` interface defined in `credvault/vault.go` uses `ctx interface{}` for all methods:

```go
type CredentialStore interface {
    CreateCredential(ctx interface{}, cred *model.BrowserCredential) error
    GetCredential(ctx interface{}, id model.UUID) (*model.BrowserCredential, error)
    ListByUser(ctx interface{}, userID model.UUID) ([]model.BrowserCredential, error)
    UpdateCredential(ctx interface{}, cred *model.BrowserCredential) error
    DeleteCredential(ctx interface{}, id model.UUID) error
}
```

This violates Go conventions (context should be `context.Context`). The concrete `store.CredentialStore` uses `context.Context`. The interface exists but is never satisfied by the concrete type due to the type mismatch. The vault package does not use this interface internally (its `Encrypt`/`Decrypt` methods operate directly on `[]byte`, not through the store interface), so this is purely a code-quality issue with no runtime impact.

**Spec ref (spec §11):**
> General data protection standard; interface hygiene is implied by the Go conventions used throughout the project.

---

## 4. What's Solidly Implemented

| # | Feature | File(s) | Spec § |
|---|---------|---------|--------|
| 1 | Argon2id password hashing with `DefaultParams` (64 MiB memory) | `auth/password.go:14-17` | §2 |
| 2 | Password validation (≥12 chars, upper, lower, digit, special) | `auth/password.go:36-67` | §2 |
| 3 | JWT access tokens: short-lived (15m), HS256, rotating keys (kid) | `auth/jwt.go:25-159` | §2 |
| 4 | Refresh tokens: cryptographically random 64-byte, SHA-256 hashed for DB | `auth/jwt.go:136-148` | §2 |
| 5 | Rotating refresh with theft detection — `RevokeFamily` on reuse | `auth_handler.go:184-186`, `store/tokens.go:43-46` | §2 |
| 6 | Token family column in DB with migration | `migrations/004_add_token_family.up.sql` | §2 |
| 7 | RBAC: `RequireAdmin`, `RequireOwner`, `CanAccessAgent`, `CanManageDevice`, `CanManageSkills`, `CanManageOrgs`, `CanSetUserTier`, `CanSubmitJob` | `auth/rbac.go:17-96` | §4 |
| 8 | Tenant scope middleware — job/file ownership enforcement | `middleware.go:181-232` | §4.2 |
| 9 | Admin-only route groups with `requireAdminMiddleware` | `router.go:96-105, 113-120, 151-189` | §4.1 |
| 10 | device_token hash-only storage (SHA-256) | `devices_handler.go:106, 240-242`, `tunnel_handler.go:43` | §3 |
| 11 | Device enrollment with one-time enrollment code | `devices_handler.go:82-121` | §3 |
| 12 | Token rotation + tunnel close on rotation (code 4005) | `devices_handler.go:209-237`, `tunnel/hub.go:428-434` | §3 |
| 13 | Tunnel auth — hash device token, look up device before WS upgrade | `tunnel_handler.go:25-86` | §5 |
| 14 | Frame validation — size cap 1 MiB, version check, close code 4004 | `tunnel/device_conn.go:128-143`, `tunnel/codec.go:44-55` | §5 |
| 15 | Security headers: HSTS, CSP, X-Content-Type-Options, X-Frame-Options, Referrer-Policy, COOP | `middleware.go:252-263` | §12 |
| 16 | CSRF protection — Origin/Referer validation on state-changing methods | `middleware.go:268-311` | §9 |
| 17 | Auth rate limiting — per-IP-per-minute on login/register/refresh | `middleware.go:316-368` | §9 |
| 18 | AES-256-GCM credential encryption with unique random nonce + SHA-256 integrity | `credvault/vault.go:81-160` | §13.2 |
| 19 | Credential tenant isolation — ownership check on get/delete/update | `vnc_handler.go:158-161, 185-188, 208-211` | §13.2 |
| 20 | File name sanitization | `relay/relay.go:57-60` | §7 |
| 21 | Audit log store with `Log` and `List` operations | `store/audit.go:15-69` | §8 |
| 22 | Refresh token `family` column for theft detection | `store/tokens.go:19-21`, `store/tokens.go:43-46` | §2 |
| 23 | CORS middleware with configurable origins + `AllowCredentials` | `router.go:206-218` | §9 |
| 24 | `CredentialStore.LinkToJob` enforces cross-tenant check via SQL JOIN | `store/credentials.go:90-106` | §11 |

---

## 5. Fix Priority

| Priority | Gap | Impact |
|----------|-----|--------|
| **P0** | Frontend TokenManager + AuthGuard stubs (1.1) | Browser auth flow completely non-functional |
| **P1** | Logout revokes all tokens, not just presented (2.1) | Multi-device logout semantics broken |
| **P1** | Admin actions lack audit logging (2.2) | No audit trail for fleet/skill/org/admin mutations |
| **P2** | KMS envelope encryption stub (2.3) | Cannot deploy with KMS-only credential vault config |
| **P2** | PostgreSQL TLS not enforced in config (2.4) | DB traffic in plaintext in production |
| **P3** | No per-user job submit rate limit (2.5) | Abuse vector: job spam across IPs |
| **P3** | No WS subscription rate limit (2.6) | Abuse vector: subscription exhaustion |
| **P3** | Device token in SQLite, no keyring (2.7) | Token extractable by local processes |
| **P4** | HIBP k-anon check (3.1) | Optional: known-breach password blocking |
| **P4** | WSS certificate pinning (3.2) | Optional: hardened mode only |
| **P4** | credvault.CredentialStore interface type (3.3) | Code quality, no functional impact |

