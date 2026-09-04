# partrack — System Architecture

This document is the primary onboarding reference for new developers. Read it alongside the
companion docs listed below.

| Document | Purpose |
|---|---|
| [userstory.md](userstory.md) | User stories and epics — what the system must do |
| [datamodel.md](datamodel.md) | Every entity, its fields, and every design decision |
| [api.md](api.md) | Full REST endpoint reference |
| [frontend.md](frontend.md) | Frontend stack, routing, component structure, page descriptions |
| [caching.md](caching.md) | In-process caching strategy and invalidation rules |
| [backend-testing.md](backend-testing.md) | Test infrastructure, patterns, and how to run the suite |
| [distributor/distributor-digikey.md](distributor/distributor-digikey.md) | DigiKey API integration specifics |

---

## 1. What partrack Does

partrack is an electronics parts inventory tracking system for PCB design teams. The core workflow:

1. User imports a BOM (CSV) from their EDA tool into a **Project**
2. System resolves each BOM line item to a **Part** (via MPN match, distributor SKU lookup, or
   distributor API query)
3. User runs a **stock check** against a build quantity — sees shortfalls per line item
4. Shortfalls are added to the team's **Shopping Cart** and converted to **Orders** per distributor
5. Orders are received; **Inventory** is updated and parts are enriched with distributor metadata

---

## 2. Guiding Principles

| Principle | Implication |
|---|---|
| **Performance** | Compiled backend; long-running operations are async with SSE progress |
| **Small footprint** | No clustering or distributed infrastructure; single binary deployment |
| **Safety** | Statically typed throughout; memory-safe runtimes; never delete parts |
| **Low complexity** | Simplest solution that satisfies requirements; avoid over-engineering |

---

## 3. Technology Stack

### Backend

| Concern | Choice |
|---|---|
| Language | **Go** — compiles to a self-contained binary; goroutine concurrency suits I/O-heavy workloads |
| HTTP router | **Chi** — thin wrapper on `net/http`; composable middleware; no framework lock-in |
| Database | **PostgreSQL** via **pgxpool** — connection pooling; single external DB assumption |
| Query layer | **sqlc** — annotated SQL → type-safe Go; queries are plain SQL, fully compile-time checked |
| Migrations | **golang-migrate** — sequential `.up.sql`/`.down.sql` files; run automatically at startup |
| Auth | **go-oidc v3 + oauth2** for OIDC; bcrypt for passwords; SHA-256 for token hashes |

### Frontend

| Concern | Choice |
|---|---|
| Framework | **Svelte 5 + TypeScript** — compiles to plain JS; runes reactivity model |
| Build | **Vite** — fast dev server; TypeScript built-in; outputs to `web/dist/` |
| CSS | **Tailwind CSS v4** — utility-first; zero runtime |
| Components | **shadcn-svelte** (on bits-ui) — headless accessible components; copied into source |
| Routing | Custom ~80-line history-mode router (no mature Svelte 5 alternative outside SvelteKit) |
| Server state | **@tanstack/svelte-query** — caching, background refetch, loading/error states |
| HTTP client | Native `fetch` + thin typed wrapper; `credentials: 'include'` for session cookies |
| SSE | Native `EventSource` API — no library needed |
| CSV parsing | **PapaParse** — browser-side BOM import |

Full frontend detail is in [frontend.md](frontend.md).

---

## 4. Project Layout

```
cmd/
  partrack/           # main() — loads config, opens DB pool, runs migrations,
                      #           constructs server + worker, starts listening
internal/
  config/             # Config struct; YAML + env-override loading
                      # (env prefix: PARTRACK_; e.g. PARTRACK_DB_URL)
  db/
    pool.go           # pgxpool.New() — panics on failure; called once at startup
    migrate.go        # golang-migrate runner; embeds db/migrations/ FS
    gen/              # sqlc output — DO NOT edit by hand; regenerate with `sqlc generate`
  server/
    server.go         # Chi router construction; global middleware; AuthCache init;
                      #   static-file fallback (SPA index.html)
    routes.go         # All route registrations, grouped by auth level (see §6)
  middleware/
    auth.go           # Authenticate — validates session/API token, injects *User into ctx
    team.go           # RequireTeamRole — loads membership, enforces role hierarchy
    cache.go          # AuthCache — in-process token + membership cache;
                      #   background flusher for last_seen_at / last_used_at
  handler/
    handler.go        # Handler struct (DB pool, Queries, Config, Worker, AuthCache, OIDC);
                      #   writeJSON() / writeError() helpers
    auth.go           # Login, OIDC, session/token management
    teams.go          # Team CRUD + membership
    members.go        # Team member add/update/remove, user deactivation
    admin.go          # Instance-level user management, password reset links
    preferences.go    # User preferences (opaque JSONB)
    sessions.go       # List/revoke sessions
    tokens.go         # Create/list/revoke API tokens
    distributors.go   # Global distributor list + per-team config (credentials, cache)
    parts.go          # Parts catalog, parameters, distributor listings, enrichment
    storage.go        # Storage location CRUD
    inventory.go      # Inventory entries, stock adjustments
    projects.go       # Project CRUD, active revision pointer
    revisions.go      # BOM import, revision CRUD
    items.go          # BOM line item resolve + substitutes
    stockcheck.go     # Stock check calculation at a given build quantity
    cart.go           # Shopping cart CRUD, add-shortfalls, convert-to-orders
    orders.go         # Order lifecycle (draft → placed → received), CSV export
    builds.go         # Build records (PCB assembly), preview + confirm
    checkouts.go      # Ad-hoc parts withdrawal
    events.go         # SSE endpoint — streams job progress to connected clients
  worker/
    queue.go          # Channel-based job queue + SSE hub; job types:
                      #   JobBOMImport, JobPartEnrich, JobListingRefresh
  distributor/
    client.go         # Client interface: EnrichPart(mpn), RefreshListing(sku)
    registry.go       # Maps api_type string → Client implementation
    digikey/          # DigiKey API integration
    testclient/       # Mock distributor for tests
  testutil/           # Test helpers: MustDB, Truncate, seed helpers, Do()

db/
  db.go               # embed.FS for migrations — imported by internal/db/migrate.go
  migrations/         # golang-migrate SQL files
  queries/            # sqlc source files — edit these, then run sqlc generate

web/
  web.go              # embed.FS for dist/ — imported by server.go
  dist/               # Vite build output (git-ignored; .gitkeep committed)
  src/                # Svelte application source — see frontend.md
  index.html          # SPA shell
  vite.config.ts
  package.json

sqlc.yaml             # sqlc config (input: db/queries/, output: internal/db/gen/)
config.yaml           # (not committed) runtime configuration
```

---

## 5. Adding a New Feature (Workflow)

Adding a feature that touches the database always follows the same steps:

1. **Write SQL** in `db/queries/<domain>.sql` with a `-- name: QueryName :one|:many|:exec` annotation
2. **Run `sqlc generate`** — regenerates `internal/db/gen/`
3. **Write handler** in `internal/handler/<domain>.go` on the `Handler` struct; call
   `h.Queries.QueryName(ctx, params)` for DB access
4. **Register route** in `internal/server/routes.go` under the appropriate auth group
5. If the operation is slow or calls an external API, **enqueue a background job** via
   `h.Worker.Enqueue(worker.JobType, teamID, payload)` and add a handler in `worker/queue.go`

For schema changes, add a new migration pair first:
```
db/migrations/000003_description.up.sql
db/migrations/000003_description.down.sql
```
Migrations run automatically at startup via golang-migrate.

---

## 6. HTTP Request Lifecycle

### Middleware Chain

```
Incoming request
  → Chi router matches route
  → Global: RequestID, RealIP, Logger, Recoverer
  → Route-level middleware (varies by route group):

  Public routes (login, OIDC callback):
    → Handler directly

  Authenticated routes:
    → Authenticate  →  Handler

  Team-scoped routes:
    → Authenticate  →  RequireTeamRole("viewer"|"member"|"purchaser"|"admin")  →  Handler
```

### Authenticate Middleware (`middleware/auth.go`)

1. Extracts token from `session` cookie or `Authorization: Bearer` header
2. Computes SHA-256 hash
3. Looks up hash in `AuthCache` (in-memory, fast path)
4. On cache miss: queries DB (`GetUserBySessionToken` or `GetUserByAPIToken`)
5. Populates cache with `User` + expiry; marks entry dirty for `last_seen_at` flush
6. Injects `*middleware.User` into context — retrieve via `middleware.UserFromContext(ctx)`

### RequireTeamRole Middleware (`middleware/team.go`)

1. Extracts `{teamId}` from URL
2. Site admins (`user.IsAdmin`) bypass membership checks (implicit admin everywhere)
3. Otherwise, looks up `(teamID, userID)` in `AuthCache`
4. On miss: queries DB (`GetTeamMembership`), populates cache
5. Compares role against required minimum (`viewer < member < purchaser < admin`)
6. Returns 403 if insufficient; otherwise injects `*middleware.TeamMembership` into context

### Route Groups (server/routes.go)

```
/api/v1/
  auth/…                  ← Public (login, OIDC, password reset)
  [Authenticate]
    auth/sessions/…
    auth/tokens/…
    preferences
    events                ← SSE endpoint (team-filtered)
    [Admin flag]
      admin/users/…
    /teams/{teamId}/
      [RequireTeamRole("viewer")]      ← every read
        inventory/…
        parts/…
        storage/…
        projects/…
        orders/…
        builds/…
        checkouts/…
        cart, sourcing-policy (read)
        members (read)
        distributor-configs (read; config specifics only for admin)
      [RequireTeamRole("member")]
        projects/{id}/revisions   ← BOM import
        items/…
        scan-sessions/…
      [RequireTeamRole("purchaser")]
        cart/… (writes), sourcing-policy (write)
        orders/{id}/receive
      [RequireTeamRole("admin")]
        members/… (writes)
        distributor-configs/{id} (writes)
        settings/…
```

### Handler Conventions

```go
func (h *Handler) GetFoo(w http.ResponseWriter, r *http.Request) {
    user := middleware.UserFromContext(r.Context())    // injected by Authenticate
    mem  := middleware.MembershipFromContext(r.Context()) // injected by RequireTeamRole
    id   := chi.URLParam(r, "id")

    result, err := h.Queries.GetFoo(r.Context(), dbgen.GetFooParams{...})
    if errors.Is(err, pgx.ErrNoRows) {
        writeError(w, http.StatusNotFound, "not_found", "Foo not found")
        return
    }
    if err != nil {
        writeError(w, http.StatusInternalServerError, "db_error", err.Error())
        return
    }
    writeJSON(w, http.StatusOK, result)
}
```

---

## 7. Authentication & Identity

### Three Authentication Methods

| Method | Mechanism |
|---|---|
| **OIDC** | Primary. Delegates to external IdP. `sub` claim identifies/creates local User. |
| **Internal password** | Fallback. bcrypt-hashed. Configured per-instance. |
| **API tokens** | Long-lived bearer tokens. Hash stored; raw token shown once. |

### Session Tokens

After any successful login, an opaque random token is generated:
- Raw token returned to client (as HTTP-only Secure cookie for browsers; in body for API use)
- SHA-256 hash stored in `sessions` table with `expires_at` and `last_seen_at`
- `last_seen_at` is updated in-memory on every request and batch-flushed every 5 minutes
  (see [caching.md](caching.md) for full cache design)

### OIDC Auto-Provisioning

Controlled by `auto_provision` flag in `config.yaml` per provider:
- `true`: unknown OIDC login creates a new User (and optionally adds them to a default team)
- `false` (default): unrecognised logins rejected; admin must pre-create the account

### Role Hierarchy

```
viewer < member < purchaser < admin
```

All team-scoped routes declare a minimum role. The `RequireTeamRole` middleware enforces it.
The instance-level `is_admin` flag (on `users` table) is separate — it grants implicit team admin
everywhere and access to `/admin/*` routes.

First admin must be set via SQL:
```sql
UPDATE users SET is_admin = TRUE WHERE email = 'your@email.com';
```

---

## 8. Database Layer

### Schema Overview

The schema is defined in `db/migrations/000001_initial_schema.up.sql`. Key tables:

| Table | Purpose |
|---|---|
| `users`, `sessions`, `api_tokens` | Identity and authentication |
| `oidc_identities`, `user_credentials` | Auth methods per user |
| `teams`, `team_memberships` | Multi-tenant access |
| `parts`, `part_parameters`, `distributor_listings` | Global parts catalog |
| `distributors`, `distributor_configs` | Per-team distributor settings |
| `inventory_entries`, `stock_adjustments` | Inventory ledger |
| `storage_locations` | Physical locations |
| `projects`, `bom_revisions`, `bom_line_items`, `bom_line_item_substitutes` | BOM data |
| `shopping_carts`, `cart_items` | Procurement staging |
| `orders`, `order_line_items` | Purchase orders |
| `builds` | PCB assembly events |
| `general_checkouts` | Ad-hoc withdrawal events |

Full entity descriptions with every field and every design decision are in [datamodel.md](datamodel.md).

### Key Data Model Concepts

**Parts are never deleted.** Use lifecycle status (`Active`, `NRND`, `Obsolete`, `Deprecated`,
`No Longer Available`) instead.

**Inventory Entry ≠ stock record.** It is the team's *profile* for a part — holds storage location,
preferred distributor listing, min stock, reorder qty. Exists with zero quantity from the moment a
part is first encountered.

**Quantity on hand is derived.** Computed as the running sum of `stock_adjustments.delta` for a
given inventory entry. Never stored directly — always reconcilable from the ledger.

**Distributor listings are cached in the DB row.** `last_refreshed_at` + `cache_duration_seconds`
(from `distributor_configs`) determine staleness. No application-level cache.

### sqlc

All DB queries live in `db/queries/*.sql`. Each query has an annotation comment:
```sql
-- name: GetPartByMPN :one
SELECT id, mpn, manufacturer … FROM parts WHERE mpn = $1 …
```

Run `sqlc generate` after any change to regenerate `internal/db/gen/`. The generated code is
type-checked at compile time — a query with wrong parameter types will fail `go build`.

**Important sqlc gotchas** (from [backend-testing.md](backend-testing.md)):
- `:exec` queries never return `pgx.ErrNoRows`. Use `:one RETURNING id` on DELETEs/UPDATEs where
  you need to distinguish "not found" from success.
- `CASE WHEN col = other THEN TRUE ELSE FALSE END` is required for nullable boolean expressions.
  `(nullable = other)` returns NULL which pgx cannot scan into `bool`.

---

## 9. Background Jobs & SSE

### Job Queue (`worker/queue.go`)

```go
// Enqueue a job — returns job ID immediately (non-blocking)
jobID, err := h.Worker.Enqueue(worker.JobBOMImport, teamID, payload)

// Publish a progress event to SSE subscribers
h.Worker.Publish(worker.Event{
    TeamID: teamID,
    JobID:  jobID,
    Type:   "bom_import_progress",
    Data:   map[string]any{"resolved": 12, "total": 48},
})
```

The queue is a buffered channel. `n` goroutines drain it concurrently (n=1 by default; configured
at startup). Jobs are processed in `handleBOMImport`, `handlePartEnrich`, `handleListingRefresh`.

**Job types:**
| Job | Trigger | What it does |
|---|---|---|
| `JobBOMImport` | `POST /teams/{id}/projects/{id}/revisions` | Resolves BOM line items to Parts via SKU lookup then MPN lookup; creates Inventory Entries; fires progress events |
| `JobPartEnrich` | `POST /teams/{id}/parts/{id}/enrich` | Calls distributor API; updates Part metadata, parameters, listings |
| `JobListingRefresh` | `POST /teams/{id}/parts/{id}/listings/{id}/refresh` | Refreshes cached price/stock for one listing |

### SSE Endpoint (`handler/events.go`)

```
GET /api/v1/events?team_id={teamId}
```

Clients connect with `EventSource`. The handler subscribes to the team's event channel. Each event
is a JSON-encoded `worker.Event`. The frontend filters by `job_id` to route events to the correct
progress component.

**BOM import resolution priority:**
1. SKU-based: if `sku` + `distributor_name` are set on the line item, look up `distributor_listings`
   by SKU + distributor name (ILIKE)
2. MPN-based fallback: if `raw_description` is set, look up `parts` by MPN

---

## 10. Caching

Two in-process caches live in `middleware/cache.go` (see [caching.md](caching.md)):

| Cache | Key | Purpose |
|---|---|---|
| Token cache | `SHA-256(raw token)` | Avoids DB lookup on every request for session/API token validation |
| Membership cache | `(teamID, userID)` | Avoids DB lookup on every team-scoped request for role check |

Both caches are explicitly invalidated on writes (logout, role change, member removal, deactivation).
A background goroutine flushes dirty `last_seen_at` / `last_used_at` timestamps every 5 minutes.

**Nothing else is cached.** Inventory quantities, cart contents, order state, and BOM data are
never cached — they must be accurate.

---

## 11. Testing

Full strategy is documented in [backend-testing.md](backend-testing.md). Summary:

- **Integration-first**: every handler test sends a real HTTP request through the full Chi router
  against a real PostgreSQL database
- **No mocks**: the only unit tests (no DB) are for the `AuthCache` pure logic
- **Isolation**: each test calls `testutil.Truncate` in cleanup; no transaction rollback (handlers
  open their own transactions)
- **Test infrastructure**: `internal/testutil` provides `MustDB`, `Truncate`, seed helpers, and
  a `Do` HTTP helper

```bash
# Unit tests only (no DB)
go test ./internal/middleware/...

# Full suite (requires PARTRACK_TEST_DB_URL)
PARTRACK_TEST_DB_URL="postgres://..." go test -p 1 -count=1 \
  ./internal/worker/... ./internal/handler/...
```

`-p 1` is required when running multiple DB-dependent packages together to prevent concurrent
`Truncate` calls from racing.

---

## 12. Build & Deployment

### Build Commands

```bash
go build ./...                      # compile all packages
go build -o partrack ./cmd/partrack # build binary
go run ./cmd/partrack               # run (requires config.yaml or env vars)
sqlc generate                       # regenerate internal/db/gen/ after SQL changes
go mod tidy                         # sync dependencies

cd web && npm install               # install frontend dependencies
cd web && npm run dev               # dev server (proxies /api → localhost:8080)
cd web && npm run build             # production build → web/dist/
```

### Configuration

The binary is configured by `config.yaml` (path: `$PWD/config.yaml` by default, overridable via
`PARTRACK_CONFIG`) with environment variable overrides using the `PARTRACK_` prefix:

| Key | Env var | Description |
|---|---|---|
| `server.listen_addr` | `PARTRACK_LISTEN_ADDR` | HTTP listen address (default `:8080`) |
| `database.url` | `PARTRACK_DB_URL` | PostgreSQL connection string |
| `auth.session_secret` | `PARTRACK_SESSION_SECRET` | 32+ byte secret for session signing |
| `auth.oidc[].issuer` | — | OIDC provider issuer URL |
| `auth.oidc[].client_id` | — | OIDC client ID |
| `auth.oidc[].client_secret` | — | OIDC client secret |
| `auth.oidc[].auto_provision` | — | Auto-create users on first OIDC login |

### Deployment Model

- **Target**: single binary + external PostgreSQL instance
- **TLS**: handled by a reverse proxy in front of the application (Caddy, nginx, etc.)
- **Schema migrations**: run automatically on startup — safe to deploy with the binary
- **Static assets**: the Vite build output (`web/dist/`) is embedded into the Go binary via
  `//go:embed`; no separate static file server required

### First-Run Bootstrap

The first admin user must be set directly in the database after initial startup creates the tables:
```sql
-- After creating the user via the login UI or API:
UPDATE users SET is_admin = TRUE WHERE email = 'admin@example.com';
```

---

## 13. Key Architectural Decisions

These decisions are finalised and should not be revisited without a strong reason.

| # | Decision | Rationale |
|---|---|---|
| 1 | Parts are never deleted | Preserves historical BOM/order/inventory references |
| 2 | Quantity on hand = sum of stock_adjustments | Full audit trail; always reconcilable |
| 3 | One Inventory Entry per Part per Team | Team-scoped preferences independent of global catalog |
| 4 | One Shopping Cart per Team | Shared staging area; items merged from all sources |
| 5 | Build quantity is ephemeral | Not stored on Project; recorded on Build when assembly happens |
| 6 | Part parameters from multiple sources coexist | User-specified always wins; `preferred_parameter_source` resolves distributor conflicts |
| 7 | Order generation selection logic is deferred | Inputs are captured; algorithm TBD |
| 8 | No SvelteKit | SSR/SSG not needed; plain Svelte + Vite is simpler for a Go-API-backed SPA |
| 9 | No external job broker | In-process channel queue is sufficient for a single-binary deployment |
| 10 | Token hashes only stored | Raw tokens are shown once and never retrievable; SHA-256 in DB |
| 11 | Roles are strictly hierarchical | No per-user permission overrides; each role is a strict superset |
| 12 | No clustering | Single process; all caches and event routing are in-process |
