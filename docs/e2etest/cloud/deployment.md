# Cloud Deployment Experience

Deploying IAgent cloud stack (gateway + web UI) on deepwitai.cn.

## Stack

| Component | Port | Tech |
|-----------|------|------|
| Gateway | 42080 | Go binary, run via setsid |
| Web UI | nginx static | Vite build served by nginx |
| PostgreSQL | 5432 | Local instance, DB `iagent` |
| Nginx | 80/443 | Existing AIBI config extended |

## Deployment Steps

```bash
# 1. Database setup
su - postgres -c "psql -c \"CREATE DATABASE iagent\""
su - postgres -c "psql -d iagent -c \"CREATE USER iagent WITH PASSWORD '...'\""
su - postgres -c "psql -d iagent -c \"GRANT ALL PRIVILEGES ON DATABASE iagent TO iagent\""
su - postgres -c "psql -d iagent -c \"GRANT ALL ON SCHEMA public TO iagent\""

# 2. Run migrations
for f in gateway/migrations/*.up.sql; do
  psql "postgresql://iagent:...@localhost:5432/iagent?sslmode=disable" -f "$f"
done

# 3. Build and start gateway
cd gateway && go build -o bin/gateway ./cmd/gateway
setsid env IAGENT_DB_URL="postgresql://iagent:...@localhost:5432/iagent?sslmode=disable" \
  IAGENT_JWT_SECRET="dev-jwt-secret-at-least-32-characters-long!!" \
  IAGENT_FILE_STORE="local:/tmp/iagent-files" \
  IAGENT_HTTP_ADDR=":42080" \
  IAGENT_ENV="development" IAGENT_LOG_LEVEL="debug" IAGENT_LOG_FORMAT="text" \
  IAGENT_CORS_ORIGINS="*" \
  IAGENT_CRED_KEY="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" \
  ./bin/gateway > /tmp/gateway.log 2>&1 &

# 4. Build web UI
cd web && VITE_API_PREFIX=/aiproduct VITE_BASE=/aiproduct/ npx vite build
```

## Issues Encountered & Resolved

### 1. Port conflicts (8080, 5173 taken)
Ports 8080 and 5173 were in use by other projects. Moved gateway to **42080**, Vite dev to **45173**. When building for production, Vite dev is not needed — nginx serves static files directly.

### 2. REST API prefix not baked into production build
Relying on shell `export VITE_API_PREFIX=/aiproduct` before `vite build` did not embed the value into `import.meta.env.VITE_API_PREFIX`. The built JS had only bare `/api/v1/...` paths.

**Fix**: Added `define` block in `vite.config.ts`:
```ts
define: {
  'import.meta.env.VITE_API_PREFIX': JSON.stringify(process.env.VITE_API_PREFIX || ''),
  'import.meta.env.VITE_BASE': JSON.stringify(process.env.VITE_BASE || '/'),
},
```
Shell env vars MUST be passed on the build command line. Then rebuild.

### 3. Hardcoded auth paths in TokenManager.ts
`web/src/auth/TokenManager.ts` had 4 hardcoded `/api/v1/auth/...` paths (login, register, refresh, logout) that bypassed the `API_PREFIX` variable used elsewhere. These calls hit `deepwitai.cn/api/v1/auth/login` instead of `deepwitai.cn/aiproduct/api/v1/auth/login`, causing 502 from the wrong backend.

**Fix**: Added `const PREFIX = import.meta.env.VITE_API_PREFIX || ''` and prefixed all 4 fetch URLs.

### 4. React Router `basename` needed
When served under `/aiproduct/`, React Router `createBrowserRouter` needs `{ basename: '/aiproduct' }`, otherwise routes resolve from root and `navigate('/login')` goes to the wrong URL.

**Fix**: Added `const BASENAME = import.meta.env.VITE_BASE || '/'` and passed it to `createBrowserRouter(routes, { basename: BASENAME })`.

### 5. `window.location.href` bypasses React Router basename
`SettingsPage.tsx` used `window.location.href = "/login"` for logout redirect — this ignores the basename.

**Fix**: Changed to `useNavigate()` + `navigate("/login")`.

### 6. `@novnc/novnc` import fails in production build
Import `@novnc/novnc/core/rfb` broke because `@novnc/novnc` `package.json` `exports` is a single string `"./core/rfb.js"` with no subpath exports. The dev server resolved it lazily, but rollup (production) strictly enforces the exports field.

**Fix**: Changed import to `import RFB from "@novnc/novnc"`.

### 7. noVNC uses top-level await (ES2022 feature)
`@novnc/novnc` contains `await` at module top-level. Vite's default build target `es2020` rejects this.

**Fix**: Set `build.target: 'es2022'` in `vite.config.ts`.

### 8. Restrictive CSP header blocks WebSocket connections
The gateway middleware set `Content-Security-Policy` with restricted `connect-src` that blocked WebSocket connections from noVNC and the frontend WS client.

**Fix**: Removed the CSP header entirely for development (securityHeadersMiddleware no longer sets CSP). Production should add a permissive CSP or handle it at the reverse proxy level.

### 9. nginx `alias` + `try_files` interaction
With `alias /path/to/dist/` and `try_files $uri $uri/ /aiproduct/index.html`, nginx tried looking up the aliased path with the full URI prefix, which never matched. The `$uri/` fallback with trailing slash produced double-slashed paths.

**Fix**: Simplified to `try_files $uri /aiproduct/index.html` — nginx aliases the `$uri` path correctly, and the fallback `/aiproduct/index.html` maps to `.../dist/index.html` through the alias.

### 10. Missing `/aiproduct` → `/aiproduct/` redirect
Requesting `https://deepwitai.cn/aiproduct` (no trailing slash) did not match the prefix location `location /aiproduct/` and fell through to the catch-all `location /`, returning 404.

**Fix**: Added exact-match redirect in both HTTP and HTTPS server blocks:
```nginx
location = /aiproduct { return 301 /aiproduct/; }
```

### 11. SPA location needed in BOTH HTTP and HTTPS server blocks
Initially only added the `/aiproduct/` location to the HTTPS block. The HTTP block was missing it.

**Fix**: Added identical blocks to both `server { listen 80 }` and `server { listen 443 ssl }` sections.

### 12. Password validation rules
Gateway rejects passwords that don't meet: ≥12 characters, ≥1 uppercase letter, ≥1 special character. Test accounts must comply.

## Nginx Configuration (added to `/etc/nginx/conf.d/aibi.conf`)

Both HTTP (port 80) and HTTPS (port 443) server blocks contain:

```nginx
location = /aiproduct              { return 301 /aiproduct/; }
location /aiproduct/assets/        { alias .../web/dist/assets/; expires 1y; }
location = /aiproduct/healthz      { proxy_pass http://127.0.0.1:42080/healthz; }
location = /aiproduct/readyz       { proxy_pass http://127.0.0.1:42080/readyz; }
location = /aiproduct/metrics      { proxy_pass http://127.0.0.1:42080/metrics; }
location /aiproduct/               { alias .../web/dist/; try_files $uri /aiproduct/index.html; }
location /aiproduct/api/           { proxy_pass http://127.0.0.1:42080/api/; ... }
location /aiproduct/ws             { proxy_pass http://127.0.0.1:42080/ws; Upgrade+Connection; }
location /aiproduct/tunnel         { proxy_pass http://127.0.0.1:42080/tunnel; Upgrade+Connection; }
location /aiproduct/session/       { proxy_pass http://127.0.0.1:42080/session/; Upgrade+Connection; }
location /aiproduct/ws/vnc/        { proxy_pass http://127.0.0.1:42080/ws/vnc/; Upgrade+Connection; }
```

Order matters: exact matches (healthz etc.) and API/WS locations must come BEFORE the SPA catch-all `location /aiproduct/`.

## Test Credentials

| Role | Email | Password |
|------|-------|----------|
| Admin | `test@deepwit.ai` | `Test1234567890!` |
| Customer | `cust@deepwit.ai` | `Test1234567890!` |

## File Changes Summary

| File | Change |
|------|--------|
| `web/vite.config.ts` | `define` for VITE_API_PREFIX, `build.target: es2022`, ports 42080/45173 |
| `web/src/api/client.ts` | API prefix from `import.meta.env.VITE_API_PREFIX` |
| `web/src/api/ws.ts` | WS path from `import.meta.env.VITE_API_PREFIX` |
| `web/src/auth/TokenManager.ts` | All 4 auth fetch URLs prefixed with `PREFIX` |
| `web/src/router.tsx` | `basename` from `VITE_BASE` |
| `web/src/pages/SettingsPage.tsx` | `window.location.href` → `navigate()` |
| `web/src/components/VNCPanel.tsx` | `@novnc/novnc/core/rfb` → `@novnc/novnc` |
| `web/.env.production` | VITE_API_PREFIX + VITE_BASE |
| `gateway/internal/httpapi/middleware.go` | Removed restrictive CSP header |
| `gateway/Dockerfile` | Multi-stage build |
| `deploy/cloud/docker-compose.yml` | Port 42080, logging env vars |
| `deploy/.env` | Dev credentials |
| `/etc/nginx/conf.d/aibi.conf` | 12 location blocks for `/aiproduct` |

## Skill Vault

### Skill Format (Claude Code)

Skills follow the Claude code support format: a single `SKILL.md` markdown file containing
skill instructions. The file is archived as a `.tar.gz` (or `.zip` when zip support is added).

### Adding a Skill via API

```bash
# 1. Create skill entry
curl -X POST /api/v1/admin/skills \
  -H "Authorization: Bearer <admin_token>" \
  -d '{"key":"my-skill","name":"My Skill","description":"...","visibility":"public"}'

# 2. Package the skill artifact
tar czf skill.tar.gz SKILL.md

# 3. Publish a version (multipart)
curl -X POST /api/v1/admin/skills/{skill_id}/versions \
  -H "Authorization: Bearer <admin_token>" \
  -F "version=1.0.0" \
  -F 'manifest={"name":"my-skill","version":"1.0.0","entrypoint":"SKILL.md","type":"claude-code"}' \
  -F "artifact=@skill.tar.gz"

# 4. Install fleet-wide (requires online devices)
curl -X POST /api/v1/admin/skills/{skill_id}/install \
  -H "Authorization: Bearer <admin_token>"
```

### Manifest Format

```json
{
  "name": "skill-key",
  "version": "1.0.0",
  "entrypoint": "SKILL.md",
  "type": "claude-code",
  "description": "Skill description"
}
```

### TODO: Zip Support for Skill Dispatch

Current implementation uses `.tar.gz` for skill artifact storage and dispatch
(`skillvault/vault.go` uses `tgz` writer). Device SkillManager (`device/iagent_device/skills/manager.py`)
receives chunked bytes and writes to local cache.

Zip support requires:
1. Gateway: Change `vault.go` `PublishVersion` to accept either format, or switch to zip
2. Device: Update `SkillManager` to handle zip extraction (currently stores raw archive)
3. Agent: Update `skills/loader.py` to extract zip archives

### Stitch Design Taste Skill

- **Key**: `stitch-design-taste`
- **Source**: `/root/.claude/skills/stitch-design-taste/SKILL.md`
- **Version**: 1.0.0
- **SHA256**: `7b1fc3c2036d0965b0b76cffbb76deffa6e6b5b6c30ba30e6771de935a930f12`
- **Visibility**: public
- **Status**: Published, visible to all customers
