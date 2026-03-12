# Partrack REST API

All endpoints are prefixed with `/api/v1`. Auth: session cookie or `Authorization: Bearer <token>`.

## Roles (team-scoped, ordered)

`viewer` < `member` < `purchaser` < `admin`

---

## Public (no auth)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/categories` | All categories (flat list; build tree from `parent_id`) |
| GET | `/parameter-definitions` | All canonical parameter definitions |

---

## Auth

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/auth/login` | — | `{email, password}` → session cookie |
| GET | `/auth/oidc/{provider}` | — | Redirect to OIDC provider |
| GET | `/auth/oidc/{provider}/callback` | — | OIDC code exchange → redirect to `/` |
| GET | `/auth/reset-password?token=` | — | Validate reset token; returns `{display_name}` |
| POST | `/auth/reset-password` | — | `{token, password}` → complete reset, log in |
| POST | `/auth/logout` | session | Invalidate session |
| GET | `/auth/me` | session | Current user + team memberships |

---

## Sessions & Tokens

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/sessions` | auth | List active sessions |
| DELETE | `/sessions/{sessionId}` | auth | Revoke session |
| GET | `/tokens` | auth | List API tokens |
| POST | `/tokens` | auth | `{name, expires_at?}` → `{token}` (shown once) |
| DELETE | `/tokens/{tokenId}` | auth | Revoke token |

---

## Preferences

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/preferences` | auth | Get user preferences blob |
| PUT | `/preferences` | auth | Replace preferences blob (must be JSON object) |

---

## Admin (instance admin only)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/admin/users` | List users; query: `limit`, `offset` |
| POST | `/admin/users` | `{display_name, email, password?}` |
| GET | `/admin/users/{userId}` | User details |
| PATCH | `/admin/users/{userId}` | `{display_name?, email?, is_active?, is_admin?}` |
| POST | `/admin/users/{userId}/set-password` | `{password}` |
| POST | `/admin/users/{userId}/password-reset-link` | Returns `{path, expires_at}` (24h) |
| GET | `/admin/parameter-definitions` | List parameter definitions |
| POST | `/admin/parameter-definitions` | `{slug, display_name, unit?, unit_symbol?, value_type}` |
| PATCH | `/admin/parameter-definitions/{id}` | Update definition fields |
| GET | `/admin/distributor-parameter-mappings` | Query: `distributor_id?`, `unmapped=true?` |
| PATCH | `/admin/distributor-parameter-mappings/{id}` | `{parameter_id?, ignored?}` |

---

## Global (authenticated)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/teams` | Teams the user is member of (admins see all) |
| POST | `/teams` | `{name, description?}` — admin only |
| GET | `/users/search?q=` | Search active users (min 2 chars, max 10 results) |
| GET | `/distributors` | All distributors with `api_type` and OAuth support flag |
| GET | `/distributors/{distributorId}/credential-spec` | Required credential fields |
| GET | `/parts` | List parts; query: `team_id?`, `q?`, `category_id?`, `package?`, `mpn?`, `lifecycle_status?`, `scope?`, `stock_status?`, param filters, `limit`, `offset` |
| POST | `/parts` | `{mpn?, description?, manufacturer?, lifecycle_status?, category_id?, package?, short_description?, product_url?, datasheet_url?, preferred_parameter_source?}` |
| GET | `/parts/{partId}` | Part details |
| PATCH | `/parts/{partId}` | Update part fields |
| GET | `/parts/{partId}/parameters` | User + distributor-sourced parameters |
| POST | `/parts/{partId}/parameters` | `{name, value, unit?}` — create/upsert user parameter |
| DELETE | `/parts/{partId}/parameters/{parameterId}` | Delete user parameter |
| GET | `/parts/{partId}/listings` | Distributor listings for part |
| GET | `/categories/{categoryId}/parameters` | Parameter definitions in category + descendants; query: `team_id?` |

---

## Team-scoped (`/teams/{teamId}/...`)

### Team (admin)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/` | viewer | Team details |
| PATCH | `/` | admin | `{name?, description?}` |
| GET | `/dashboard` | viewer | Stats, recent activity, alerts, outstanding orders, build pipeline |
| GET | `/members` | admin | List members with roles |
| POST | `/members` | admin | `{user_id, role}` |
| PATCH | `/members/{userId}` | admin | `{role}` |
| DELETE | `/members/{userId}` | admin | Remove member |
| POST | `/members/{userId}/deactivate` | admin | Globally deactivate user; clears sessions/tokens |
| GET | `/distributor-configs` | admin | List configs (secrets omitted) |
| PUT | `/distributor-configs/{distributorId}` | admin | `{enabled, preferred, cache_duration_seconds, api_credentials?}` |
| DELETE | `/distributor-configs/{distributorId}` | admin | Delete config + OAuth tokens |

### Inventory

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/inventory` | viewer | List entries; query: `q?`, `storage_location_id?`, `below_min_stock=true?`, `limit`, `offset` |
| GET | `/inventory/replenish-preview` | viewer | Preview cart items needed to meet reorder levels |
| GET | `/inventory/{entryId}` | viewer | Entry details |
| GET | `/inventory/{entryId}/adjustments` | viewer | Stock adjustment ledger; query: `limit`, `offset` |
| POST | `/inventory` | member | `{part_id, preferred_listing_id?, storage_location_id?, min_stock_qty?, reorder_qty?}` |
| PATCH | `/inventory/{entryId}` | member | `{preferred_listing_id?, storage_location_id?, min_stock_qty?, reorder_qty?}` |
| POST | `/inventory/{entryId}/adjustments` | member | `{delta, reason?}` — manual stock adjustment |

### Storage Locations

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/storage-locations` | viewer | List locations |
| POST | `/storage-locations` | member | `{label, description?}` |
| PATCH | `/storage-locations/{locationId}` | member | `{label?, description?}` |
| DELETE | `/storage-locations/{locationId}` | member | Delete location |

### Projects & BOMs

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/projects` | viewer | List; query: `status?`, `limit`, `offset` |
| GET | `/projects/{projectId}` | viewer | Project + active revision summary |
| GET | `/projects/{projectId}/revisions` | viewer | BOM revisions |
| GET | `/projects/{projectId}/revisions/{revId}` | viewer | Revision details |
| GET | `/projects/{projectId}/revisions/{revId}/substitutes` | viewer | Approved substitutes for revision |
| GET | `/projects/{projectId}/revisions/{revId}/stock-check` | viewer | Stock check; query: `build_quantity` (required) |
| GET | `/parts/categories` | viewer | Categories with part counts for team |
| POST | `/projects` | member | `{name, description?, status?, preferred_distributor_id?, source_url?, project_manager_id?, bom_import_config?}` |
| PATCH | `/projects/{projectId}` | member | Update project fields |
| POST | `/projects/{projectId}/revisions` | member | Import BOM; `{label, notes?, set_active, bom_import_config?, items: [{reference_designators, quantity, raw_description?, manufacturer?, mpn?, name?, sku?, distributor_name?, resolved_distributor_id?}]}` |
| PATCH | `/projects/{projectId}/revisions/{revId}` | member | `{label?, notes?, set_active?}` |
| PATCH | `/projects/{projectId}/revisions/{revId}/items/{itemId}` | member | `{resolved_part_id?, ignored?}` |
| POST | `/projects/{projectId}/revisions/{revId}/items/{itemId}/enrich` | member | `{sku, distributor_id}` → 202 `{job_id}` |
| POST | `/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes` | member | `{part_id}` |
| DELETE | `/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes/{partId}` | member | Remove substitute |

### Parts (team operations)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/parts/{partId}/allocations` | viewer | Build allocations for part across pending/ordered builds |
| POST | `/parts/import` | member | `{sku, distributor_id}` → `{part_id}` — atomic import: part + listings + inventory |
| POST | `/parts/enrich-all` | admin | Enqueue enrichment for all parts with listings → 202 `{job_count}` |
| POST | `/parts/{partId}/enrich` | member | `{sku, distributor_id}` → 202 `{job_id}` |
| POST | `/parts/{partId}/listings` | member | `{sku, distributor_id}` → 202 `{job_id}` |
| DELETE | `/parts/{partId}/listings/{listingId}` | member | Remove listing + distributor-sourced parameters |
| POST | `/parts/{partId}/listings/refresh` | member | Enqueue refresh for all listings → 202 `{job_count}` |
| POST | `/parts/{partId}/listings/{listingId}/refresh` | member | → 202 `{job_id}` |

### Distributor Search

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/distributor-configs/{distributorId}/search` | viewer | Query: `keywords` (required), `limit` (max 50), `offset` → `{items, total, exact_match_indices}` |

### Builds

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/builds` | viewer | List; query: `project_id?`, `status?`, `created_by?`, `sort_by?`, `sort_dir?`, `limit`, `offset` |
| GET | `/builds/pipeline` | viewer | Builds grouped by status |
| GET | `/builds/{buildId}` | viewer | Build details |
| POST | `/builds/preview` | member | `{bom_revision_id, quantity, substitute_selections?}` → preview deductions without persisting |
| POST | `/builds` | member | `{project_id, bom_revision_id, quantity, notes?}` → draft build |
| POST | `/builds/{buildId}/submit` | member | draft→pending; recomputes cart shortfalls |
| POST | `/builds/{buildId}/checkout` | member | pending→assembled; `{assembled_quantity?, substitute_selections?, allow_stock_reconciliation?}` |
| PATCH | `/builds/{buildId}` | member | `{quantity?, notes?}` (draft/pending only) |
| DELETE | `/builds/{buildId}` | member | Cancel build; release allocations |
| PATCH | `/builds/{buildId}/cart-inclusion` | purchaser | `{excluded: bool}` |

### Cart

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/cart/count` | purchaser | `{count}` |
| GET | `/cart/part-quantity` | purchaser | Quantity for specific part |
| GET | `/cart` | purchaser | Full cart with items and listings |
| DELETE | `/cart` | purchaser | Clear all items |
| POST | `/cart/items` | purchaser | `{part_id, quantity, listing_id?, source?}` |
| PATCH | `/cart/items/{itemId}` | purchaser | `{quantity?, listing_id?}` |
| DELETE | `/cart/items/{itemId}` | purchaser | Remove item |
| POST | `/cart/add-low-stock` | purchaser | Add items to reach reorder levels |
| POST | `/cart/convert` | purchaser | Convert to orders; groups by distributor; transitions pending builds→ordered |

### Orders

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/orders` | viewer | List; query: `status?`, `distributor_id?`, `limit`, `offset` |
| GET | `/orders/{orderId}` | viewer | Order + line items |
| GET | `/orders/{orderId}/distributor-status` | viewer | Real-time status from distributor API |
| POST | `/orders/import` | purchaser | Import order from file (CSV/JSON) |
| PATCH | `/orders/{orderId}` | purchaser | `{notes?, purchase_order_number?}` |
| DELETE | `/orders/{orderId}` | purchaser | Cancel order; release build allocations |
| GET | `/orders/{orderId}/export` | purchaser | Export as CSV |
| POST | `/orders/{orderId}/submit` | purchaser | `{submission_method?}` — submit or mark submitted |
| POST | `/orders/{orderId}/reference` | purchaser | `{external_reference}` — link tracking number |
| POST | `/orders/{orderId}/items/{itemId}/receive` | purchaser | `{quantity_received, arrived_at?}` — allocates to builds first |
| POST | `/orders/{orderId}/push-to-distributor` | purchaser | → 202 `{job_id}` |
| POST | `/orders/{orderId}/request-quote` | purchaser | → 202 `{job_id}` |

### OAuth (distributor)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/oauth/{distributorApiType}/initiate` | purchaser | `{auth_url}` — start OAuth flow |
| GET | `/oauth/{distributorApiType}/status` | purchaser | Token status |
| DELETE | `/oauth/{distributorApiType}/token` | purchaser | Disconnect OAuth |
| GET | `/oauth/callback/{distributorApiType}` | — | OAuth2 callback (no auth required) |

### Checkouts

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/checkouts` | viewer | List; query: `limit`, `offset` |
| GET | `/checkouts/{checkoutId}` | viewer | Checkout + adjustments |
| POST | `/checkouts` | member | `{reason?, notes?, items: [{inventory_entry_id or part_id, quantity, reconcile_to?}]}` |

### Search & Events

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/search?q=` | viewer | Full-text search across parts, projects, orders, builds, storage (min 2 chars) |
| GET | `/events` | viewer | SSE stream; events: `part_changed`, `inventory_changed`, `cart_changed`, `order_changed`, `storage_changed`, `project_changed`, `revision_changed`, `build_changed` |

---

## Standard Responses

**List:** `{total, limit, offset, items: [...]}`

**Error:** `{error: "message", code: "ERROR_CODE"}`

**Async (202):** `{job_id: "uuid"}` — progress via SSE

---

## Build Lifecycle

`draft` → `pending` → `ordered` → `ready` → `assembled`

- `POST /builds` creates draft (no stock deduction)
- `submit` → pending; recomputes cart shortfalls
- Cart `convert` → ordered (or ready if already in stock)
- `checkout` → assembled; applies stock deductions
