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
| PATCH | `/inventory/{entryId}` | member | `{preferred_listing_id?, storage_location_id?, storage_coordinate?, min_stock_qty?, reorder_qty?}`. Three-state: absent=keep, null=clear, value=set. `storage_coordinate` is a flat `number[]` interpreted against the referenced location's `grid_config`. |
| POST | `/inventory/{entryId}/adjustments` | member | `{delta, reason?}` → 201 `{id, inventory_entry_id, delta, adjusted_at, reason, created_by, quantity_on_hand}`. Delta must be non-zero. Returns 422 `INSUFFICIENT_STOCK` if would go negative. Positive adjustments trigger build readiness re-evaluation. |

### Storage Locations

All storage location responses include: `{id, team_id, parent_id: string|null, label, description: string|null, grid_config: GridConfig|null}`.

`GridConfig = { levels: [{dims: number[], name?: string}], display?: "numeric"|"alphanumeric" }`. `dims` is 1 or 2 positive integers (1D or 2D grid). Multiple levels enable drill-down (e.g. wall of boxes → drawers within a box).

`storage_coordinate` on inventory entries is a flat `number[]`. Its structure is derived from the referenced location's `grid_config.levels[*].dims` — a partial prefix covering a whole number of levels is valid (partial addressing).

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/storage-locations` | viewer | List all locations (flat, client assembles tree using `parent_id`). |
| GET | `/storage-locations/{locationId}` | viewer | `{location: StorageLocation, ancestors: StorageLocation[]}`. Ancestors ordered root-first including the location itself. |
| GET | `/storage-locations/{locationId}/inventory` | viewer | Paginated inventory entries at this location or any descendant. Supports `limit`/`offset`. Returns `{items, total}`. |
| POST | `/storage-locations` | member | `{label, description?, parent_id?: string, grid_config?: GridConfig}` → 201. Returns 409 if sibling label conflict. |
| PATCH | `/storage-locations/{locationId}` | member | `{label?, description?, grid_config?}`. Send `grid_config: null` to clear. |
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
| GET | `/builds/{buildId}` | viewer | Build details with `allocations`, `order_ids`, `substitute_selections`, `selections_stale`, `excluded_designators`, `line_item_overrides`, and (if assembled) `items` showing per-part deductions |
| POST | `/builds/preview` | member | `{bom_revision_id, quantity}` → ad-hoc preview without a saved build |
| GET | `/builds/{buildId}/preview` | viewer | Preview for a saved draft/pending build; accounts for `excluded_designators` and `line_item_overrides`. Items include `effective_quantity_per_board`, `effective_total_quantity`, `excluded_designator_count`, `override` (`use_available`/`skip`/`null`), `available_quantity` |
| PUT | `/builds/{buildId}/excluded-designators` | member | `{designators: string[]}` — replace full excluded designator set (draft/pending only); recomputes cart shortfalls for pending builds; 200 |
| PATCH | `/builds/{buildId}/line-item-overrides/{itemId}` | member | `{override_type: "use_available" \| "skip" \| null}` — set or clear stock override for a BOM line item (draft/pending only); null deletes the override; 200 |
| POST | `/builds` | member | `{project_id, bom_revision_id, quantity, notes?}` → 201 draft build; enqueues substitute selection computation |
| POST | `/builds/{buildId}/submit` | member | draft→pending; recomputes cart shortfalls. Returns 409 if substitute selections still being computed. |
| POST | `/builds/{buildId}/checkout` | member | draft/pending/ready→assembled; `{assembled_quantity?}`. Excluded designators and per-item overrides (`skip`/`use_available`) are applied at checkout. Returns 409 `INSUFFICIENT_STOCK` if any unacknowledged item has insufficient stock. Response: `{id, status, deduction_count}`. |
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
| POST | `/orders/{orderId}/retry-unresolved` | purchaser | Re-attempt enrichment for unresolved import items → `{job_id}`. Requires `external_reference` set and order not cancelled. Returns 400 `NOT_LINKED` or 409 `ORDER_CANCELLED`. SSE: `order_import_progress` with `{status, job_id, order_id, total, attempted, resolved, unresolved_skus}`, then `order_changed`. |

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

## Mobile Scanner

### Desktop-facing (session cookie / API token auth)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| POST | `/teams/{teamId}/scan-sessions` | member+ | Create scan session. Body: `{order_id?: string}`. Returns `{id, token, expires_at}`. Token is 30-day TTL. |
| GET | `/teams/{teamId}/scan-sessions/active` | member+ | Get current user's active scan session. Returns `{id, phone_connected, context_type, context_order_id, context_order_ref, phone_ip, phone_ua}` or 204 if none. |
| DELETE | `/teams/{teamId}/scan-sessions/{sessionId}` | purchaser+ | Delete scan session. |
| PATCH | `/teams/{teamId}/scan-sessions/{sessionId}/context` | purchaser+ | Push order context to phone. Body: `{order_id}`. Returns `{ok, context_order_ref}`. Sends SSE to connected phone immediately. |
| POST | `/teams/{teamId}/scan-sessions/{sessionId}/kitting` | member+ | Start kitting session for a build. Body: `{build_id}`. Seeds confirmations from build allocations, pushes `scan_context_changed` to phone. Returns `{kitting_session_id}`. |
| GET | `/teams/{teamId}/scan-audit-log` | viewer+ | Paginated scan activity log for the current user. Query: `event_type` (comma-separated), `cursor_time`, `cursor_id`, `limit` (default 50). Returns `{items:[...], next_cursor_time, next_cursor_id}`. |
| GET | `/teams/{teamId}/kitting/{kittingSessionId}` | viewer+ | Desktop view of kitting session (team auth). Returns same shape as phone `/scan/kitting/{id}`. |
| GET | `/teams/{teamId}/kitting/{kittingSessionId}/items` | viewer+ | Desktop view of kitting items (team auth). Returns same shape as phone `/scan/kitting/{id}/items`. |

### Phone-facing (scan token auth — `Authorization: Bearer scan_...`)

Phone endpoints accept the scan token either as `Authorization: Bearer` header (fetch) or `?token=` query param (EventSource).

| Method | Path | Description |
|--------|------|-------------|
| GET | `/scan/stream` | SSE stream for phone. Token via `?token=`. Server publishes `scan_context_changed` events when desktop pushes context. Marks phone connected on open, disconnected on close. |
| GET | `/scan/session` | Fallback poll — returns `{session_id, context_type, context_order_id, context_order_ref, pending_scan, active_kitting_session_id, active_kitting_build_ref}`. Returns 410 when session expired. |
| POST | `/scan/context` | Phone sets own context. Body: `{order_id}`. |
| GET | `/scan/orders` | List receivable orders (status `placed` or `partially_received`). Returns `{orders: [{id, ref, status, distributor_name, item_count}]}`. |
| GET | `/scan/parts?q=` | Search parts catalog (global + team). Returns `{parts: [{id, mpn, description, inventory_entry_id, quantity_on_hand, storage_location}]}`. |
| POST | `/scan/scans` | Submit barcode. Body: `{barcode}`. Matches to order line item. Returns `{status: matched|no_match|parse_error, ...}`. Requires active order context. |
| POST | `/scan/confirm` | Confirm pending scan (apply receive). Body: `{qty?}`. Returns `{ok, new_quantity_received, new_quantity_on_hand, order_status}`. |
| POST | `/scan/skip` | Discard pending scan. |
| POST | `/scan/stock-adjustment` | Apply stock delta. Body: `{inventory_entry_id, delta, reason?}`. Member+ only. Returns `{ok, new_quantity_on_hand}`. |
| POST | `/scan/lookup` | Parse a bag barcode and look up the part. Body: `{barcode}`. Returns discriminated union on `status`: `found` (part in catalog), `not_found_in_db` (parsed but unknown — includes `distributor_configs:[{id,name}]`, `can_auto_import`, `parsed_*` fields), `no_fields`, `parse_error`. |
| GET | `/scan/parts/{partId}` | Full part detail for scanner. Returns part + inventory + listings + parameters. |
| POST | `/scan/parts/import` | Import part from a single distributor SKU. Body: `{distributor_id, sku}`. Returns `{part_id}`. Member+ only. |
| POST | `/scan/parts/import-multi` | Import part from multiple distributor listings (≥1). Body: `{listings:[{distributor_id,sku}]}`. Returns `{part_id, failed:[{distributor_id,sku,reason}]}`. All listings with the same MPN+manufacturer deduplicate to one part. Member+ only. |
| GET | `/scan/distributor-search` | Search a configured distributor. Query: `distributor_id`, `q`, `limit?`. Returns `{items:[{mpn,manufacturer,description,category,primary_sku,package,unit_price,stock_available}], total, exact_match_indices}`. |
| POST | `/scan/focus` | Send-to-desktop navigation. Body: `{entity_type: part|order|storage|build, entity_id}`. Returns 204. Returns 409 if no desktop subscriber is connected. |
| GET | `/scan/kitting/{kittingSessionId}` | Get kitting session info + progress counts. Returns `{session:{id,build_id,build_ref,status}, progress:{pending,confirmed,shortfall,skipped}}`. |
| GET | `/scan/kitting/{kittingSessionId}/items` | List kitting items with storage location. Returns `{items:[{part_id,part_mpn,part_manufacturer,part_description,quantity_needed,quantity_confirmed,status,storage_location_id,storage_location_path,storage_coordinate}]}`. |
| POST | `/scan/kitting/{kittingSessionId}/confirm` | Mark item confirmed. Body: `{part_id, quantity_confirmed}`. Returns `{item, progress}`. |
| POST | `/scan/kitting/{kittingSessionId}/shortfall` | Mark item shortfall. Body: `{part_id, quantity_available}`. Returns `{item, progress}`. |
| POST | `/scan/kitting/{kittingSessionId}/complete` | Complete kitting session. Returns `{progress}`. |
| POST | `/scan/kitting/{kittingSessionId}/abandon` | Abandon kitting session. Returns 204. |

SSE events from scan session changes are published to the owning user's desktop channel as `scan_session_changed` with `{session_id, phone_connected, context_order_ref?}`.

SSE events published to the desktop user channel:
- `scan_audit_changed` — a new audit log entry was written; invalidate `scan-audit-log` queries
- `scan_focus_requested` — phone triggered navigation; payload `{entity_type, entity_id}`
- `build_kitting_changed` — kitting progress updated; payload `{build_id, kitting_session_id}`

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
