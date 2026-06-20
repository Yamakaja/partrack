# Administration

This chapter is for two audiences:

- **Instance admins** — users with the `is_admin` flag set. The **Admin** section of the sidebar is only visible to them, and every endpoint under `/api/v1/admin/` is gated by `RequireAdmin`.
- **Team admins** — users with the `admin` role on a team, who configure that team's distributor connections under **Settings → Distributors**.

Instance-admin access overrides and is orthogonal to team roles. An instance admin does not automatically gain membership in every team, but they can read or write any team's catalog data through the admin API. See [Roles and Permissions](roles-and-permissions.md) for how team roles work.

---

## Users

**Admin → Users** gives you a full list of every account on the instance (paginated, up to 200 per page via the API). The table shows display name, email, status badge (Active / Inactive), the Admin flag, whether a local password is set, and the account creation date.

### Creating an account

Click **Add User** and supply a display name, email, and an optional initial password. If you leave the password blank the account is created with no local password — send the user a one-time reset link instead (see below). The email must be unique on the instance.

### Setting and resetting passwords

Two actions are available per row:

- **Set Password** — immediately replaces any existing local password. Use this when you need to set a known credential for a user (for example, a service account or initial setup).
- **Reset Link** — generates a cryptographically random, single-use password-reset token valid for **24 hours**. The dialog shows the full URL (`/reset-password?token=…`); copy it and send it to the user over a secure channel. The link is consumed the moment the user sets their password.

Deactivating an account (see below) does **not** expire outstanding reset links. If you deactivate a user, revoke any outstanding link by not sharing it, or generate a new one after reactivation to invalidate the old hash chain.

### Editing an account

**Edit** lets you change a user's display name, email, and the instance-admin flag in one dialog. You can grant or revoke admin access here. There is no secondary confirmation — the change takes effect immediately.

### Deactivating and reactivating

**Deactivate** sets `is_active = false` and **immediately revokes all sessions and API tokens** for that user. They cannot sign in again (all credential checks reject inactive users) but their history, team memberships, audit log entries, and data remain intact. Use this when someone leaves rather than deleting anything.

**Reactivate** restores the flag. Existing sessions and tokens were already purged; the user needs to sign in again. You may also want to send a fresh reset link if they no longer know their password.

### First-run bootstrap

On the **first startup with an empty database**, partrack automatically creates an `admin@localhost` account with the instance-admin flag and prints a one-time reset link to **stdout** (not stderr):

```
  ────────────────────────────────────────────────────────────
  Bootstrap admin created (no existing users found)
  Email:   admin@localhost
  Link:    /reset-password?token=<token>
  Expires: <24 hours from start>
  ────────────────────────────────────────────────────────────
```

Capture this output (Docker: `docker logs <container>`), prepend your `app.base_url` to the path, and open the link to set the first password. The bootstrap runs exactly once; subsequent starts with existing users are no-ops.

---

## Teams

Team **creation** is restricted to instance admins (all other team management — membership, roles, settings — is done by team admins). In the UI this means the **New Team** button on the team-select screen is only visible to admins. Via the API, `POST /api/v1/teams` requires the `RequireAdmin` gate.

Once a team exists, its admin can manage it independently. Instance admins are not automatically members of teams they create.

---

## Parameters

**Admin → Parameters** (labelled "Parameter Registry" in the UI) maintains the catalog-wide master list of attribute definitions. Every technical characteristic that a part can carry — resistance, capacitance, voltage rating, operating temperature, package — lives here as a **parameter definition**.

### Parameter definitions

Each definition has:

| Field | Description |
|---|---|
| **Slug** | Machine identifier, lowercase and underscores (e.g. `resistance`). Referenced by all other tables. |
| **Display Name** | Human label shown in the UI (e.g. "Resistance"). |
| **Unit identifier** | The dimensional unit (e.g. `ohm`). Optional; used for display grouping. |
| **Display symbol** | The SI symbol shown in values (e.g. `Ω`). Optional. |
| **Value type** | How raw string values are parsed: `numeric`, `range`, `text`, `enum`, or `enum_set`. |

**Value types:**

- `numeric` — a single SI-aware number (`10k`, `4.7µ`, `100`). Parsed to a float for range search.
- `range` — a min–max pair (e.g. `-40℃ to +125℃`). Stored as two floats.
- `text` — a free-form string; not numerically searchable.
- `enum` — a single value from a discrete vocabulary.
- `enum_set` — a list of enum values (e.g. operating modes).

**Changing the value type** of an existing definition triggers an immediate background re-parse of every part parameter linked to that definition — the response includes `reparsed_count`. This keeps stored numeric values consistent with how the registry now interprets raw strings.

You can delete a definition, but doing so orphans any distributor mappings and part parameters that referenced it. They retain their raw value strings but lose their canonical link and numeric parsing.

### Distributor parameter mappings

When part enrichment imports data from a distributor, it records every parameter name the distributor uses as a **distributor parameter mapping**. These mappings are not yet linked to canonical definitions — you do that here.

The **Unmapped** tab (with a red badge showing the count) lists mappings that have no canonical link. For each one you can:

- **Link** — open a searchable picker and choose an existing definition. This immediately back-fills all part parameters for that mapping with the canonical ID and re-parses numeric values.
- **Make New** — opens the "Add Parameter Definition" dialog pre-filled from the distributor parameter name, with a live **parse preview** showing how real sample values would parse under the chosen value type and symbol.
- **Ignore** — marks the mapping as intentionally unmapped (hides it from the Unmapped tab). Restore it via the **Mappings** tab.

The **Mappings** tab shows every known distributor parameter and its current status (linked, ignored, or unmapped). You can relink or unignore from here.

Keeping unmapped parameters low ensures that numeric search and parametric BOM matching see as much data as possible. Newly enriched parts discover new parameters; check Unmapped periodically after enabling a new distributor.

---

## Category Parameters

**Admin → Category Parameters** curates the *class profile* for each part category. This drives how generic/parametric BOM imports are interpreted and how the "Edit interpretation" UI presents parameters to the reviewer.

### What each field means

| Field | Description |
|---|---|
| **Value parameter** | The parameter slug that a bare value field encodes for this category. For resistors it is `resistance`; for capacitors, `capacitance`. Leave blank for non-parametric categories. |
| **Generic code** | A short uppercase code (max 8 characters) used to synthesize MPNs for generic parts in this category — e.g. `RES` → `GEN-RES-4K7-0402-1%`. Requires a value parameter; blank means generics are unsupported for the category. |
| **Primary parameters** | An ordered list of parameter slugs that matter most for this category, used when displaying a part and when the LLM parser extracts structured values from BOM descriptions. The value parameter is automatically included. |

### Workflow

The page defaults to showing only configured categories. Check **Show all** or use the search box to find a category to set up. Click **Edit** to open the profile dialog:

1. Choose the value parameter from a dropdown of all parameter definitions.
2. Set a generic code if this category supports batch/generic part creation.
3. Add and reorder primary parameters using the picker and the up/down arrows.

Every referenced slug is validated against the parameter registry on save. Getting category profiles right matters for BOM import: the value parameter tells the interpreter what "10k" means on a resistor line, and primary parameters tell the LLM extraction what fields to look for. See [Projects and BOM](projects-bom.md) for how these are used at import time.

---

## Authentication

partrack has one canonical user identity and several ways to prove it. All of them converge on the same 30-day session cookie.

### Local passwords

The default. Users sign in with email and password. Instance admins can set or reset passwords from **Admin → Users**. Local login is always active — it cannot be disabled — so the bootstrap `admin@localhost` account keeps working even when SSO is enabled.

### OIDC (single sign-on)

Configure one or more OpenID Connect providers in the server config file under the `oidc:` key. Each provider needs:

| Key | Required | Description |
|---|---|---|
| `name` | yes | URL-safe slug; appears in the OIDC redirect URI. |
| `display_name` | no | Label on the sign-in button; defaults to `name`. |
| `issuer` | yes | The IdP's issuer URL (discovery runs from `<issuer>/.well-known/openid-configuration`). |
| `client_id` | yes | OAuth2 client identifier. |
| `client_secret` | yes (or env) | OAuth2 client secret. Override with `PARTRACK_OIDC_<NAME>_SECRET`. |
| `auto_provision` | no | Create a partrack account on first login if none exists. |

**`app.base_url` is required whenever any OIDC provider is configured.** It is used to build the redirect URI that the IdP must whitelist — typically `https://partrack.example.com`. Without it, startup aborts. Override with `PARTRACK_APP_BASE_URL`.

OIDC providers appear as separate sign-in buttons on the login page. An external identity is linked to a partrack account keyed on `(provider, subject)`. The same user can log in with different providers and land on the same account.

Discovery runs lazily on first use if the IdP is unreachable at startup — the button appears but fails gracefully until the IdP comes back.

### LDAP / Active Directory

LDAP login is **credential-based**, not a browser redirect: users type their username and password into partrack's normal login form, and partrack verifies them against the directory. There is no separate LDAP button. Because local passwords are checked first, LDAP only runs when the user has no stored local password.

Configure one or more providers under `ldap:` in `config.yaml`. Required fields are `name`, `url`, `user_base_dn`, and `user_filter` (which must contain a single `%s` placeholder for the escaped username). See the [LDAP configuration reference](#ldap-configuration-reference) below for full details.

Key behaviors:

- **Account linking** — on a successful bind, the directory identity is matched to a partrack user: first by a prior `(provider, subject)` link, then by email, then auto-provisioned if `auto_provision: true`.
- **Login gate** — set `required_groups` to a list of group DNs. Users who bind successfully but are in none of them are denied, exactly like a wrong password.
- **Instance admin sync** — if `admin_group` is set, the user's `is_admin` flag is granted or revoked on every login to match current group membership. Unlike team-role overrides, this is not pinnable — manage it through the directory group.
- **Team-role sync** — `group_mappings` entries map a directory group DN to a partrack team name and role. On every login, memberships in those teams are reconciled to the user's current groups (highest role wins on conflict). Memberships not covered by any mapping are left untouched. A manually-overridden membership is pinned and never touched by sync.
- **Directory changes apply on next login**, not in real time.

For Active Directory: use `attr_unique_id: objectGUID` and `user_filter: "(sAMAccountName=%s)"` or `"(userPrincipalName=%s)"`.

#### LDAP Diagnostics

**Admin → LDAP** is a read-only diagnostic tool. It never creates users, identities, memberships, or sessions — it only reports what would happen.

Three actions:

- **Run login test** — supply a username and password; runs the real search-then-bind and group evaluation and shows a step-by-step trace, resolved identity, authorization decision, group checks, and account resolution outcome.
- **Look up user** — supply only a username; runs every lookup without checking a password. Use this when a user says they cannot sign in and you don't have their credentials.
- **Test connection** — no username needed; dials the directory, verifies TLS, binds the service account, and checks that every configured DN (user base, group base, required groups, admin group, all mapping groups) actually exists. The effective configuration (bind password redacted) is shown so you can confirm env-var overrides applied.

If no LDAP providers are configured in `config.yaml`, the page shows an empty state.

#### LDAP configuration reference

Minimal required config:

```yaml
ldap:
  - name: corp
    url: "ldaps://ldap.corp.example:636"
    bind_dn: "cn=svc-partrack,ou=svc,dc=corp,dc=example"
    bind_password: "..."          # or env PARTRACK_LDAP_CORP_PASSWORD
    user_base_dn: "ou=people,dc=corp,dc=example"
    user_filter: "(uid=%s)"
    auto_provision: true
```

Key optional fields:

| Key | Default | Description |
|---|---|---|
| `display_name` | `name` | Informational label. |
| `start_tls` | `false` | Upgrade a plaintext `ldap://` connection to TLS. Ignored for `ldaps://`. |
| `ca_cert_file` | — | PEM file of additional trusted CA certs. Prefer over `insecure_skip_verify`. |
| `insecure_skip_verify` | `false` | Disable TLS certificate verification. Development only. |
| `timeout_seconds` | `8` | Connect + bind timeout. |
| `email_domains` | — | Route logins for these domains to this provider; catch-all otherwise. |
| `strip_domain` | `false` | Pass only the local part of an email to `user_filter` (use with `uid=` filters). |
| `attr_email` | `mail` | User entry attribute for email. |
| `attr_display_name` | `cn` | User entry attribute for display name. |
| `attr_unique_id` | `entryUUID` | Stable unique subject; `objectGUID` for AD. |
| `attr_member_of` | `memberOf` | Group membership attribute. |
| `group_base_dn` | — | Enable reverse-search group resolution (needed without `memberOf` or for AD nested groups). |
| `group_filter` | `(member=%s)` | Search filter for group resolution when `group_base_dn` is set. |
| `expand_nested_groups` | `true` | Resolve group membership transitively (bounded walk, ≤50 groups, ≤10 levels). |
| `required_groups` | — | Gate login to users in any of these group DNs. |
| `admin_group` | — | Members get the instance-admin flag; non-members lose it. |
| `group_mappings` | — | List of `{group, team, role}` entries. An empty `group` matches all authenticated users. |

The bind password can always be supplied via `PARTRACK_LDAP_<NAME>_PASSWORD` (where `<NAME>` is the uppercased provider slug) to avoid storing it in the config file.

### External identities

LDAP and OIDC identities are stored in the same `external_identities` table keyed on `(provider, subject)`. A single partrack account can have multiple external identities — for example, both an OIDC identity from a corporate IdP and an LDAP identity from a directory. All of them resolve to the same user record and its history.

---

## Distributor Configuration (team admins)

Each team configures its own distributor connections under **Settings → Distributors** (requires the team `admin` role). This is not an instance-admin task — each team manages its own credentials independently.

### What you configure per distributor

| Setting | Description |
|---|---|
| **Enabled** | Whether this distributor is active for the team. Disabled distributors are skipped for enrichment, listing refresh, and order generation. |
| **Preferred** | Marks this as the default distributor for the team. Only one distributor can be preferred at a time; setting a new one automatically clears the previous. |
| **Cache duration** | How long fetched price and availability data is considered fresh before partrack refreshes it. Set this to match how quickly the distributor's data changes and how frequently your team checks stock. |
| **API credentials** | Distributor-specific fields (client ID, client secret, API key, etc.). Secret fields are never returned in responses; leaving them blank on an update preserves the stored value. |

### OAuth distributors (DigiKey)

DigiKey uses OAuth 2.0. Instead of entering a static API key, click **Connect** to start an OAuth flow — you are redirected to DigiKey's authorization page and back. The resulting tokens are stored per team per distributor and refreshed automatically. You can disconnect at any time, which revokes the stored tokens.

### What enabling a distributor unlocks

Once a distributor is enabled with valid credentials, partrack can:

- **Enrich parts** — look up a distributor SKU and pull manufacturer, description, parameters, package, lifecycle status, and pricing into the part record.
- **Refresh listings** — update cached price breaks and stock availability.
- **Search** — find new parts by keyword from the catalog.
- **Generate order lists** — during [cart conversion](cart.md), group cart items into orders by distributor.

See [Parts and Inventory](parts-inventory.md) and [Orders](orders.md) for how these capabilities are used.

---

## Running partrack

This section covers operational concerns for whoever deploys and maintains the instance.

### Single-instance model

partrack is designed for **one replica**. The worker queue, SSE live-update hubs, scan-ticket store, and auth cache are all in-process. Do not run multiple instances against the same database — migrations auto-run at startup and would race, and in-memory state is not shared between processes. Scale vertically (more CPU and RAM), not horizontally.

### Configuration

partrack is configured by a YAML file plus environment variable overrides. The file path is read from `PARTRACK_CONFIG` (defaults to `./config.yaml`). If the file does not exist, defaults and env vars apply.

**Required settings:**

| Setting | Env var | Description |
|---|---|---|
| `auth.session_secret` | `PARTRACK_SESSION_SECRET` | HMAC key for sessions and signed tokens. Must be at least 32 bytes. Startup aborts if absent or too short. |
| `database.url` | `PARTRACK_DB_URL` | PostgreSQL connection URL. |
| `app.base_url` | `PARTRACK_APP_BASE_URL` | Public URL of the instance. Required when any OIDC provider is configured; used to build redirect URIs. |

**Common optional settings:**

| Setting | Env var | Default | Description |
|---|---|---|---|
| `server.listen_addr` | `PARTRACK_LISTEN_ADDR` | `:8080` | Address and port to listen on. |
| `server.trusted_proxies` | — | — | CIDRs or IPs whose `X-Real-IP`/`X-Forwarded-For` headers are trusted (for correct client IP in rate limiting and logs). |
| `app.secure_cookies` | `PARTRACK_SECURE_COOKIES` | `true` | Set `false` only for local HTTP development without TLS. |
| `log.level` | `PARTRACK_LOG_LEVEL` | `info` | `debug` for verbose output. |
| `auth.rate_limit_per_minute` | — | `10` | Per-IP request budget for unauthenticated credential endpoints (login, password reset, token creation). Set `0` to disable. |

**Blob storage (for note images and documents):**

| Setting | Env var | Default | Description |
|---|---|---|---|
| `storage.backend` | — | `file` | Currently only `file` is supported. |
| `storage.data_dir` | `PARTRACK_STORAGE_DATA_DIR` | `./data` | Filesystem root for stored blobs. Back this directory up alongside the database. |
| `storage.max_upload_bytes` | `PARTRACK_STORAGE_MAX_UPLOAD_BYTES` | 5 MiB | Per-upload size cap for note-embed images. |
| `storage.max_document_bytes` | `PARTRACK_STORAGE_MAX_DOCUMENT_BYTES` | 25 MiB | Per-upload size cap for part and project documents. |

**LLM fallback (optional):**

If enabled, partrack sends BOM line items that the deterministic parser cannot interpret to an OpenAI-compatible chat-completions API for interpretation. This sends BOM data to an external provider. Set `llm.enabled: true` and supply `llm.api_key` and `llm.model`. The API key can be set via `PARTRACK_LLM_API_KEY`. Defaults to OpenRouter; set `llm.base_url` for a different endpoint (Azure, LiteLLM, etc.).

### Database and migrations

Migrations run **automatically at startup** via golang-migrate. There is no separate migration command for normal operation. Startup backfills (for data that needs app logic rather than pure SQL) also run at startup and are guarded by a marker table (`app_backfills`) so they execute only once.

Postgres must have the `pg_uuidv7` extension installed. The `docker/Dockerfile.postgres` image builds it.

### Backup

Back up in this order:

1. **Database dump** (pg_dump) — captures all relational data.
2. **Blob storage directory** (`storage.data_dir`) — captures uploaded images and documents. Every blob the database references must be present, so dump the DB first.

### Graceful shutdown

partrack traps `SIGINT`/`SIGTERM`. On receipt it:

1. Stops accepting new connections (`srv.Shutdown` with a 30-second budget).
2. Drains in-flight worker jobs (`q.Shutdown`).
3. Closes the database pool.

A second signal during shutdown force-quits immediately (the default signal handler is restored after the first signal).

`WriteTimeout` is intentionally unset so SSE streams (`/events`, `/scan/stream`) are not severed mid-connection.

### Health endpoints

| Endpoint | Purpose | Notes |
|---|---|---|
| `GET /api/v1/health` | Liveness | Always returns 200 as long as the process is up. Used for Docker `HEALTHCHECK`. Does not query the database. |
| `GET /api/v1/ready` | Readiness | Pings the database. Returns 503 when the DB is unreachable. Use for load-balancer routing decisions. Does not trigger container restarts on its own. |

### Metrics

**Admin → Metrics** (or `GET /api/v1/admin/metrics`) returns a snapshot of the in-process, ephemeral request and query timing registry:

- Slowest SQL queries by name (aggregated from `-- name:` comments, measured via a pgx query tracer).
- Slowest route patterns (by chi route pattern, total DB time + handler time).
- Heaviest users and projects by database time.

Aggregates rotate on a one-hour window and reset on process restart. There is no external metric collector — this is a lightweight first-look view. Use `pprof` (if enabled separately) for CPU and heap profiling.

---

## Security notes

A few things to keep in mind when operating a partrack instance:

- **Session secret rotation** — changing `auth.session_secret` invalidates all existing sessions and signed tokens. Users will need to log in again.
- **LDAP bind password** — always supply this via the `PARTRACK_LDAP_<NAME>_PASSWORD` environment variable rather than the config file, so it does not end up in version control.
- **OIDC client secrets** — similarly, use `PARTRACK_OIDC_<NAME>_SECRET`.
- **TLS termination** — partrack expects TLS to be terminated by a reverse proxy. Set `server.trusted_proxies` to the proxy's IP/CIDR so client IPs are resolved correctly for rate limiting and audit logging.
- **`insecure_skip_verify` in LDAP** — this disables TLS certificate verification entirely. It is intended for development only; a valid certificate or a `ca_cert_file` PEM bundle is always preferred.
- **Deactivating users** immediately revokes all sessions and API tokens, so the change takes effect without waiting for tokens to expire.
