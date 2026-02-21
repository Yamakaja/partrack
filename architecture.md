# partrack — System Architecture

> This document is a living record of architectural decisions made during the design phase.
> It will grow as the design sessions progress.

## Status: In Progress

---

## 1. System Overview

partrack is an electronics parts inventory tracking system for PCB designers. It manages the full lifecycle from BOM import through procurement and physical inventory management.

Detailed user stories are in [userstory.md](userstory.md).
The conceptual data model is in [datamodel.md](datamodel.md).

---

## 2. Guiding Principles

| Principle | Implication |
|---|---|
| **Performance** | Compiled backend; long-running operations are async with user-visible progress |
| **Small footprint** | No clustering, sharding, or distributed infrastructure; single binary deployment |
| **Safety** | Statically typed languages throughout; memory-safe runtimes |
| **Low complexity** | Avoid over-engineering; choose the simplest solution that satisfies requirements |

---

## 3. Backend Stack

### Language: Go

- Compiles to a self-contained native binary
- Statically typed; GC-based memory safety
- Goroutine concurrency model suits the I/O-heavy workload (distributor API calls, DB queries)
- Simple language; avoids framework lock-in

### HTTP Layer: Chi

Thin router built on `net/http`. Uses standard `http.Handler` interfaces; no reflection-heavy middleware. Composable middleware chain for auth, logging, and error handling.

### Database: PostgreSQL

- Connection pooling via `pgxpool`
- Deployment assumption: external PostgreSQL instance; application connects over a standard connection string

### Query Layer: sqlc

sqlc compiles annotated SQL query files into type-safe Go code. Queries are plain SQL; the generated Go is fully type-checked at compile time.

Schema migrations managed by `golang-migrate`.

### Long-running Operations

Distributor API calls (BOM enrichment, price/availability refresh) run in in-process goroutines using a channel-based work queue. No external broker or worker process is required.

Progress is pushed to the frontend over **Server-Sent Events (SSE)**. This covers BOM import status, part enrichment progress, and order status polling.

---

## 4. Authentication & Identity

### Authentication Methods

Three authentication paths are supported:

| Method | Description |
|---|---|
| **OIDC** | Primary method. Delegates credential verification to a configured external identity provider. |
| **Internal username/password** | Fallback for environments without an OIDC provider. Passwords stored as bcrypt hashes. |
| **API tokens** | User-generated long-lived bearer tokens for programmatic access (scripts, integrations). |

### OIDC Integration

- Library: `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2`
- Provider configuration (issuer URL, client ID, client secret) is stored in the application config file, not the database
- On successful OIDC login, the `sub` claim is used to identify/create the local User record
- A User may be linked to an OIDC identity and/or have a local password (accounts can exist under either or both)
- Auto-provisioning is configured per-provider in the config file via `auto_provision` (bool) and an optional `default_team_id`. See `datamodel.md` decision #12 for full behaviour.

### Session Management

- After any successful login (OIDC or internal), an opaque session token (cryptographically random bytes) is issued
- The token hash (SHA-256) is stored in the database; only the raw token is returned to the client
- Sessions expire; sliding expiry is supported
- Delivered as an HTTP-only, Secure cookie for browser clients; `Authorization: Bearer` for API clients

### API Tokens

- Created by users with a name/description and optional expiry
- Only the raw token is shown once at creation time; the hash is stored
- Passed as `Authorization: Bearer <token>` on each request
- `last_used_at` is updated on use (for auditability and cleanup)

### Roles & Permissions

Team Membership uses four strictly ordered roles. See `datamodel.md` (Team Membership entity and decision #15) for the full permission boundary table.

| Role | Scope |
|---|---|
| `viewer` | Read-only |
| `member` | Inventory + assembly operations |
| `purchaser` | `member` + full procurement lifecycle |
| `admin` | `purchaser` + team/user management + distributor configuration |

---

## 5. Frontend Stack

### Framework: Svelte + TypeScript

- Svelte compiles to plain JavaScript at build time — no framework runtime is shipped to the browser
- TypeScript satisfies the "statically typed" principle on the frontend
- Simpler reactivity model than React/Vue: no virtual DOM, no hook rules
- Standard browser `EventSource` API handles SSE (BOM import progress, enrichment status, order polling) without any library

### Build: Vite

Fast development server and optimised production bundler. Minimal configuration. TypeScript support built in.

**Not SvelteKit**: SvelteKit is designed for SSR/SSG. Using it as a pure SPA would require `adapter-static` and adds unnecessary complexity. Plain Svelte + Vite with a client-side router is the right shape for a Go-API-backed application.

### Deployment Integration

The Vite production build outputs a static `dist/` directory. This is embedded directly into the Go binary using `//go:embed`. The Go server:

- Serves static assets from the embedded filesystem for all non-API routes
- Falls back to `index.html` for unknown paths (enabling client-side routing)

Single binary deployment is fully preserved — no separate static file server required.

---

## 6. API

REST over HTTP. JSON request and response bodies. Standard HTTP status codes.

The API serves both the web frontend and external integrations (via API tokens).

The full endpoint reference is in [api.md](api.md). Key conventions:

- Base path: `/api/v1`
- Team-scoped resources: `/api/v1/teams/{teamId}/…`
- Auth: HTTP-only session cookie (browser) or `Authorization: Bearer` header (API tokens / programmatic)
- Pagination: `?limit=N&offset=N` with `{ total, limit, offset, items }` envelope
- Monetary values: JSON strings (`"10.4500"`) to avoid float precision loss
- Async operations (BOM import, part enrichment, listing refresh): `202 { job_id }` + SSE progress stream

---

## 7. Deployment

- **Target:** self-hosted single binary + external PostgreSQL
- **Packaging:** Docker Compose packaging is a future concern; the binary is designed to be container-friendly (config via environment variables / config file, structured logging to stdout)
- **Reverse proxy assumption:** TLS termination and any network-level security handled by the reverse proxy in front of the application. The application itself does not handle TLS.
