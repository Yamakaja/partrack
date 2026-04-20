# partrack

**Electronics parts inventory tracker for PCB design teams**

[![License: LGPL v3](https://img.shields.io/badge/License-LGPL_v3-blue.svg)](LICENSE.md)

partrack tracks parts inventory for PCB design teams. It covers the full lifecycle from BOM import through stock checking, purchasing, and assembly tracking — with team-based access control and distributor API integration.

## How It Works

```
                 ┌──────────────────────────────────────────────────────────┐
                 │                      PROJECT                             │
                 │                                                          │
  BOM (CSV) ────►  Import & resolve ────► BOM Revision (line items)         │
                 │       │                     │                            │
                 │       │  match by MPN,      │  specify build             │
                 │       │  SKU, or API        │  quantity                  │
                 │       ▼                     ▼                            │
                 │  Parts Catalog         Stock Check                       │
                 │  (parameters,          (in stock / partial / missing)    │
                 │   lifecycle,                │                            │
                 │   datasheets)               │                            │
                 └─────────────────────────────┼────────────────────────────┘
                                               │
               ┌───────────────────────────────┼──────────────────────┐
               │                    TEAM       │                      │
               │                               ▼                      │
               │  Manual additions ────► Shopping Cart ◄──── Restock  │
               │                               │              scan    │
               │  Build (reserve) ─────────┐   │                      │
               │                           │   ▼                      │
               │                           │  Orders (by distributor) │
               │                           │   │                      │
               │                           │   ▼                      │
               │  Inventory ◄──────────────┴─ Receive ◄──── Mobile    │
               │  (ledger, storage               │          Scanner   │
               │   locations, min stock)         │          (QR auth) │
               │                                 ▼                    │
               │                           Build ──► Kitting ──► Assemble │
               │                          (reserve)   (scanner)  (deduct) │
               └──────────────────────────────────────────────────────┘
```

## Features

### BOM Import & Resolution

Upload a CSV from KiCad, Altium, or any EDA tool. partrack maps your columns and resolves each line item to a part in the catalog — first by exact MPN match, then by distributor SKU, and finally by querying distributor APIs. Unresolved items can be matched manually. Projects support multiple BOM revisions so you can track changes across board spins.

### Stock Checks

Enter a build quantity and see which parts are in stock, which are short, and by how much. Stock checks account for approved substitutes, so if an alternate part has available inventory, it counts toward coverage. Shortfalls can be sent to the shopping cart in one action, with minimum reorder quantities applied automatically.

### Build Planning

Register a build before you're ready to assemble. Once submitted, pending builds feed into the team's shopping cart so the purchasing team can see upcoming demand. When the cart is converted to orders, parts are allocated to builds automatically — and as orders are received, incoming stock is assigned to the builds that need it. If a BOM line item has approved substitutes, you choose which part to pull from at checkout time.

### Shopping Cart & Orders

The team shares a single persistent cart. Items come from BOM shortfalls, low-stock replenishment scans, or manual additions. When you're ready to order, the cart groups items by distributor and generates draft orders. Orders can be submitted directly via distributor API, exported as CSV for manual placement, or linked to an externally placed order by reference number. Receiving is per line item and supports partial deliveries. DigiKey orders can be imported directly — partrack fetches the line items and links them to open builds automatically.

### Inventory Tracking

Every stock change — receipt, build, withdrawal, manual correction — is recorded as an immutable ledger entry. Quantity on hand is the running total, always reconcilable against the full history. Each part in a team's inventory has a storage location, a preferred distributor listing, and configurable minimum stock and reorder quantity thresholds.

### Storage Locations

Storage locations support a grid coordinate system for physical organisation of bins, drawers, and shelves. Each location has an optional grid layout so you can track exactly which cell within a storage unit holds a given part. The mobile scanner can scan location barcodes to show everything stored there and reassign parts to new locations.

### Parts Catalog

Parts store manufacturer info, lifecycle status, datasheets, categories, and electrical/mechanical parameters. Data can be pulled from distributor APIs — DigiKey, Mouser, and LCSC are supported, with the integration designed to add more. When multiple sources report different parameter values, you can set a preferred source or override with your own.

### Part Substitutes

Designate approved replacement parts per BOM line item. Stock checks and order calculations cover all approved alternatives, so you can take advantage of whatever is available. Substitutes can be imported from prior BOM revisions when parts carry over.

### Mobile Scanner

A dedicated mobile interface at `/scan` turns a phone into a handheld scanner for warehouse-style inventory work. Sessions are authenticated by scanning a QR code generated from the desktop — no login required on the phone. Once connected, the scanner offers:

- **Bag scan** — scan a distributor barcode on a parts bag; partrack resolves the SKU to a known part or opens the listing picker to import a new one and set the initial stock count.
- **Receive order** — work through an open order line by line, scanning bags and recording received quantities into the correct storage location.
- **Find part** — search the catalog by name or MPN, then view stock, location, reservations, and make stock adjustments directly.
- **Location inventory** — scan a location barcode to see all parts stored there and navigate to individual parts.
- **Storage assignment** — reassign a part from one location to another by scanning both.
- **Build kitting** — pick components for a specific build; the desktop initiates a kitting session and the scanner guides the operator through each line item.

The mobile UI is optimised for one-handed use with large touch targets, pinch-zoom prevention, and a stack-based navigation model that mirrors a native app feel. Scan activity is recorded in a persistent audit log visible from the desktop.

### Team Access Control

Four role levels control what team members can do:

- **Viewer** — read-only access to inventory, projects, and orders
- **Member** — inventory operations: BOM import, builds, checkouts, stock adjustments
- **Purchaser** — full procurement: cart management, ordering, and receiving
- **Admin** — team management, user roles, and distributor configuration

Users can belong to multiple teams with separate roles in each. A system-level admin role handles user management and global configuration.

### Authentication

partrack supports OpenID Connect (Google, Okta, Keycloak, etc.) with optional auto-provisioning of new users on first login. Internal username/password auth is available as a fallback. API tokens provide access for scripts and integrations.

## Deployment

### Docker Compose

All Docker files live in the `docker/` directory. Copy `.env.example` to `.env` and fill in the required values, then:

```bash
docker compose -f docker/docker-compose.yml up --build
```

The database image is a custom Arch Linux build that includes the `pg_uuidv7` extension (required for time-ordered primary keys). It is built automatically by Compose — no manual setup needed.

Required environment variables (set in `.env` or the shell):

| Variable | Description |
|---|---|
| `PARTRACK_DB_PASSWORD` | Postgres password |
| `PARTRACK_SESSION_SECRET` | Random secret for session signing |
| `PARTRACK_APP_BASE_URL` | Public URL (e.g. `https://partrack.example.com`) |

Optional:

| Variable | Default | Description |
|---|---|---|
| `PARTRACK_PORT` | `8080` | Host port to expose |

Database migrations run automatically on startup. The compiled frontend is embedded in the Go binary — no separate asset server needed.

**OIDC providers** cannot be configured via environment variables alone — register them in a `config.yaml` and mount it into the container:

```yaml
# config.yaml
oidc:
  - name: google
    issuer: https://accounts.google.com
    client_id: 1234567890-abc.apps.googleusercontent.com
    # client_secret set via PARTRACK_OIDC_GOOGLE_SECRET env var
    auto_provision: true
    default_team_id: 00000000-0000-0000-0000-000000000001
```

Uncomment the `volumes` and `PARTRACK_CONFIG` entries in `docker/docker-compose.yml` and point them at your config file.

### Standalone Binary

```bash
# Build frontend
cd web && npm install && npm run build && cd ..

# Build binary
go build -o partrack ./cmd/partrack

# Run
PARTRACK_DB_URL=postgres://user:pass@host/partrack \
PARTRACK_SESSION_SECRET=a-long-random-string \
PARTRACK_APP_BASE_URL=https://partrack.example.com \
./partrack
```

### Configuration

Configuration is loaded from a YAML file (default: `./config.yaml`, override with `PARTRACK_CONFIG`) with environment variable overrides.

| Option | YAML key | Environment variable | Default |
|---|---|---|---|
| Server address | `server.listen_addr` | `PARTRACK_LISTEN_ADDR` | `:8080` |
| Database URL | `database.url` | `PARTRACK_DB_URL` | — (required) |
| Session secret | `auth.session_secret` | `PARTRACK_SESSION_SECRET` | — (required) |
| App base URL | `app.base_url` | `PARTRACK_APP_BASE_URL` | — (required for OIDC) |
| Log level | `log.level` | — | `info` |

OIDC providers are configured in the YAML file:

```yaml
oidc:
  - name: google
    issuer: https://accounts.google.com
    client_id: 1234567890-abc.apps.googleusercontent.com
    # client_secret set via PARTRACK_OIDC_GOOGLE_SECRET
    auto_provision: true
    default_team_id: 00000000-0000-0000-0000-000000000001
```

See [getting-started.md](getting-started.md) for full setup details including nginx/TLS and OIDC provider setup.

### Reverse Proxy

partrack does not handle TLS itself — use a reverse proxy (nginx, Caddy, etc.) in front of it. A few things to keep in mind:

**SSE requires disabled buffering.** partrack uses Server-Sent Events for real-time job progress (BOM import, part enrichment). The backend already sets `X-Accel-Buffering: no`, but you should also configure the proxy to avoid buffering and to allow long-lived connections:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # Required for SSE
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 3600s;
}
```

**Set `app.base_url` to your public URL.** OIDC redirect URLs and distributor OAuth callbacks are constructed from this value. If it's missing or wrong, login flows will fail with redirect mismatches.

**No separate static file server needed.** The frontend is embedded in the binary, and the backend handles SPA fallback routing internally. Proxy all paths to the backend.

---

## Development

### Guiding Principles

| Principle | What it means in practice |
|---|---|
| **Small footprint** | Single binary, single Postgres instance. No clustering, no message broker, no external cache. |
| **Performance** | Compiled Go backend; long-running operations (BOM import, distributor API calls) run asynchronously with real-time progress via SSE. |
| **Safety** | Statically typed end-to-end (Go + TypeScript). Parts are never deleted — lifecycle status preserves history. Stock quantities are derived from an immutable ledger. |
| **Low complexity** | Simplest solution that satisfies the requirement. In-process job queue, plain SQL via sqlc, no ORM, no framework lock-in. |

### Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.25+ | `go version` |
| Node.js | 20 LTS+ | `node --version` |
| npm | 10+ | bundled with Node |
| PostgreSQL | 15+ | local, Docker, or managed |
| sqlc | latest | only needed when editing SQL queries |

### Getting Started

```bash
# 1. Create the database (migrations run automatically on startup)
createdb partrack

# 2. Configure the server
cat > config.yaml <<EOF
database:
  url: postgres://localhost/partrack?sslmode=disable
auth:
  session_secret: change-me-in-production
EOF

# 3. Start the backend (Terminal 1)
go run ./cmd/partrack

# 4. Start the frontend dev server (Terminal 2)
cd web && npm install && npm run dev
```

The frontend dev server runs on `http://localhost:5173` and proxies `/api` to the backend on `:8080`.

### Tech Stack

| Layer | Technology |
|---|---|
| Language | Go 1.25 |
| HTTP router | [Chi](https://github.com/go-chi/chi) v5 |
| Database | PostgreSQL 15+ |
| Query layer | [sqlc](https://sqlc.dev) — type-safe SQL code generation |
| Migrations | [golang-migrate](https://github.com/golang-migrate/migrate) — auto-run at startup |
| Distributors | DigiKey, Mouser, LCSC (pluggable `distributor.Client` interface) |
| Auth | go-oidc v3, bcrypt, signed session cookies |
| Frontend | Svelte 5 + TypeScript |
| Build tool | Vite 7 |
| Styling | Tailwind CSS v4 |
| UI components | bits-ui v2 (headless, accessible) |
| Async state | TanStack Svelte Query v6 |
| Real-time | Server-Sent Events (native `EventSource`) |

### Project Layout

```
cmd/partrack/        main entrypoint
internal/
  config/            config loading (YAML + env overrides)
  db/                pgxpool setup + migrations; gen/ holds sqlc output
  server/            Chi router + route registrations
  middleware/        auth (session/token) + team role enforcement
  handler/           HTTP handlers, one file per domain
  worker/            background job queue + SSE event hub
  distributor/       distributor API client interface + implementations
db/
  migrations/        SQL migration files (up/down pairs)
  queries/           sqlc query definitions
web/                 Svelte frontend (embedded into Go binary at build time)
```

### Documentation

| Document | Purpose |
|---|---|
| [getting-started.md](getting-started.md) | Full setup guide: database, config, dev workflow, production deployment, nginx, OIDC |
| [doc/userstory.md](doc/userstory.md) | User stories across 6 epics — defines the full feature scope |
| [doc/datamodel.md](doc/datamodel.md) | Conceptual data model: entities, relationships, and design decisions |
| [doc/architecture.md](doc/architecture.md) | Technical architecture: project layout, patterns, key decisions |
| [doc/api.md](doc/api.md) | Full REST API reference: endpoints, roles, request/response shapes |

## Contributing

Issues and pull requests are welcome. Please open an issue first for significant changes.

## License

partrack is licensed under the [GNU Lesser General Public License v3.0](LICENSE.md).
