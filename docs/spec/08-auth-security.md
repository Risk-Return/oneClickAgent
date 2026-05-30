# 08 — Authentication & Security

Security model for the whole system. The **gateway is the only public surface**; everything else is reachable only via the device-initiated tunnel or loopback.

## 1. Trust Boundaries

```
Untrusted internet ── TLS ──► [Gateway]  (authn/authz, tenant scope)
                                  │ device_token over WSS (device dials out)
                                  ▼
                              [Device]  (private network, no inbound ports)
                                  │ loopback HTTP
                                  ▼
                              [Agent]   (isolated container, no public access)
```

## 2. User Authentication

- **Registration**: `email` + `username` + `password`. Passwords hashed with **Argon2id** (memory ≥ 64MB, iterations tuned to ~250ms). Never store plaintext.
- **Password policy**: min length 12, block known-breached (optional HIBP k-anon), rate-limited attempts.
- **JWT**:
  - Access token: short-lived (15m), signed (HS256 with rotating secret, or RS256). Claims: `sub=user_id`, `role`, `exp`, `iat`, `jti`.
  - Refresh token: 30d, **rotating** — each refresh issues a new refresh and revokes the old (stored as hash in `refresh_tokens`). Reuse of a revoked refresh → revoke entire family (token theft detection).
- **Logout**: revoke presented refresh token.
- **Transport**: tokens only over HTTPS/WSS. Web stores refresh in `HttpOnly`, `Secure`, `SameSite=Strict` cookie; access token in memory (not localStorage).

## 3. Device Authentication

- **Enrollment**: an **admin** registers a device → gateway returns a **one-time `enrollment_code`** (short TTL, single use). The device exchanges it via `POST /devices/enroll` for a long-lived `device_token`. (Customers are never involved in enrollment.)
- **device_token**: high-entropy opaque secret; gateway stores **only its hash** (`token_hash`). Presented as `Authorization: Bearer` on the WSS upgrade.
- **Rotation/Revocation**: `POST /devices/{id}/rotate-token` issues a new token and invalidates the old; revoked tokens close the live tunnel with `4005`.
- **Storage on device**: OS keystore (`keyring`) when available; otherwise a `0600` file. Never logged.

## 4. Roles, Ownership & Authorization

### 4.1 Roles

- **`admin` (operator)** — manages the **device fleet** (enroll, rotate, decommission), owns the **entire skill lifecycle** (vault, fleet install/disable/update/delete across **all** devices), and controls **skill visibility** (which customers can see which skills). Admins do **not** submit customer jobs on others' behalf.
- **`user` (customer)** — owns **agents/jobs/files**; submits commands; receives results; **selects which visible skills to use**. A customer **does not own or even see devices**, and cannot manage skills beyond selecting from those made visible to them.

### 4.2 Ownership & tenant isolation

- **Customer-owned resources** (`agents`, `jobs`, `files`): every route runs `tenant_scope` → assert `resource.user_id == jwt.sub`. A customer can only access their own agents/jobs/files.
- **Admin-owned resources** (`devices`, vault `skills`, `skill_versions`, `device_skills`, `skill_grants`): require `role=admin`. Customers receive `403` (and devices are not even enumerable to them).
- **Agent placement** is platform-controlled; the customer never references a `device_id`. The gateway resolves `agent → device` internally for routing and validates `agent.user_id == jwt.sub` before emitting any tunnel frame.
- **Inbound device frames** are attributed to the device's hosted agents; results route to the owning customer only.
- **WS subscriptions**: customers may subscribe only to their own `job/agent` topics; device-fleet and `skill.status` rollout topics are admin-only.

### 4.3 Skill authorization & visibility

- **Vault management**, **fleet ops** (install/disable/update/delete across all devices), **visibility** (`public`/`restricted` + grants), and **organization/membership** management require `role=admin`.
- **Visibility resolution** — a skill is visible to a customer iff:
  `skills.visibility = 'public'` **OR** a `skill_grants` row with `(principal_type='user', principal_id=jwt.sub)` **OR** a `skill_grants` row with `(principal_type='org', principal_id=user.org_id)` (the caller's organization). Members of a granted org all inherit visibility.
- **Customer selection** (enable/disable a skill on the customer's own agent) requires all of:
  1. the caller owns the agent (`agent.user_id == jwt.sub`),
  2. the skill is **visible** to the caller (per resolution above),
  3. the skill is **installed** (not `disabled`/`deleting`) on the device hosting the agent.
- **One-skill-per-job**: a job submit carries **at most one** `skill_id`. The gateway rejects arrays / more than one (`422`), and verifies the chosen skill is `enabled` on the target agent before dispatch (`422 SKILL_NOT_ENABLED`).
- The customer-facing skill list returns **only visible skills**; non-visible skills are never disclosed (no existence leak).
- All admin skill/visibility/org/device actions are written to `audit_log` with `actor=admin user_id`.
- Skill artifacts are validated by `sha256` end-to-end (vault → device → agent); the agent never receives gateway/user credentials.

## 5. Tunnel Security

- **WSS only** (TLS). Device verifies gateway certificate (pinning optional in hardened mode).
- Auth on upgrade; unauthenticated upgrades closed with `4001`.
- Frame validation: schema + size cap (1 MiB) → violations close with `4004`.
- Rate limiting / overload → `4290` with backoff guidance.
- At-least-once + idempotency prevents replay-induced double execution (handlers idempotent by `job_id`+`event_seq`).

## 6. Agent Container Hardening

| Control | Setting |
|---------|---------|
| User | non-root (`USER app`) |
| Capabilities | `cap_drop: ALL`, add only if strictly required |
| Network | bridge/internal only; **no host network**; no inbound from internet |
| Filesystem | `read_only` root + tmpfs `/tmp` + dedicated `/work` volume |
| Resources | `cpu=2, mem=4g, disk=10g`, `pids_limit` set |
| Secrets | injected at create time (env/secret mount), excluded from logs and `/status` |
| Data | user files wiped on job terminal state; nothing persisted across jobs |

The agent never receives the user's JWT or the device_token. It only knows its job context.

## 7. File Security

- Uploads scanned for size/type limits at the gateway; `sha256` integrity end-to-end.
- Files transit gateway → device only; mounted **read-only** into the agent.
- Strict lifecycle deletion (see `06-data-model §4`); cloud staging purged after delivery + grace window.
- No path traversal: workspace paths derived from server-generated IDs only; uploaded filenames sanitized.

## 8. Data Protection

- TLS in transit everywhere. At rest: encrypt DB volumes/disk; secrets in a manager (env/Vault), never in VCS.
- PII minimized: store only email/username/hash. No raw agent logs containing user data are persisted by the gateway.
- Audit log for sensitive actions (login, token rotation, device/agent create/delete, job submit/cancel).

## 9. Input Validation & Abuse Prevention

- DTO validation at gateway boundary (types, lengths, enums).
- Rate limits: login/register per-IP+account; job submit per-user; WS subscribe per-connection.
- CORS locked to the web origin(s). CSRF: cookie-based refresh uses `SameSite=Strict` + double-submit/Origin checks on state-changing requests.
- Command injection: agent `command`/`params` are data, never shell-interpolated by gateway/device; the agent brain decides safe handling.

## 10. Secrets Management

| Secret | Where | Rotation |
|--------|-------|----------|
| JWT signing key | gateway env / KMS | supports key rollover (kid) |
| `device_token` | device keystore + cloud hash | on demand via API |
| DB credentials | gateway env / secret manager | per ops policy |
| LLM/API keys for agents | injected to container at create | per ops policy |

## 11. Threat Model (summary)

| Threat | Mitigation |
|--------|------------|
| Stolen access token | short TTL; in-memory only |
| Stolen refresh token | rotation + reuse detection → family revoke |
| Compromised device_token | hash-only storage; rotation/revocation; per-device scope |
| Cross-tenant access | mandatory tenant_scope on every route + frame |
| Malicious uploaded file | size/type limits, read-only mount, ephemeral, sandboxed container |
| Agent escape attempt | dropped caps, non-root, no host net, read-only fs, resource limits |
| Replay of tunnel frames | idempotent handlers, msg_id dedupe |
| DoS on gateway | rate limits, connection caps, backpressure, `4290` |

## 12. Compliance & Hygiene

- Dependency scanning (Go `govulncheck`, Python `pip-audit`), image scanning (Trivy) in CI.
- Least-privilege deploy; gateway runs as non-root; minimal base images.
- Security headers on web responses (HSTS, CSP, X-Content-Type-Options, etc.).
