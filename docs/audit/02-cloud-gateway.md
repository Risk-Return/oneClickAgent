# 02-cloud-gateway — Implementation Audit

> **Audited against:** `docs/spec/02-cloud-gateway.md`, `docs/spec/00-overview.md`, `docs/spec/01-architecture.md`
> **Dev record:** `docs/dev/02-cloud-gateway.md`
> **Date:** 2026-06-01

## Summary

| Category | Count |
|----------|-------|
| Critical gaps (flow-breaking) | 4 |
| Significant gaps (incomplete feature) | 4 |
| Minor gaps (nice-to-have) | 3 |

---

## 1. Critical Gaps (flow-breaking)

### 1.1 Device token hashing is a placeholder

**File:** `gateway/internal/httpapi/devices_handler.go:155-157`
**Severity:** Critical — device auth is effectively broken

`hashTokenForStorage` returns `model.NewUUID().String()` instead of actually hashing the token. A comment says *"actual hashing would use auth.HashToken"* but real hashing was never implemented. Device tokens are stored as random UUIDs, not as cryptographic hashes of the actual token.

**Spec ref:** `02 §6` — "mint `device_token` (store only hash)"

### 1.2 No inbound handler for VNC_OPENED

**File:** `gateway/internal/tunnel/device_conn.go`
**Severity:** Critical — VNC sessions stay PENDING forever

The device's `VNC_OPENED` response (which carries the RFB password) is received by the tunnel read pump but has no case in `handleFrame`. The `vncrelay.MarkReady()` method exists but is never auto-called from the device response. VNC sessions remain stuck in `PENDING` state.

**Also missing:** No outbound `VNC_CLOSE` frame is sent to the device when the API closes a VNC session.

**Spec ref:** `02 §16` — "Device replies VNC_OPENED {status:ready, rfb_password}; gateway stores status=ready"

### 1.3 CRED_PUSH has no API route or handler

**File:** `gateway/internal/credvault/`, `gateway/internal/httpapi/`
**Severity:** Critical — credentials cannot be injected into agents for job reuse

`FrameCredPush` and `CredPushPayload` are defined in the model but no HTTP endpoint or handler sends credentials from the gateway to the device. Credentials can be *captured* (`POST /vnc/{id}/save-login` → `FrameCredCapture`) but there is no path to *inject* them back into an agent at job dispatch time.

**Spec ref:** `02 §17` — "Inject (on job submit with credential_ids): for each owned credential, decrypt in memory → verify sha256 → CRED_PUSH over the tunnel"

### 1.4 No inbound handlers for CRED_CAPTURE_ACK / CRED_PUSH_ACK

**File:** `gateway/internal/tunnel/device_conn.go`
**Severity:** Critical — credential operations lack completion confirmation

The device's acknowledgements for credential push and capture operations are received but silently dropped. The gateway has no way to confirm whether a credential was successfully injected or captured.

**Spec ref:** `02 §17` — "CRED_PUSH_ACK" and "CRED_CAPTURE_ACK" in the wire protocol

---

## 2. Significant Gaps (incomplete feature)

### 2.1 DispatchToAllDevices is a stub

**File:** `gateway/internal/skillvault/dispatch.go:252-260`
**Severity:** Significant — fleet-wide skill operations don't work

`DispatchToAllDevices` only logs and returns. It does not iterate over online devices, does not send `SKILL_DISPATCH_*` frames, and does not update `device_skills` state.

**Also:** Fleet `disable`, `update`, and `delete-fleet` HTTP handlers return `"not yet implemented"` (`skills_handler.go:207-223`).

**Spec ref:** `02 §9` — "an admin install/update/disable/delete targets all devices"

### 2.2 No tenant_scope middleware

**File:** `gateway/internal/httpapi/router.go`
**Severity:** Significant — tenant isolation is not enforced at the middleware layer

The spec describes a middleware chain of `request_id → recover → cors → rate_limit → auth(jwt) → tenant_scope → handler`, and notes *"loads the resource and asserts ownership before the handler runs"*. There is no `tenant_scope` middleware; tenant isolation must be done manually in each handler (if at all).

**Spec ref:** `02 §5` — "tenant_scope loads the resource and asserts ownership before the handler runs"

### 2.3 No JWT key rotation (kid)

**File:** `gateway/internal/auth/jwt.go`
**Severity:** Significant — key rollover is impossible without redeployment

The JWT manager uses a single static `secret` field. No `kid` header claim is emitted, and no key registry exists. The credential vault supports `key_id` for rotation but the auth system does not.

**Spec ref:** `02 §12` — implies key rotation capability for cloud deployments

### 2.4 Minimal password complexity

**File:** `gateway/internal/auth/password.go:35-39`
**Severity:** Significant — weak passwords are accepted

Only `len >= 12` is checked. No requirements for uppercase, lowercase, digits, or special characters. The spec does not define explicit complexity rules, but 12-char minimum without character-class requirements allows trivially weak passwords (e.g., `password1234`).

**Spec ref:** `00 §3` — "safe gateway: the only public surface; authenticates users, authorizes actions, isolates tenants"

---

## 3. Minor Gaps (nice-to-have)

### 3.1 Channel adapter interface has no concrete implementations

**File:** `gateway/internal/channel/`
**Severity:** Minor — architectural debt, web channel works via direct HTTP handlers

The `channel.Adapter` interface is defined but only `StubAdapter` exists. The web channel is handled directly in HTTP handlers rather than through the adapter interface. Feishu/QQ stubs return `NOT_IMPLEMENTED` as intended, but there is no `WebAdapter` that conforms to the interface.

**Spec ref:** `02 §11` — "Web adapter implemented; Feishu/QQ registered as no-op stubs"

### 3.2 KMS envelope encryption is a stub

**File:** `gateway/internal/credvault/vault.go:41`
**Severity:** Minor — single-key mode works

The KMS envelope encryption path is acknowledged with a `// stub:` comment. Only `IAGENT_CRED_KEY` (direct AES-256-GCM with data key) is functional. `IAGENT_CRED_KMS` is parsed from config but not wired to a real KMS client.

**Spec ref:** `02 §17` — "preferably, envelope-encrypted via IAGENT_CRED_KMS (KMS-managed; supports rotation by key_id)"

### 3.3 HMAC-SHA256 (HS256) for JWT signing

**Severity:** Minor — functional but suboptimal for cloud

Symmetric HS256 is used. For multi-instance cloud deployments, asymmetric signing (RS256/EdDSA) would allow any instance to verify tokens without sharing the signing secret. This is a deployment hardening concern, not a functional bug.

---

## 4. What's Solidly Implemented

| Package | Status |
|---------|--------|
| **Model** | All domain types, DTOs, frame types, state machines |
| **Config** | All 30+ env vars from spec, env/file loading, validation |
| **Auth — JWT** | Issue/verify, access 15m + refresh 30d rotating, family-based theft detection |
| **Auth — Passwords** | Argon2id via `alexedwards/argon2id` with `DefaultParams` |
| **Auth — RBAC** | Full helper suite: `IsAdmin`, `IsOwner`, `CanAccessAgent`, `CanManageDevice`, etc. |
| **Store** | PostgreSQL repos for all 15 tables, pgx/v5 + pgxpool |
| **Tunnel Hub** | Read/write pumps, ack tracking + retry, liveness (45s miss), supersede (4002), Registry interface with InMemory + SimulatedRedis impls |
| **Pool Allocator** | Tiered FIFO (`enterprise>pro>free`), queue TTL, per-user cap, wake-up on release, `AGENT_CREATE` dispatch |
| **File Relay** | 256KiB chunks, ≤8 in-flight backpressure, SHA-256, BEGIN/CHUNK/END/ACK flow, staging, retention cleanup |
| **Skill Vault** | Catalog CRUD, version management (SHA-256), visibility resolution (public + user grants + org grants), `SKILL_SYNC`, `SkillState` tracking |
| **VNC Relay** | Session pairing, bidirectional byte relay with buffer cap, idle/max TTL reaper, per-user concurrency cap, single-use session token |
| **Credential Vault** | AES-256-GCM encrypt/decrypt with random nonce, SHA-256 verification, key_id, capture flow (save-login) |
| **PubSub** | In-process topic broker with `job:{id}`, `agent:{id}`, `device:{id}` topics |
| **HTTP API** | Full route table (45+ endpoints), auth middleware, CORS, rate limiting, structured logging |
| **Observability** | `slog` structured logging, Prometheus `/metrics`, OTEL tracing stub |
| **Migrations** | 2 migrations (initial schema + docker/vnc/creds), up + down scripts |

---

## 5. Fix Priority

| Priority | Gap | Impact |
|----------|-----|--------|
| P0 | Device token hashing placeholder | Device tunnel auth broken |
| P0 | `VNC_OPENED` inbound + `VNC_CLOSE` outbound | VNC sessions non-functional |
| P0 | `CRED_PUSH` API route + handler | Credential injection for jobs broken |
| P0 | `CRED_CAPTURE_ACK`/`CRED_PUSH_ACK` inbound handlers | No confirmation of credential ops |
| P1 | `DispatchToAllDevices` + fleet disable/update/delete | Admin fleet skill management broken |
| P1 | `tenant_scope` middleware | Tenant isolation not enforced |
| P2 | Password complexity requirements | Security hardening |
| P2 | JWT `kid` key rotation | Security hardening |
| P3 | Concrete channel adapters | Architectural debt |
| P3 | KMS envelope encryption | Security hardening |
