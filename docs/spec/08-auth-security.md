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

- **`admin` (operator)** — manages the **device fleet + agent pool** (enroll, rotate, decommission, set pool size, drain/release agents), manages **user tiers** (free/pro/enterprise for queue priority), owns the **entire skill lifecycle** (vault, fleet install/disable/update/delete across **all** devices), and controls **skill visibility** (which customers can see which skills). Admins do **not** submit customer jobs on others' behalf.
- **`user` (customer)** — owns **jobs/files**; submits commands; receives results; selects visible skills for jobs. A customer **does not own or even see devices or the agent pool**, and cannot manage skills beyond selecting from those made visible to them. Agents are transparently allocated per job and released on completion. The customer's `tier` affects job queue priority.

### 4.2 Ownership & tenant isolation

- **Customer-owned resources** (`jobs`, `files`): every route runs `tenant_scope` → assert `resource.user_id == jwt.sub`. A customer can only access their own jobs/files.
- **Admin-owned resources** (`devices`, `agents` (pool), vault `skills`, `skill_versions`, `device_skills`, `skill_grants`): require `role=admin`. Customers receive `403` (devices and the agent pool are not even enumerable to them).
- **Agents are pooled** — they do not belong to any customer. On job submit, the allocator sets `agent.user_id = job.user_id` (temporary). On job completion, it clears it. Tunnel routing validates `job.user_id == jwt.sub`, not `agent.user_id`.
- **Customer sees only allocated agents**: agents appear in the customer's view only while `agent.user_id == jwt.sub` (i.e., while their job is running).
- **Inbound device frames** are attributed to the device's pooled agents; results route to the owning customer via `job.user_id`.
- **WS subscriptions**: customers may subscribe only to their own `job` topics; device-fleet and `skill.status` rollout topics are admin-only.
- **VNC sessions** (`vnc_sessions`) and **saved logins** (`browser_credentials`) are **customer-owned**: every VNC/credential route asserts `resource.user_id == jwt.sub`. A customer may only open a VNC session for their own running job and may only inject their own credentials.

### 4.3 Skill authorization & visibility

- **Vault management**, **fleet ops** (install/disable/update/delete across all devices), **visibility** (`public`/`restricted` + grants), and **organization/membership** management require `role=admin`.
- **Visibility resolution** — a skill is visible to a customer iff:
  `skills.visibility = 'public'` **OR** a `skill_grants` row with `(principal_type='user', principal_id=jwt.sub)` **OR** a `skill_grants` row with `(principal_type='org', principal_id=user.org_id)` (the caller's organization). Members of a granted org all inherit visibility.
- **Customer skill selection per job**: when submitting a job with a skill_id, the gateway verifies:
  1. the skill is **visible** to the caller (per resolution above),
  2. the skill is **installed** (not `disabled`/`deleting`) on the device hosting the allocated agent,
  3. the skill is **enabled** on the allocated agent (reported by `agent_skills`).
- **One-skill-per-job**: a job submit carries **at most one** `skill_id`. The gateway rejects arrays / more than one (`422`), and verifies the chosen skill is `enabled` on the target agent before dispatch (`422 SKILL_NOT_ENABLED`).
- The customer-facing skill list returns **only visible skills**; non-visible skills are never disclosed (no existence leak).
- All admin skill/visibility/org/device/agent-pool actions are written to `audit_log` with `actor=admin user_id`.
- Skill artifacts are validated by `sha256` end-to-end (vault → device → agent); the agent never receives gateway/user credentials.
- Agent allocation and deallocation is NOT an auditable customer action — it is an internal platform operation transparent to the customer.

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
| Browser/VNC | RFB server binds **loopback only**, never published to host; per-session random RFB password; VNC stack runs only during a session |
| Credentials | injected storage-state confined to `/work/profile`, wiped on job terminal; never logged or in `/status` |

The agent never receives the user's JWT, the device_token, or the credential-vault key. It only knows its job context.

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
| Stolen saved cookies (DB dump) | AES-256-GCM at rest; key in KMS/env, not in DB; plaintext never stored |
| VNC session hijack | JWT + session ownership on browser side; single-use `session_token` on device side; per-session RFB password; short TTL |
| Credential exfiltration via agent | agent never sees the vault key/owner; storage-state confined to `/work/profile`, wiped on terminal; not in logs/`/status` |
| Cross-tenant credential reuse | a job may reference only the caller's own `credential_ids` (else `403`) |

## 12. Compliance & Hygiene

- Dependency scanning (Go `govulncheck`, Python `pip-audit`), image scanning (Trivy) in CI.
- Least-privilege deploy; gateway runs as non-root; minimal base images.
- Security headers on web responses (HSTS, CSP, X-Content-Type-Options, etc.).

## 13. Browser Sessions (VNC) & Credential Vault

The Docker/browser feature has two security-sensitive halves: live **VNC sessions** and the **encrypted login-cookie vault**.

### 13.1 VNC session security

Interactive browser (VNC) sessions are relayed gateway↔device↔container with **no inbound device port** (`05-tunnel-protocol §9`).

- **Two-sided auth**: the browser side of the relay (`/ws/vnc/{id}`) requires the user's JWT **and** ownership of `vnc_sessions.user_id`; the device side (`/session/{id}`) requires the single-use `session_token` (hash stored, TTL 60s to connect, bound to `session_id`+`device_id`+`user_id`).
- **RFB auth**: x11vnc enforces a **per-session random password** generated at `/vnc/start`; it is delivered to the noVNC client only (via the trusted control tunnel → gateway → browser), never exposed in URLs or logs.
- **Loopback only**: the container RFB port is never published to the host; only the device's in-process bridge connects to it.
- **Bounded lifetime**: sessions auto-close on job terminal, idle (`IAGENT_VNC_IDLE_TTL`), or max duration (`IAGENT_VNC_MAX_TTL`); per-user concurrency capped.
- **Transparent relay**: the gateway never parses or stores RFB bytes; only session metadata is persisted.

### 13.2 Credential vault & login-cookie protection

Saved website logins (cookies + localStorage = **storage-state**) are stored **encrypted** in the cloud (`06-data-model §1.16`) and re-injected into a container's browser per job (`05-tunnel-protocol §10`).

- **Encryption at rest**: `AES-256-GCM` with a unique 96-bit nonce per write; ciphertext + nonce + auth tag + `key_id` persisted. Plaintext is **never** written to disk or DB.
- **Key management**: the data key is provided via `IAGENT_CRED_KEY` (base64 AES-256) or, preferably, envelope-encrypted via KMS (`IAGENT_CRED_KMS`). Keys live outside the database; rotation tracked by `key_id`. The gateway is the **sole** key holder — neither device nor agent ever receives it.
- **Capture path**: storage-state is exported from the live browser only during an authenticated VNC session and relayed device→gateway (`CRED_CAPTURE`); it never transits the browser client. Integrity checked via `sha256` of plaintext before encryption.
- **Inject path**: at dispatch the gateway decrypts in memory, verifies `sha256`, and pushes over the tunnel (`CRED_PUSH`); the device streams it straight to the agent. Plaintext exists only transiently in memory on gateway and device; the agent confines it to `/work/profile` and wipes it on job terminal.
- **Tenant isolation**: credentials are customer-owned; only the owner may list/rename/delete or reference them in a job.
- **No logging**: storage-state and RFB bytes are excluded from all logs, traces, `/status`, and audit `meta` (only the credential `id`/`label`/`origin` are auditable).
