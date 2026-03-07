# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Status

partrack has completed the architecture and design phase and is now in **active implementation**. The core parts catalog, distributor listings, BOM import pipeline, and distributor enrichment API are implemented.

## Build & Run

```bash
go build ./...                    # compile all packages
go build -o partrack ./cmd/partrack  # build the binary
go run ./cmd/partrack             # run directly (requires config.yaml or env vars)
go mod tidy                       # sync dependencies
```

Runtime requires:
- `PARTRACK_DB_URL=postgres://...` or `database.url` in `config.yaml`
- Migrations run automatically at startup via golang-migrate

## Tests

```bash
PARTRACK_TEST_DB_URL=postgres://partrack:partrack@test-postgres/partrack-test go test -p 1 ./...
```

- Tests require a live Postgres instance; use `-p 1` to run packages serially and avoid deadlocks from concurrent table truncation.
- The test DB URL is `postgres://partrack:partrack@test-postgres/partrack-test`.

## Frontend

```bash
cd web
npm run dev      # Vite dev server (proxies /api to Go backend)
npm run build    # production build → web/dist/
npm run check    # svelte-check + tsc (type-check without emitting)
```

Stack: Svelte 5 + Vite + Tailwind CSS v4 + bits-ui v2 + TanStack Query v6. The dist output is embedded into the Go binary via `web/web.go`.

## Code Layout

```
cmd/partrack/        # main entrypoint
internal/
  config/            # Config struct; YAML + env override loading
  db/                # pgxpool setup (pool.go) + migrate runner (migrate.go); gen/ holds sqlc output
  server/            # Chi router construction (server.go) + all route registrations (routes.go)
  middleware/        # auth.go (session/token → User in ctx), team.go (min-role enforcement)
  handler/           # HTTP handlers; one file per domain group; all share handler.Handler struct
  worker/            # channel-based job queue + per-team SSE hub
  distributor/       # Client interface + Registry; digikey/ and testclient/ are implementations
db/
  db.go              # exports embed.FS for migrations (used by internal/db/migrate.go)
  migrations/        # golang-migrate SQL files (up/down pairs, numbered sequentially)
  queries/           # sqlc .sql query files
web/
  web.go             # exports embed.FS for frontend dist/
  dist/              # Vite build output (git-ignored; .gitkeep placeholder committed)
sqlc.yaml            # sqlc code-gen config (input: db/queries, output: internal/db/gen)
```

## Key Patterns

- All handler methods live on `handler.Handler` (holds DB pool, Config, Worker queue). Add DB queries by writing SQL in `db/queries/`, running `sqlc generate`, then calling the generated types from handlers.
- Authentication middleware (`middleware.Authenticate`) injects `*middleware.User` into the request context. Retrieve it with `middleware.UserFromContext(ctx)`.
- Team role enforcement: `middleware.RequireTeamRole("member")` etc. — injects `*middleware.TeamMembership` into ctx.
- Background jobs: `h.Worker.Enqueue(worker.JobBOMImport, teamID, payload)` returns a job ID. Progress events are published via `h.Worker.Publish(ev)` and consumed by SSE subscribers.
- **SSE entity-change events**: every successful mutation publishes a lightweight event (`part_changed`, `inventory_changed`, `cart_changed`, `order_changed`, `storage_changed`, `project_changed`) via `h.publishEvent(r, type, teamID, data)` in `internal/handler/handler.go`. The worker queue skips delivery to the acting user's own SSE channel (`worker.Event.UserID`). Async job completions (`handlePartEnrich`, `handleListingRefresh`, `handleBOMImport`) publish via `q.Publish` with `payload["user_id"]`. The frontend subscribes in `web/src/lib/cache/invalidation.ts` and calls `queryClient.invalidateQueries` on receipt.
- `writeJSON` / `writeError` / `userIDFromContext` / `publishEvent` helpers are in `internal/handler/handler.go`.
- Distributor integrations implement `distributor.Client` (`EnrichPart`, `RefreshListing`, `SearchParts`) and are registered in `distributor.Registry`. `EnrichPart` takes a **distributor SKU** (not MPN) — use `SearchParts` first for discovery. `POST /parts/{id}/enrich` requires `{"sku": "...", "distributor_id": "..."}` in the body plus `team_id` as a query param.
- `distributor_listings.price_breaks` is stored as JSONB (`[]distributor.PriceBreak`). `parts.lifecycle_status` is updated by enrichment via `SetPartEnriched` with a nullable lifecycle param (empty string = leave unchanged).

## UX Patterns

- See doc/ux.md

## Design Documents

Read these in order when onboarding to the project:

| File | Purpose |
|---|---|
| [userstory.md](userstory.md) | User stories organised into 6 epics. Defines what the system must do. |
| [datamodel.md](datamodel.md) | Conceptual data model — entities, relationships, and design decisions. Start here before touching any schema or data layer. |
| [architecture.md](architecture.md) | Living architecture document. Updated as design sessions progress. |
| [api.md](api.md) | Full REST API endpoint reference — methods, paths, roles, request/response shapes. |

## What partrack Does

An electronics parts inventory tracking system for PCB designers. Core workflow:

1. User imports a BOM from their PCB tool into a Project
2. System resolves BOM line items to Parts (via MPN or distributor SKU match, or distributor API lookup)
3. User sees stock status per line item and shortfalls
4. System generates order lists per distributor to cover shortfalls
5. User receives orders; inventory is updated and parts are enriched with distributor API data

## Key Data Model Concepts

The data model (`datamodel.md`) has no open design questions. Critical things to understand before writing code:

- **Part** (global or team-scoped) is a catalog entry — it exists independently of stock. Identified by MPN where possible (MPN may only be unique within a manufacturer, but are generally not unique globally). Never deleted; use lifecycle status instead.
- **Inventory Entry** is the team's *profile* for a Part — holds preferred storage location, preferred distributor listing, minimum stock level, reorder quantity, and the stock ledger. Created with zero quantity when a Part first becomes relevant to a team. Not the same as "has stock."
- **Stock Adjustment** is an immutable ledger entry; quantity on hand is a running total derived from these. Source of an adjustment is one of: Order Line Item (receipt), Build (PCB assembly), General Checkout (ad-hoc withdrawal), or null (manual correction).
- **Distributor Listing** is one Distributor × Part × Packaging Type combination. A single MPN can have many listings (different distributors, different packaging, different MOQ and fixed costs).
- **Part Parameters** from multiple distributor sources coexist on a single Part. User-specified values always win; a `preferred_parameter_source` on the Part resolves conflicts between distributor sources.
- **BOM Line Item** resolves to a primary Part plus zero or more approved substitute Parts. Stock checking covers all of them.
- **Build quantity is ephemeral** — it is a parameter at stock-check time, not stored on the Project. It is recorded on a **Build** entity when boards are actually assembled.
- **Shopping Cart** is the team's persistent staging area (one per team) for parts to be ordered, populated from BOM shortfall checks, inventory maintenance scans, or manual additions. Cart items are converted to Orders grouped by distributor.
- **Build** records a PCB assembly event (project + BOM revision + quantity built) and drives stock deductions.
- **General Checkout** records ad-hoc parts withdrawal not tied to a project.
- Distributor + SKU pairs can be considered unique identifiers of a listing, which itself is associated with a manufacturer and MPN.

## Architectural Constraints Already Decided

- Parts are never deleted (use lifecycle status: Active, NRND, Obsolete, Deprecated, No Longer Available)
- One storage location per Inventory Entry
- Part scope is `global` or `team` — no user-level scope
- Distributor Listing price/availability data is cached; cache duration is configurable per distributor per team in Distributor Config
- Order generation selection logic is intentionally deferred — the data needed for it is captured in Distributor Listing fields, Distributor Config preferred flag, and Inventory Entry preferred listing reference
- One Shopping Cart per Team; items are merged when added from multiple sources

# General Instructions

### Code Intelligence

Prefer LSP over Grep/Read for code navigation — it's faster, precise, and avoids reading entire files:
- `workspaceSymbol` to find where something is defined
- `findReferences` to see all usages across the codebase
- `goToDefinition` / `goToImplementation` to jump to source
- `hover` for type info without reading the file

Use Grep only when LSP isn't available or for text/pattern searches (comments, strings, config).

After writing or editing code, check LSP diagnostics and fix errors before proceeding.
