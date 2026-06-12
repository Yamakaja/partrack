# Getting Started

## Prerequisites

| Tool | Minimum version | Notes |
|---|---|---|
| Go | 1.25 | `go version` |
| Node.js | 20 LTS | `node --version` |
| npm | 10 | bundled with Node |
| PostgreSQL | 15 | local install, Docker, or managed |
| sqlc | latest | only needed when editing SQL queries |

---

## 1. Database

partrack requires a PostgreSQL database. Create one before starting the server:

```bash
createdb partrack
```

Or with a connection string directly:

```bash
psql postgres://localhost/postgres -c "CREATE DATABASE partrack;"
```

**Migrations run automatically** on every server start via golang-migrate. There is no manual migration step.

---

## 2. Configuration

partrack is configured with a YAML file and/or environment variables. Environment variables override file values.

### Minimal `config.yaml`

```yaml
database:
  url: postgres://localhost/partrack?sslmode=disable

server:
  listen_addr: ":8080"
```

Place this file in the directory where you run the binary (or set `PARTRACK_CONFIG=/path/to/config.yaml`).

### Full reference

```yaml
database:
  url: postgres://user:password@host:5432/partrack?sslmode=disable

server:
  listen_addr: ":8080"            # default
  trusted_proxies: ["127.0.0.1"]  # IPs/CIDRs whose X-Forwarded-For is trusted
                                  # for the client IP (rate limiting / audit log).
                                  # Set to your reverse proxy's address.

auth:
  session_secret: "…"             # ≥ 32 bytes; startup fails otherwise
  rate_limit_per_minute: 10       # per-IP budget on credential endpoints; 0 disables

log:
  level: info                     # debug | info | warn | error

# app.base_url is required when OIDC providers are configured — the redirect URI
# is derived from it; register <base_url>/api/v1/auth/oidc/<name>/callback at the
# identity provider.
app:
  base_url: https://partrack.example.com
  secure_cookies: true            # default; set false only for local HTTP (no TLS)

oidc:
  - name: google             # URL-safe slug, used in routes
    display_name: Google     # login button label; defaults to the slug
    issuer: https://accounts.google.com
    client_id: "..."
    client_secret: "..."     # or PARTRACK_OIDC_GOOGLE_SECRET env var
    auto_provision: true     # create user accounts on first login
```

On first SSO login, identities with a **verified** email matching an existing user are linked to that user automatically; unmatched identities create a new account only when `auto_provision` is enabled. Newly provisioned users have no team memberships — an admin assigns them to teams.

To authenticate against an LDAP/Active Directory directory instead (or in addition), configure an `ldap:` block — see [LDAP / Active Directory Authentication](doc/auth-ldap.md). Directory users sign in through the normal login form; group memberships can be mapped to partrack teams and roles.

### Environment variable overrides

| Variable | Equivalent config key |
|---|---|
| `PARTRACK_DB_URL` | `database.url` |
| `PARTRACK_SESSION_SECRET` | `auth.session_secret` |
| `PARTRACK_LISTEN_ADDR` | `server.listen_addr` |
| `PARTRACK_APP_BASE_URL` | `app.base_url` |
| `PARTRACK_SECURE_COOKIES` | `app.secure_cookies` (`false`/`0` to disable) |
| `PARTRACK_CONFIG` | path to the config file |
| `PARTRACK_OIDC_<NAME>_SECRET` | `oidc[name].client_secret` |
| `PARTRACK_LDAP_<NAME>_PASSWORD` | `ldap[name].bind_password` |

---

## 3. Development

Development runs two processes side by side: the Go backend and the Vite dev server. They communicate via Vite's API proxy.

### Terminal 1 — Go backend

```bash
go run ./cmd/partrack
```

The server starts on `:8080` and prints:

```
partrack listening on :8080
```

Migrations are applied automatically on startup. The backend serves the API at `/api/v1/...` and the embedded frontend at all other paths.

### Terminal 2 — Frontend dev server

```bash
cd web
npm install          # first time only
npm run dev
```

Vite starts on `http://localhost:5173` (default). Requests to `/api` are proxied to `http://localhost:8080`, so the frontend talks to the live Go backend. Hot module replacement (HMR) is active for all Svelte and TypeScript files.

Open `http://localhost:5173` in your browser.

### Bootstrapping the first admin

On the **first** start against an empty `users` table, partrack automatically
creates an admin account (`admin@localhost`) and prints a one-time
password-reset link to the server log (stdout):

```
  ────────────────────────────────────────────────────────────
  Bootstrap admin created (no existing users found)
  Email:   admin@localhost
  Link:    /reset-password?token=…
  Expires: …
  ────────────────────────────────────────────────────────────
```

Open that link (prefixed with your base URL) to set the admin password, then
log in as `admin@localhost`. The link is valid for 24 hours; if it expires,
request a new one from the login page's "forgot password" flow. The bootstrap
runs only while the users table is empty — it is a no-op on every subsequent
start.

To promote an existing user to admin later, use the admin panel, or set the
flag directly:

```bash
psql "$PARTRACK_DB_URL" -c \
  "UPDATE users SET is_admin = TRUE WHERE email = 'your@email.com';"
```

### Modifying SQL queries

partrack uses [sqlc](https://sqlc.dev) to generate type-safe Go from SQL. After editing any file under `db/queries/`:

```bash
sqlc generate
```

This regenerates `internal/db/gen/`. Commit the generated files alongside the SQL changes.

---

## 4. Production

### Step 1 — Build the frontend

```bash
cd web
npm install
npm run build
```

Output goes to `web/dist/`. This directory is embedded into the Go binary at compile time; it must be built before the Go build.

### Step 2 — Build the binary

```bash
go build -o partrack ./cmd/partrack
```

The resulting `partrack` binary is self-contained: it embeds the frontend assets and SQL migrations. No separate file deployment is needed.

### Step 3 — Run

```bash
PARTRACK_DB_URL=postgres://user:password@host/partrack ./partrack
```

Or with a config file:

```bash
PARTRACK_CONFIG=/etc/partrack/config.yaml ./partrack
```

The server listens on `:8080` by default. Put a reverse proxy (nginx, Caddy, etc.) in front of it for TLS termination.

### Minimal nginx config

```nginx
server {
    listen 443 ssl;
    server_name partrack.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;

        # Required for SSE (inventory events stream)
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 3600s;
    }
}
```

---

## 5. Type-checking the frontend

```bash
cd web
npm run check
```

This runs `svelte-check` + `tsc` and reports any TypeScript or Svelte type errors without building.

---

## 6. Quick reference

```bash
# Run backend (dev)
go run ./cmd/partrack

# Run frontend dev server
cd web && npm run dev

# Build everything (production)
cd web && npm run build && cd .. && go build -o partrack ./cmd/partrack

# Regenerate DB query types after editing db/queries/
sqlc generate

# Type-check frontend
cd web && npm run check

# Promote an existing user to admin (the first admin is auto-created on first start)
psql "$PARTRACK_DB_URL" -c "UPDATE users SET is_admin = TRUE WHERE email = 'you@example.com';"
```
