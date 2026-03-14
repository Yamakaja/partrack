# Partrack REST API

All endpoints are prefixed with `/api/v1`. Auth: session cookie or `Authorization: Bearer <token>`.

## Roles (team-scoped, ordered)

`viewer` < `member` < `purchaser` < `admin`

Higher roles inherit all permissions of lower roles.

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
| POST | `/auth/login` | — | `{email, password}` → session cookie + `{user}` |
| GET | `/auth/oidc/{provider}` | — | Redirect (302) to OIDC provider; sets CSRF state cookie |
| GET | `/auth/oidc/{provider}/callback` | — | OIDC code exchange → session cookie + redirect to `/` |
| GET | `/auth/reset-password?token=` | — | Validate reset token; returns `{display_name, email, expires_at}` |
| POST | `/auth/reset-password` | — | `{token, password}` → session cookie + `{user, memberships}` |
| POST | `/auth/logout` | session | Invalidate session; 204 |
| GET | `/auth/me` | session | Current user + team memberships |

### OAuth callbacks (no auth)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/oauth/callback/{distributorApiType}` | Distributor OAuth2 callback; exchanges code for tokens, redirects to `return_to` |

---

## Sessions & Tokens

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/sessions` | auth | List active sessions (`{total, limit, offset, items: [{id, created_at, expires_at, last_seen_at, user_agent, ip_address, is_current}]}`) |
| DELETE | `/sessions/{sessionId}` | auth | Revoke session (own sessions only); 204 |
| GET | `/tokens` | auth | List API tokens (`{total, limit, offset, items: [{id, name, created_at, last_used_at, expires_at}]}`) |
| POST | `/tokens` | auth | `{name, expires_at?}` → `{id, name, token, created_at, expires_at}` (raw token shown once, `ptk_` prefix); 201 |
| DELETE | `/tokens/{tokenId}` | auth | Revoke token (own tokens only); 204 |

---

## Preferences

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/preferences` | auth | `{preferences: {...}}` (opaque JSON object; `{}` if not set) |
| PUT | `/preferences` | auth | Replace preferences blob; body must be a JSON object (max 1 MiB) |

---

## Admin (instance admin only)

### User Management

| Method | Path | Description |
|--------|------|-------------|
| GET | `/admin/users` | List users; query: `limit` (max 200), `offset`. Response adds `has_password` field. |
| POST | `/admin/users` | `{display_name, email, password?}` → 201; password optional (user can be OIDC-only) |
| GET | `/admin/users/{userId}` | User details |
| PATCH | `/admin/users/{userId}` | `{display_name?, email?, is_active?, is_admin?}`. Deactivating clears all sessions and tokens. |
| POST | `/admin/users/{userId}/set-password` | `{password}` → 204 |
| POST | `/admin/users/{userId}/password-reset-link` | Returns `{path, expires_at}` (24h expiry, path is `/reset-password?token=...`) |

### Parameter Registry

| Method | Path | Description |
|--------|------|-------------|
| GET | `/admin/parameter-definitions` | List parameter definitions |
| POST | `/admin/parameter-definitions` | `{slug, display_name, unit?, unit_symbol?, value_type}` |
| PATCH | `/admin/parameter-definitions/{id}` | Update definition fields |
| GET | `/admin/distributor-parameter-mappings` | Query: `distributor_id?`, `unmapped=true?` |
| PATCH | `/admin/distributor-parameter-mappings/{id}` | `{parameter_id?, ignored?}` |

---

## Global (authenticated, no team scope)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/teams` | Teams the user is member of (admins see all) |
| POST | `/teams` | `{name, description?}` — admin only |
| GET | `/users/search?q=` | Search active users (min 2 chars, max 10 results) |
| GET | `/distributors` | All distributors with `api_type` and `supports_oauth` flag |
| GET | `/distributors/{distributorId}/credential-spec` | Required credential fields (`[{key, description, required, secret}]`; empty array if no API) |
| GET | `/categories/{categoryId}/parameters` | Parameter definitions in category + descendants; query: `team_id?` |

### Parts Catalog (global)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/parts` | List parts; query: `team_id?`, `q?`, `category_id?`, `package?`, `mpn?`, `lifecycle_status?`, `scope?` (`global`/`team`), `stock_status?`, `param_{id}_min/max`, `param_{id}_values`, `limit`, `offset`. When `category_id` set, includes parameters in response. When `team_id` set, includes inventory fields. |
| POST | `/parts` | Create part. Body: `{scope, team_id?, mpn?, manufacturer?, description?, category?, category_id?, package?, datasheet_url?, lifecycle_status?}`. `scope` required (`global` requires admin; `team` requires team membership + `team_id`). Returns 409 `PART_EXISTS` with existing part if MPN+manufacturer match found. 201. |
| GET | `/parts/{partId}` | Part details with parameters (user + distributor-sourced) and listings. Query: `team_id?` (adds inventory fields). |
| PATCH | `/parts/{partId}` | `{description?, category?, category_id?, package?, datasheet_url?, lifecycle_status?, preferred_parameter_source_id?}`. Team-scoped parts require team membership. |
| GET | `/parts/{partId}/parameters` | User + distributor-sourced parameters |
| POST | `/parts/{partId}/parameters` | `{name, value, unit?}` — create/upsert user parameter |
| DELETE | `/parts/{partId}/parameters/{parameterId}` | Delete user parameter |
| GET | `/parts/{partId}/listings` | Distributor listings for part |

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
| POST | `/members/{userId}/deactivate` | admin | Globally deactivate user; clears all sessions/tokens |
| GET | `/distributor-configs` | admin | List configs (secret credential values omitted; includes `rate_limit` if available) |
| PUT | `/distributor-configs/{distributorId}` | admin | `{enabled, preferred, cache_duration_seconds, api_credentials?}`. Setting `preferred` clears other distributors' preferred flag and deletes their OAuth tokens. Credentials are merged (blank secret fields preserve existing values). |
| DELETE | `/distributor-configs/{distributorId}` | admin | Delete config + OAuth tokens; 204 |

### Inventory

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/inventory` | viewer | List entries; query: `q?`, `storage_location_id?`, `below_min_stock=true?`, `limit`, `offset` |
| GET | `/inventory/replenish-preview` | viewer | Preview cart items needed to meet reorder levels |
| GET | `/inventory/{entryId}` | viewer | Entry details |
| GET | `/inventory/{entryId}/adjustments` | viewer | Stock adjustment ledger; query: `limit`, `offset`. Each entry includes `source_type` (`order_line_item`, `build`, `general_checkout`, or null for manual) and `source_id`. |
| POST | `/inventory` | member | `{part_id, preferred_listing_id?, storage_location_id?, min_stock_qty?, reorder_qty?}` → 201. Returns 409 if entry already exists. |
| PATCH | `/inventory/{entryId}` | member | `{preferred_listing_id?, storage_location_id?, min_stock_qty?, reorder_qty?}`. Three-state: absent=keep, null=clear, value=set. |
| POST | `/inventory/{entryId}/adjustments` | member | `{delta, reason?}` → 201 `{id, inventory_entry_id, delta, adjusted_at, reason, created_by, quantity_on_hand}`. Delta must be non-zero. Returns 422 `INSUFFICIENT_STOCK` if would go negative. Positive adjustments trigger build readiness re-evaluation. |

### Storage Locations

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/storage-locations` | viewer | List locations |
| POST | `/storage-locations` | member | `{label, description?}` → 201. Returns 409 if label exists. |
| PATCH | `/storage-locations/{locationId}` | member | `{label?, description?}` |
| DELETE | `/storage-locations/{locationId}` | member | 204. Returns 422 `LOCATION_IN_USE` if referenced by inventory entries. |

### Projects & BOMs

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/projects` | viewer | List; query: `status?`, `limit`, `offset` |
| GET | `/projects/{projectId}` | viewer | Project + active revision summary (`item_count`, `resolved_count`, `unresolved_count`, `ambiguous_count`) |
| GET | `/projects/{projectId}/revisions` | viewer | All BOM revisions (no pagination) |
| GET | `/projects/{projectId}/revisions/{revId}` | viewer | Revision detail with line items and substitutes |
| GET | `/projects/{projectId}/revisions/{revId}/substitutes` | viewer | All substitutes for revision (flat list) |
| GET | `/projects/{projectId}/revisions/{revId}/stock-check` | viewer | Stock check; query: `build_quantity` (required, positive int). Returns `{revision_id, build_quantity, fully_buildable, items}` where each item includes: `quantity_per_board`, `required_quantity`, `quantity_on_hand`, `quantity_reserved`, `quantity_free`, `quantity_on_order`, `shortfall`, `stock_status` (`in_stock`/`partial`/`out_of_stock`), `min_stock_qty`, `below_restock`, `selected_substitute_id/mpn`, `substitute_qty_on_hand`. |
| GET | `/parts/categories` | viewer | Categories with part counts for team |
| POST | `/projects` | member | `{name, description?, preferred_distributor_id?, source_url?, project_manager_id?}` → 201 |
| PATCH | `/projects/{projectId}` | member | `{name?, description?, status?, active_bom_revision_id?, bom_import_config?, preferred_distributor_id?, source_url?, project_manager_id?}`. `bom_import_config`: absent=keep, null=clear, object=set. |
| POST | `/projects/{projectId}/revisions` | member | Import BOM → 202 `{job_id, bom_revision_id}`. Body: `{label, notes?, set_active, bom_import_config?, items: [{reference_designators, quantity, raw_description?, manufacturer?, mpn?, name?, sku?, distributor_name?, resolved_distributor_id?}]}`. Enqueues `JobBOMImport`. |
| PATCH | `/projects/{projectId}/revisions/{revId}` | member | `{label?, notes?}` |
| PATCH | `.../revisions/{revId}/items/{itemId}` | member | `{resolved_part_id?, ignored?}`. Setting `resolved_part_id` marks resolution as `manual`. |
| POST | `.../items/{itemId}/enrich` | member | `{sku, distributor_id}` → `{part_id, part_mpn, part_manufacturer}`. Looks up locally first, falls back to distributor API. Creates inventory entry and resolves the BOM line item. Returns 422 if distributor not configured; 502 on API error. |

### Substitutes

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `.../items/{itemId}/substitutes` | viewer | List substitutes for a line item with inventory data (`{items: [{part_id, part_mpn, priority, manufacturer, description, lifecycle_status, quantity_on_hand}]}`) |
| GET | `.../items/{itemId}/substitutes/previous-revisions` | viewer | Revisions in the same project that have substitutes for this line item's part |
| POST | `.../items/{itemId}/substitutes` | member | `{part_id}` → 201. Returns 409 if already exists. |
| DELETE | `.../items/{itemId}/substitutes/{partId}` | member | 204 (idempotent) |
| PUT | `.../items/{itemId}/substitutes/order` | member | `{part_ids: [...]}` — reorder priorities (index 0 = highest); 204 |
| POST | `.../items/{itemId}/substitutes/import` | member | `{source_revision_id}` — copy substitutes from another revision; 204 |

### Parts (team operations)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/parts/{partId}/allocations` | viewer | Build allocations for part across pending/ordered builds |
| POST | `/parts/import` | member | `{sku, distributor_id}` → `{part_id}` — atomic import: part + listings + inventory |
| POST | `/parts/enrich-all` | member | Enqueue enrichment for all parts with listings → 202 `{job_count}` |
| POST | `/parts/{partId}/enrich` | member | `{sku, distributor_id}` → 202 `{job_id}` |
| POST | `/parts/{partId}/listings` | member | `{sku, distributor_id}` → 202 `{job_id}` |
| DELETE | `/parts/{partId}/listings/{listingId}` | member | Remove listing + distributor-sourced parameters |
| POST | `/parts/{partId}/listings/refresh` | member | Enqueue refresh for all listings → 202 `{job_count}` |
| POST | `/parts/{partId}/listings/{listingId}/refresh` | member | → 202 `{job_id}` |

### Distributor Search

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/distributor-configs/{distributorId}/search` | viewer | Query: `keywords` (required), `limit` (max 50), `offset` → `{items: [{mpn, manufacturer, description, category, primary_sku, package, unit_price, stock_available}], total, exact_match_indices}`. Returns 422 if no API integration. |

### Builds

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/builds` | viewer | List; query: `project_id?`, `status?` (repeatable), `created_by?`, `sort_by?` (`project_name`/`bom_revision_label`/`status`/`quantity`/`created_at`), `sort_dir?` (`asc`/`desc`), `limit`, `offset` |
| GET | `/builds/pipeline` | viewer | Builds grouped by status: `{draft: [], pending: [], ordered: [], ready: []}` |
| GET | `/builds/{buildId}` | viewer | Build details with `allocations`, `order_ids`, `substitute_selections`, `selections_stale`, and (if assembled) `items` showing per-part deductions |
| POST | `/builds/preview` | member | `{bom_revision_id, quantity, substitute_selections?}` → preview deductions without persisting |
| POST | `/builds` | member | `{project_id, bom_revision_id, quantity, notes?}` → 201 draft build; enqueues substitute selection computation |
| POST | `/builds/{buildId}/submit` | member | draft→pending; recomputes cart shortfalls. Returns 409 if substitute selections still being computed. |
| POST | `/builds/{buildId}/checkout` | member | pending/ready→assembled; `{assembled_quantity?, substitute_selections?, allow_stock_reconciliation?, allow_orphaned_selections?}`. Returns 409 with `insufficient_items` if stock short and reconciliation not allowed. Response: `{id, status, assembled_quantity, remaining_quantity?, assembled_at, deduction_count}`. |
| PATCH | `/builds/{buildId}` | member | `{quantity?, notes?}` (draft/pending only); recomputes shortfalls if pending and quantity changed |
| DELETE | `/builds/{buildId}` | member | Cancel any non-terminal build; releases allocations; 204 |
| PATCH | `/builds/{buildId}/cart-inclusion` | purchaser | `{excluded: bool}` — toggle whether pending build contributes to cart demand; 204 |
| PATCH | `/builds/{buildId}/substitute-selections` | member | `{selections: [{bom_line_item_id, part_id}]}` → `{selections, warnings}` (draft only; warns on insufficient stock) |
| POST | `/builds/{buildId}/substitute-selections/refresh` | member | Re-enqueue substitute selection computation (draft only) → 202 `{status: "refreshing"}` |

### Cart

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/cart/count` | purchaser | `{count}` |
| GET | `/cart/part-quantity` | purchaser | Quantity for specific part; query: `part_id` (required), `listing_id?` |
| GET | `/cart` | purchaser | Full cart with items, listings, and build demands per item |
| DELETE | `/cart` | purchaser | Clear all items; 204 |
| POST | `/cart/items` | purchaser | `{part_id, quantity, listing_id?, source?}` → 201 |
| PATCH | `/cart/items/{itemId}` | purchaser | `{quantity?, listing_id?}` → 204. Demand floors enforced for `bom_shortfall`/`inventory_maintenance` sources. |
| DELETE | `/cart/items/{itemId}` | purchaser | Remove item; 204 |
| POST | `/cart/add-low-stock` | purchaser | Add items to reach reorder levels → `{added_count}` |
| POST | `/cart/refresh-listings` | purchaser | Enqueue refresh for all cart listings → 202 `{job_id}` (or 204 if nothing to refresh) |
| POST | `/cart/convert` | purchaser | Convert to orders grouped by distributor; links pending builds, creates allocations, transitions pending→ordered/ready. Returns 422 if cart empty or items missing listings. → `{order_ids}` |

### Orders

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/orders` | viewer | List; query: `status?`, `distributor_id?`, `limit`, `offset` |
| GET | `/orders/{orderId}` | viewer | Order + line items + integrations metadata |
| GET | `/orders/{orderId}/linked-builds` | viewer | Builds linked to this order with fulfillment progress |
| GET | `/orders/{orderId}/distributor-status` | viewer | Live order status from distributor API (persisted to `integrations` JSONB). Returns 422 if no `external_reference` or no ordering support. |
| POST | `/orders/import` | purchaser | `{sales_order_id, existing_order_id?, force?}` → `{job_id}`. With `existing_order_id`: links to existing partrack order. Without: creates new order from distributor data. Returns 409 if already imported. |
| PATCH | `/orders/{orderId}` | purchaser | `{notes?, purchase_order_number?, line_items?: [{id, quantity}]}` → 204 (draft only) |
| DELETE | `/orders/{orderId}` | purchaser | Cancel order; releases build allocations; 204 (any status except received) |
| GET | `/orders/{orderId}/export` | purchaser | Download CSV; transitions to `exported`. Content-Disposition: `order-{distributor}-{date}.csv` |
| POST | `/orders/{orderId}/submit` | purchaser | Submit order to distributor (not yet implemented; returns 501) |
| POST | `/orders/{orderId}/reference` | purchaser | `{external_reference?}` → `{id, status, external_reference, external_ref_linked_at}`. Links tracking number; transitions to `placed`. |
| POST | `/orders/{orderId}/items/{itemId}/receive` | purchaser | `{quantity_received}` → `{line_item_id, quantity_ordered, quantity_received, order_status, new_quantity_on_hand, allocation_breakdown}`. Allocates to linked builds first, remainder to general inventory. Auto-transitions order status. Returns 409 if order not in receivable status. |
| POST | `/orders/{orderId}/push-to-distributor` | purchaser | Push order as parts list on distributor platform → `{list_id, list_url}`. Transitions to `exported_via_api`. Returns 401 if OAuth required; 422 if no ordering support. |
| POST | `/orders/{orderId}/request-quote` | purchaser | Request confirmed pricing quote from distributor → `{quote_id, quote_name, items}`. Returns 422 if no ordering support. |

### OAuth (distributor)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/oauth/{distributorApiType}/initiate` | purchaser | `{auth_url}` — start OAuth flow; query: `return_to?` (default `/`) |
| GET | `/oauth/{distributorApiType}/status` | purchaser | `{connected, account_id, updated_at}` |
| DELETE | `/oauth/{distributorApiType}/token` | purchaser | Disconnect OAuth; 204 |

### Checkouts

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/checkouts` | viewer | List; query: `limit`, `offset` |
| GET | `/checkouts/{checkoutId}` | viewer | Checkout + items (part details + stock adjustment IDs) |
| POST | `/checkouts` | member | `{reason?, notes?, items: [{inventory_entry_id or part_id, quantity, reconcile_to?}]}` → 201 `{id, performed_at, reason, items, reservation_warnings?}`. Returns 409 with `insufficient_items` if stock insufficient. |

### Search & Events

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/search?q=` | viewer | Full-text search across parts, projects, orders, builds, storage (min 2 chars) |
| GET | `/events` | viewer | SSE stream. Events: `part_changed`, `inventory_changed`, `cart_changed`, `order_changed`, `storage_changed`, `project_changed`, `revision_changed`, `build_changed`. Events from the acting user's own actions are skipped. |

---

## Standard Responses

**List:** `{total, limit, offset, items: [...]}`

**Error:** `{error: "message", code: "ERROR_CODE"}`

**Async (202):** `{job_id: "uuid"}` — progress via SSE

---

## Build Lifecycle

`draft` → `pending` → `ordered` → `ready` → `assembled`

- `POST /builds` creates draft (no stock deduction, no allocations); enqueues substitute selection computation
- `submit` → pending; recomputes cart shortfalls for the team
- Cart `convert` → ordered (or ready if already fully in stock); creates `build_part_allocations`
- Order `receive` → allocates received stock to linked builds; transitions ordered→ready when all allocations met
- `checkout` → assembled; applies stock deductions

Builds can be cancelled from any non-terminal status via `DELETE /builds/{buildId}`.

---

## Order Lifecycle

`draft` → `exported`/`exported_via_api`/`submitted` → `placed` → `partially_received` → `received`

- Cart `convert` creates draft orders grouped by distributor
- `export` → `exported` (CSV download)
- `push-to-distributor` → `exported_via_api` (creates parts list on distributor platform)
- `reference` → `placed` (links external order number)
- `receive` on line items → `partially_received` → `received` (auto-transitions when all items fully received)
- `DELETE` → `cancelled` (any status except `received`)
