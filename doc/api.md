# Partrack REST API

All endpoints are prefixed with `/api/v1`. Auth: session cookie or `Authorization: Bearer <token>`.

## Roles (team-scoped, ordered)

`viewer` < `member` < `purchaser` < `admin`

Higher roles inherit all permissions of lower roles.

---

## Public (no auth)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Liveness check — returns `{"status":"ok"}` with 200 |
| GET | `/categories` | All categories (flat list; build tree from `parent_id`) |
| GET | `/parameter-definitions` | All canonical parameter definitions |

---

## Auth

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/auth/login` | — | `{email, password}` → session cookie + `{user}`. The identifier is matched against local credentials first; if no local credential exists and an LDAP directory is configured, it is treated as a directory username and verified by a search+bind. Directory logins auto-link to an existing user with the same email, else auto-provision (when enabled) and sync group→team-role mappings |
| GET | `/auth/providers` | — | List SSO providers and directory state: `{providers: [{name, display_name}], ldap_enabled}`. `ldap_enabled` tells the login page to accept a non-email username |
| GET | `/auth/oidc/{provider}` | — | Redirect (302) to OIDC provider; sets state/nonce/PKCE cookie. 503 if the provider is configured but unreachable |
| GET | `/auth/oidc/{provider}/callback` | — | OIDC code exchange (PKCE + nonce verified) → session cookie + redirect to `/`. Failures redirect to `/login?error=<CODE>` (`INVALID_STATE`, `OIDC_EXCHANGE_FAILED`, `OIDC_VERIFY_FAILED`, `OIDC_NO_EMAIL`, `OIDC_EMAIL_UNVERIFIED`, `PROVISIONING_DISABLED`, `ACCOUNT_INACTIVE`, …). Unknown identities auto-link to an existing user with the same **verified** email, else auto-provision a new user if enabled for the provider |
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

### LDAP Diagnostics

| Method | Path | Description |
|--------|------|-------------|
| GET | `/admin/ldap/providers` | List configured directories: `{providers: [{name, display_name, url}]}` |
| POST | `/admin/ldap/test-connection` | `{provider}` → dial + TLS + service bind (no user credential), checks configured DNs exist, echoes redacted config: `{connected, error?, steps[], dn_checks[], config}`. **Read-only**. |
| POST | `/admin/ldap/diagnose` | `{provider, username, password}` → runs the real search-then-bind + group evaluation and returns a report `{authenticated, error?, steps[], identity?, authorization?, resolution?, warnings[]}`. **Read-only** — never creates a user, identity, membership, or session. See [auth-ldap.md](auth-ldap.md). |

### Parameter Registry

| Method | Path | Description |
|--------|------|-------------|
| GET | `/admin/parameter-definitions` | List parameter definitions |
| POST | `/admin/parameter-definitions` | `{slug, display_name, unit?, unit_symbol?, value_type}`. `value_type` ∈ `numeric`/`range`/`text`/`enum`/`enum_set` (`enum_set` = list value, e.g. `CANbus, Ethernet`). |
| PATCH | `/admin/parameter-definitions/{id}` | Update definition fields. Changing `value_type` re-parses every part parameter mapped to this definition. Response includes `reparsed_count`. |
| DELETE | `/admin/parameter-definitions/{id}` | Delete definition; linked part parameters and distributor mappings have their `parameter_id` set to null |
| GET | `/admin/distributor-parameter-mappings` | Query: `distributor_id?`, `unmapped=true?` |
| GET | `/admin/distributor-parameter-mappings/{id}/parse-preview` | Query: `value_type?` (default `numeric`), `limit?` (default 12, max 100). Parses the mapping's distinct real sample values under `value_type`; returns `{value_type, results:[{value, parsed, numeric_value, numeric_value_max}]}` for an admin to preview parsing before linking. |
| PATCH | `/admin/distributor-parameter-mappings/{id}` | `{parameter_id?, ignored?}`. Linking back-fills matching distributor-sourced part parameters with the canonical `parameter_id` and re-parses their numeric values (no re-enrichment needed). Response `{reparsed_count}`. |

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
| GET | `/blobs/{sha256}` | Serve blob bytes by content address. **Capability-by-hash**: any authenticated user (no team check, no DB lookup) — the 256-bit hash is the bearer token. Immutable: `Cache-Control: public, max-age=31536000, immutable`, strong `ETag`, honours `If-None-Match` (304). `HEAD` supported. |

### Parts Catalog (global)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/parts` | List parts; query: `team_id?`, `q?`, `category_id?`, `package?`, `mpn?`, `lifecycle_status?`, `scope?` (`global`/`team`), `stock_status?`, `param_{id}_min/max`, `param_{id}_values` (for `enum`/`text`/`enum_set` params; matches contains-any), `limit`, `offset`. When `category_id` set, includes parameters in response. When `team_id` set, includes inventory fields. |
| POST | `/parts` | Create part. Body: `{scope, team_id?, mpn?, manufacturer?, description?, category?, category_id?, package?, datasheet_url?, lifecycle_status?}`. `scope` required (`global` requires admin; `team` requires team membership + `team_id`). Returns 409 `PART_EXISTS` with existing part if MPN+manufacturer match found. 201. |
| GET | `/parts/{partId}` | Part details with parameters (user + distributor-sourced) and listings. Query: `team_id?` (adds inventory fields). |
| PATCH | `/parts/{partId}` | `{description?, category?, category_id?, package?, datasheet_url?, lifecycle_status?, preferred_parameter_source_id?}`. Team-scoped parts require team membership. |
| DELETE | `/parts/{partId}` | **Global admin only.** Permanently deletes the part and all associated data (inventory entries, listings, parameters, BOM references, stock history, order history). Irreversible. 204. |
| GET | `/parts/{partId}/parameters` | User + distributor-sourced parameters |
| POST | `/parts/{partId}/parameters` | `{name, value, unit?}` — create/upsert user parameter. The value is resolved to a canonical parameter (inheriting a same-named distributor parameter's mapping, else matching the registry by slug/display name) and parsed to `numeric_value`/`numeric_value_max`, so user values appear in parameter columns and value search. Response echoes `parameter_id`, `numeric_value`, `numeric_value_max` (null when unresolved). |
| DELETE | `/parts/{partId}/parameters/{parameterId}` | Delete user parameter |
| GET | `/parts/{partId}/listings` | Distributor listings for part |

---

## Team-scoped (`/teams/{teamId}/...`)

### Attachments

| Method | Path | Role | Description |
|--------|------|------|-------------|
| POST | `/attachments` | member | Upload an image. Body is the **raw image bytes** (not multipart/JSON); optional `?filename=` records the original name. Content type is sniffed from the bytes (the declared type is ignored) and validated against the allowlist (`image/png,jpeg,webp,gif`; SVG rejected). Deduplicated by content hash. Returns 201 `{id, url, content_type, byte_size, width?, height?, filename?}` where `url` is `/api/v1/blobs/{sha256}` to embed in note markdown. 413 over the size cap (default 5 MiB); 415 for unsupported type. |

### Team (admin)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/` | viewer | Team details |
| PATCH | `/` | admin | `{name?, description?}` |
| GET | `/dashboard` | viewer | Stats, recent activity, alerts, outstanding orders, build pipeline |
| GET | `/members` | admin | List members; each item includes `directory_managed` (true when maintained by LDAP group sync) |
| POST | `/members` | admin | `{user_id, role}` (manual membership) |
| PATCH | `/members/{userId}` | admin | `{role}`; a manual change pins the membership (`directory_managed=false`), preserving it against LDAP sync |
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
| GET | `/projects/{projectId}/revisions/{revId}` | viewer | Revision detail with line items and substitutes. Each line item includes `resolution_state` (`auto_sku`/`auto_mpn`/`auto_prior_revision`/`ambiguous`/`manual`/`ignored`, plus new-part-pipeline states `importing`/`auto_import`/`import_ambiguous`/`import_no_result`) and, when a new part is being imported via the resolve pipeline, `resolve_item: {id, status, task_type, enrichment_data, error_message}`. |
| GET | `/projects/{projectId}/revisions/{revId}/substitutes` | viewer | All substitutes for revision (flat list) |
| GET | `/projects/{projectId}/revisions/{revId}/stock-check` | viewer | Stock check; query: `build_quantity` (required, positive int). Returns `{revision_id, build_quantity, fully_buildable, items}` where each item includes: `quantity_per_board`, `required_quantity`, `quantity_on_hand`, `quantity_reserved`, `quantity_free`, `quantity_on_order`, `shortfall`, `stock_status` (`in_stock`/`partial`/`out_of_stock`), `min_stock_qty`, `below_restock`, `selected_substitute_id/mpn`, `substitute_qty_on_hand`. |
| GET | `/parts/categories` | viewer | Categories with direct part counts for team → `{categories: [{id, count}]}`. Used to build the category filter; counts are direct (not recursive); callers aggregate up the tree client-side. |
| POST | `/projects` | member | `{name, description?, preferred_distributor_id?, source_url?, project_manager_id?}` → 201 |
| PATCH | `/projects/{projectId}` | member | `{name?, description?, status?, active_bom_revision_id?, bom_import_config?, preferred_distributor_id?, source_url?, project_manager_id?}`. `bom_import_config`: absent=keep, null=clear, object=set. |
| POST | `/projects/{projectId}/revisions` | member | Import BOM → 202 `{job_id, bom_revision_id}`. Body: `{label, notes?, set_active, bom_import_config?, items: [{reference_designators, quantity, raw_description?, manufacturer?, mpn?, name?, sku?, distributor_name?, resolved_distributor_id?}]}`. `bom_import_config` may include `single_distributor_id` (applied to all SKU rows when the BOM has no distributor column). Enqueues `JobBOMImport`, which resolves known parts inline (SKU/MPN/prior-revision) and hands **new** parts to the resolve pipeline (`part_resolve_items`); unambiguous new parts auto-commit, ambiguous/failed ones surface for review. |
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
| POST | `/parts/resolve` | member | `{sku?, distributor_id?, mpn?, manufacturer?}` → `202 {resolve_item_id, job_id}` — create a resolution job; worker fans out to all enabled distributors |
| GET | `/parts/resolve/{resolveItemId}` | member | Poll resolution status → `{id, status, task_type, resolved_part_id, enrichment_data, error_message, enriched_at, committed_at}` |
| POST | `/parts/resolve/{resolveItemId}/commit` | member | `{primary_distributor_id, primary_sku, additional_listings?:[{distributor_id,sku}], initial_qty?, initial_qty_reason?, storage_location_id?}` → `{part_id}` — write part/listings/inventory from cached enrichment; no API re-call. `primary_distributor_id`+`primary_sku` identify the preferred listing (used for enrichment/metadata authority); only the selected SKUs from that distributor are persisted. `additional_listings` selects individual listings from any distributor at SKU granularity (can include extra SKUs from the primary distributor). `initial_qty` creates a stock adjustment; `storage_location_id` sets the inventory entry location. |
| POST | `/parts/resolve/{resolveItemId}/retry-distributors` | member | `{distributor_ids:[]}` → `202 {job_id}` — re-query only failed distributors; merges into existing results |
| POST | `/parts/import` | member | `{sku, distributor_id}` → `{part_id}` — atomic synchronous import: part + listings + inventory (single distributor) |
| POST | `/parts/refresh-parameters` | member | Enqueue a re-enrichment (one live distributor API call per part) for every part matching the given ListParts filters (same query params as `/parts/refresh-listings`) that has an API-integrated listing. Scopes the operation to the current search. → 202 `{job_id, part_count}`, 204 if nothing enrichable, or 400 `TOO_MANY_PARTS` if the match set exceeds 1000. Progress streams over SSE: per-part `part_enrich_done` events then a terminal `part_enrich_complete` `{total, done, not_found, failed}` (all carry `job_id`). |
| POST | `/parts/{partId}/enrich` | member | `{sku, distributor_id}` → 202 `{job_id}` |
| POST | `/parts/{partId}/listings` | member | `{sku, distributor_id}` → 202 `{job_id}` |
| DELETE | `/parts/{partId}/listings/{listingId}` | member | Remove listing + distributor-sourced parameters |
| POST | `/parts/refresh-listings` | member | Enqueue a stock/price refresh for every refreshable listing whose part matches the given ListParts filters (same query params: `q`, `mpn`, `category_id`, `package`, `lifecycle_status`, `scope`, `stock_status`, `storage_location_id`, `storage_cells`, `param_*`). Scopes the refresh to the current search. → 202 `{job_id, listing_count}`, 204 if nothing refreshable, or 400 `TOO_MANY_LISTINGS` if the match set exceeds 1000. Progress streams over SSE: per-listing `listing_refresh_done` events then a terminal `listing_refresh_complete` `{total, done, unavailable, failed}` (all carry `job_id`). |
| POST | `/parts/{partId}/listings/refresh` | member | Enqueue refresh for all of a part's listings → 202 `{job_id, listing_count}` |
| POST | `/parts/{partId}/listings/{listingId}/refresh` | member | → 202 `{job_id}` |

### Part Notes

The team's freeform markdown note for a part (one per team per part — the knowledge profile, parallel to the inventory entry). The body is markdown that may embed images as `attachment:<uuid>` (uploaded via `POST /attachments`) and **object links** as `:<kind>[label]{#<uuid>}` remark directives, where `kind` ∈ `part | project | revision | order | build | storage`. Both endpoints return a **manifest** that resolves those references server-side (batched), so the renderer needs no per-reference round-trips.

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/parts/{partId}/note` | viewer | The team's note for the part. Returns `{body, manifest, flag, updated_at?, updated_by?}`; `body` is `""` and `flag` is `"none"` when no note exists yet (200, not 404). |
| PUT | `/parts/{partId}/note` | member | `{body}` — upsert the note (created lazily on first save; leaves `flag` untouched). Part must be visible to the team (global or team-owned), else 404. Body capped at 256 KiB. Returns the saved `{body, manifest, flag, updated_at, updated_by}`. |
| PUT | `/parts/{partId}/flag` | member | `{flag}` — set the team's opinion flag, one of `none\|preferred\|approved\|banned` (independent of the note body). 400 on an unknown value. Returns `{flag}`. `banned` surfaces as `resolved_part_flag` on BOM revision line items (a design-in warning). |
| GET | `/projects/{projectId}/note` | viewer | The team's note for the project (stored on the project row — projects are single-team). Returns `{body, manifest, updated_at?, updated_by?}`. 404 if the project isn't the team's. |
| PUT | `/projects/{projectId}/note` | member | `{body}` — update the project note. 404 if not the team's. Body capped at 256 KiB. |

**Manifest shape** (`manifest`):
```json
{
  "attachments": { "<attachment-uuid>": { "url": "/api/v1/blobs/<sha256>", "content_type": "image/png", "width": 800, "height": 600 } },
  "objects": { "<object-uuid>": { "kind": "project", "label": "Power Supply v2", "href": "/teams/<tid>/projects/<id>", "status": "active" } }
}
```
Each `objects` entry is a normalized, render-ready link: the server computes `label`, the route `href` (per-kind — e.g. revisions nest under their project), and an optional `status` (lifecycle/order/build/project status, used for styling). Only resolvable references appear: attachments and objects are team-scoped (parts also include global). Dangling references are omitted (the renderer falls back to the stored label, struck-through).

### Documents

File attachments (datasheets, images, office files, Touchstone S-parameters, …) pinned to a **part** or **project** and listed **separately from note markdown**. They reuse the blob/attachment layer: ownership is recorded on the `attachments` row (`owner_kind`/`owner_id`); a note-embedded image leaves the owner NULL and is not a document. Classification is by **file extension** (office/Touchstone files have no distinctive magic bytes); PDFs and raster images are **served inline** (and verified to actually sniff as an inline-safe type), every other type is **forced to download** so no uploaded bytes can execute in-origin.

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/parts/{partId}/documents` | viewer | List the team's documents on the part (team-scoped; global part visible). Returns `{documents: [{id, filename, content_type, byte_size, viewable, uploaded_by?, uploaded_at?}]}`, newest first. `viewable` = PDF or raster image. 404 if the part isn't visible to the team. |
| POST | `/parts/{partId}/documents` | member | Upload a document. Body is the **raw file bytes**; `?filename=` is **required** (drives extension classification + the download name). 201 with the document row. 415 unsupported extension or content not matching an inline type; 413 over the cap (default 25 MiB). |
| GET | `/projects/{projectId}/documents` | viewer | List the project's documents. Same shape; 404 if not the team's. |
| POST | `/projects/{projectId}/documents` | member | Upload a document to the project. 404 if not the team's. |
| GET | `/documents/{docId}/content` | viewer | Serve the bytes — **access-controlled** (resolved team-scoped) and filename-aware, unlike `/blobs/{sha}`. `Content-Disposition: inline` for PDF/image, else `attachment`; pass `?download=1` to force download with the original filename. Strong `ETag` (blob hash), `If-None-Match`→304, `nosniff`, `Cache-Control: private, immutable`. `HEAD` supported. 404 cross-team. |
| DELETE | `/documents/{docId}` | member | Remove the document attachment (the shared blob is retained — no-GC). 204; 404 if not the team's. |

Upload/delete publish the owner's existing entity-change event (`part_changed` / `project_changed`), so other clients refetch the document list.

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
| POST | `/builds/{buildId}/submit` | member | draft→pending (or →ready when stock fully covers); recomputes cart shortfalls. Returns 409 `SUBSTITUTE_SELECTIONS_PENDING` if selections are still computing, or 409 `SUBSTITUTE_SELECTIONS_STALE` if inventory changed since they were computed (refresh first). |
| POST | `/builds/{buildId}/checkout` | member | draft/pending/ready→assembled; `{assembled_quantity?}`. Excluded designators and per-item overrides (`skip`/`use_available`) are applied at checkout. Returns 409 `INSUFFICIENT_STOCK` if any unacknowledged item has insufficient stock. Response: `{id, status, deduction_count}`. |
| PATCH | `/builds/{buildId}` | member | `{quantity?, notes?}` (draft/pending only); recomputes shortfalls if pending and quantity changed |
| DELETE | `/builds/{buildId}` | member | Cancel any non-terminal build; releases allocations; 204 |
| PATCH | `/builds/{buildId}/cart-inclusion` | purchaser | `{excluded: bool}` — toggle whether pending build contributes to cart demand; 204 |
| PATCH | `/builds/{buildId}/substitute-selections` | member | `{selections: [{bom_line_item_id, part_id}]}` → `{selections, warnings}` (draft only; warns on insufficient stock). A null `part_id` (or the primary part itself) reverts the line item to its primary; any other `part_id` must be an approved substitute, else 400 `INVALID_SELECTION`. |
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
| POST | `/cart/refresh-listings` | purchaser | Enqueue refresh for all cart listings → 202 `{job_id, listing_count}` (or 204 if nothing to refresh). Progress streams over SSE: per-listing `listing_refresh_done` then a terminal `listing_refresh_complete` `{total, done, unavailable, failed}`. |
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
| POST | `/oauth/{distributorApiType}/initiate` | purchaser | `{auth_url}` — start OAuth flow; query: `return_to?` (default `/`). POST (not GET) to avoid a cross-site GET side effect. |
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
| GET | `/search?q=` | viewer | Full-text search across parts, projects, BOM revisions, orders, builds, storage (min 2 chars). Returns `{parts, projects, revisions, orders, builds, storage_locations}` |
| GET | `/events` | viewer | SSE stream. Events: `part_changed`, `inventory_changed`, `cart_changed`, `order_changed`, `storage_changed`, `project_changed`, `revision_changed`, `build_changed`. Events from the acting user's own actions are skipped. |

---

## Mobile Scanner

### Desktop-facing (session cookie / API token auth)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| POST | `/teams/{teamId}/scan-sessions` | member+ | Create scan session. Body: `{order_id?: string}`. Returns `{id, pairing_ticket, pairing_ticket_expires_in, expires_at}`. The durable token is NOT returned here — the phone obtains it by redeeming `pairing_ticket` at `POST /scan/pair`. Durable token TTL 24h. |
| POST | `/teams/{teamId}/scan-sessions/{sessionId}/pairing-ticket` | member+ | Mint a fresh single-use pairing ticket for an existing session (desktop refreshes the QR before the 60s TTL lapses). Returns `{pairing_ticket, pairing_ticket_expires_in}`. |
| GET | `/teams/{teamId}/scan-sessions/active` | member+ | Get current user's active scan session. Returns `{id, phone_connected, context_type, context_order_id, context_order_ref, phone_ip, phone_ua}` or 204 if none. |
| DELETE | `/teams/{teamId}/scan-sessions/{sessionId}` | purchaser+ | Delete scan session. |
| PATCH | `/teams/{teamId}/scan-sessions/{sessionId}/context` | purchaser+ | Push order context to phone. Body: `{order_id}`. Returns `{ok, context_order_ref}`. Sends SSE to connected phone immediately. |
| POST | `/teams/{teamId}/scan-sessions/{sessionId}/kitting` | member+ | Start kitting session for a build. Body: `{build_id}`. Seeds confirmations from build allocations, pushes `scan_context_changed` to phone. Returns `{kitting_session_id}`. |
| GET | `/teams/{teamId}/activity-log` | viewer+ | Paginated unified activity feed. Default scope = the caller's own activity; `?scope=team` (purchaser+) returns the team-wide feed (`feed='team'` rows). Query: `action` (comma-separated), `cursor_time`, `cursor_id`, `limit` (default 50). Each item: `{id, action, feed, source, actor_label, subject:{type,id,label}|null, summary, payload, created_at, part_*}`. Returns `{items:[...], next_cursor_time, next_cursor_id}`. |
| GET | `/teams/{teamId}/kitting/{kittingSessionId}` | viewer+ | Desktop view of kitting session (team auth). Returns same shape as phone `/scan/kitting/{id}`. |
| GET | `/teams/{teamId}/kitting/{kittingSessionId}/items` | viewer+ | Desktop view of kitting items (team auth). Returns same shape as phone `/scan/kitting/{id}/items`. |
| GET | `/teams/{teamId}/bulk-scan-sessions` | member+ | List non-deleted bulk scan sessions for current user with per-status counts. Returns `{sessions:[{id, created_at, closed_at, counts:{pending, reviewed, dismissed, ready}}]}`. `ready` = items with status `pending_review` where enrichment is done or not needed. |
| GET | `/teams/{teamId}/bulk-scan-badge` | member+ | Badge count: `pending_review` items that are ready for user action (enriched or no resolve item). Returns `{count}`. |
| GET | `/teams/{teamId}/bulk-scan-sessions/{sessionId}/items` | member+ | List all items in a session with nested resolve item data. Returns `{items:[{id, session_id, barcode, status, task_type, parsed_fields, initial_qty, part_id, part, resolve_item:{id,status,task_type,enrichment_data,error_message,enriched_at}\|null, created_at}]}`. |
| PATCH | `/teams/{teamId}/bulk-scan-sessions/{sessionId}/items/{itemId}` | member+ | Set item status to `reviewed` or `dismissed`. Body: `{status, part_id?}`. First-write-wins: returns 409 if already terminal. |
| POST | `/teams/{teamId}/bulk-scan-sessions/{sessionId}/items/{itemId}/retry` | member+ | Re-enqueue the linked resolve job for `failed`/`enriched` items. Returns `202 {job_id}`. Returns 422 if item has no resolve item. |
| POST | `/teams/{teamId}/bulk-scan-sessions/{sessionId}/items/{itemId}/retry-distributors` | member+ | Re-query a specific set of distributors on the linked resolve item. Body: `{distributor_ids:[]}`. Returns `202 {job_id}`. |
| DELETE | `/teams/{teamId}/bulk-scan-sessions/{sessionId}` | member+ | Soft delete session (`deleted_at = now()`). Returns 204. |

### Phone-facing (scan token auth — `Authorization: Bearer scan_...`)

Phone endpoints take the scan token as an `Authorization: Bearer scan_...` header. The token never appears in a URL; the SSE stream uses a single-use ticket instead. The phone first obtains the durable token by redeeming the QR's pairing ticket.

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/scan/pair` | none (ticket is credential) | Redeem a single-use pairing ticket (from the QR fragment) for the durable scan token. Body: `{ticket}`. Returns `{token, session_id, team_id, expires_at}`. 401 `PAIRING_TICKET_INVALID` if spent/expired. Rate-limited per IP. |
| POST | `/scan/stream-ticket` | Bearer | Mint a single-use, 30s ticket for opening the SSE stream. Returns `{ticket, expires_in}`. |
| GET | `/scan/stream?ticket=` | stream ticket | SSE stream for phone. Auth via single-use `?ticket=` (consumed on connect). 401 `STREAM_TICKET_INVALID` on a spent/invalid ticket — the client mints a fresh ticket and reconnects. Publishes `scan_context_changed`; marks phone connected/disconnected. |
| GET | `/scan/session` | Fallback poll — returns `{session_id, context_type, context_order_id, context_order_ref, pending_scan, active_kitting_session_id, active_kitting_build_ref}`. Returns 410 when session expired. |
| POST | `/scan/context` | Phone sets own context. Body: `{order_id}`. |
| GET | `/scan/orders` | List receivable orders (status `placed` or `partially_received`). Returns `{orders: [{id, ref, status, distributor_name, item_count}]}`. |
| GET | `/scan/parts?q=` | Search parts catalog (global + team). Returns `{parts: [{id, mpn, description, inventory_entry_id, quantity_on_hand, storage_location}]}`. |
| POST | `/scan/scans` | Submit barcode. Body: `{barcode}`. Matches to order line item. Returns `{status: matched|no_match|parse_error, ...}`. Requires active order context. |
| POST | `/scan/confirm` | Confirm pending scan (apply receive). Body: `{qty?}`. Returns `{ok, new_quantity_received, new_quantity_on_hand, order_status}`. |
| POST | `/scan/skip` | Discard pending scan. |
| POST | `/scan/stock-adjustment` | Apply stock delta. Body: `{inventory_entry_id, delta, reason?}`. Member+ only. Returns `{ok, new_quantity_on_hand}`. |
| POST | `/scan/lookup` | Parse a bag barcode and look up the part. Body: `{barcode}`. Returns discriminated union on `status`: `found` (part in catalog), `not_found_in_db` (parsed but unknown — includes `distributor_configs:[{id,name}]`, `can_auto_import`, `parsed_*` fields, and `audit_log_id` — the id of the logged `part_not_found` row, to be echoed back on import for exact activity-feed resolution), `no_fields`, `parse_error`. |
| GET | `/scan/parts/{partId}` | Full part detail for scanner. Returns part + inventory + listings + parameters. |
| POST | `/scan/parts/import` | Import part from a single distributor SKU. Body: `{distributor_id, sku, audit_log_id?}`. When `audit_log_id` (from a prior `/scan/lookup` `not_found_in_db` response) is supplied, that exact `part_not_found` activity-log row is marked resolved to the imported part. Returns `{part_id}`. Member+ only. |
| POST | `/scan/parts/import-multi` | Import part from multiple distributor listings (≥1). Body: `{listings:[{distributor_id,sku}], audit_log_id?}`. `audit_log_id` resolves the originating `part_not_found` activity-log row (all listings deduplicate to one part). Returns `{part_id, failed:[{distributor_id,sku,reason}]}`. All listings with the same MPN+manufacturer deduplicate to one part. Member+ only. |
| GET | `/scan/distributor-search` | Search a configured distributor. Query: `distributor_id`, `q`, `limit?`. Returns `{items:[{mpn,manufacturer,description,category,primary_sku,package,unit_price,stock_available}], total, exact_match_indices}`. |
| POST | `/scan/focus` | Send-to-desktop navigation. Body: `{entity_type: part|order|storage|build|bulk_scan_session, entity_id}`. Returns 204. Returns 409 if no desktop subscriber is connected. |
| POST | `/scan/bulk/start` | Create bulk scan session. Returns `{session_id, item_count}`. |
| POST | `/scan/bulk/sessions/{sessionId}/items` | Add a scanned barcode to the bulk session. Body: `{barcode}`. Deduped by barcode within active items. Performs synchronous DB lookup; creates a resolve job for unknown parts. Returns `{scan_item_id, found_in_db: bool, task_type: string\|null, encoded_qty: int\|null, resolve_item_id: uuid\|null}`. |
| PATCH | `/scan/bulk/sessions/{sessionId}/items/{itemId}` | Set initial quantity. Body: `{initial_qty: int\|null}`. Returns 204. |
| POST | `/scan/bulk/sessions/{sessionId}/close` | Close session (exit bulk mode). Sets `closed_at`. Returns 204. |
| POST | `/scan/bulk/sessions/{sessionId}/navigate` | Push `scan_focus_requested` to desktop with `entity_type=bulk_scan_session`. Returns 204 or 409 if offline. |
| GET | `/scan/kitting/{kittingSessionId}` | Get kitting session info + progress counts. Returns `{session:{id,build_id,build_ref,status}, progress:{pending,confirmed,shortfall,skipped}}`. |
| GET | `/scan/kitting/{kittingSessionId}/items` | List kitting items with storage location. Returns `{items:[{part_id,part_mpn,part_manufacturer,part_description,quantity_needed,quantity_confirmed,status,storage_location_id,storage_location_path,storage_coordinate}]}`. |
| POST | `/scan/kitting/{kittingSessionId}/confirm` | Mark item confirmed. Body: `{part_id, quantity_confirmed}`. Returns `{item, progress}`. |
| POST | `/scan/kitting/{kittingSessionId}/shortfall` | Mark item shortfall. Body: `{part_id, quantity_available}`. Returns `{item, progress}`. |
| POST | `/scan/kitting/{kittingSessionId}/complete` | Complete kitting session. Returns `{progress}`. |
| POST | `/scan/kitting/{kittingSessionId}/abandon` | Abandon kitting session. Returns 204. |

SSE events from scan session changes are published to the owning user's desktop channel as `scan_session_changed` with `{session_id, phone_connected, context_order_ref?}`.

SSE events published to the desktop user channel:
- `activity_changed` — a new activity-log entry was written; invalidate `activity-log` queries. Personal-feed events target only the actor; team-feed events broadcast; audit-only events emit nothing.
- `scan_focus_requested` — phone triggered navigation; payload `{entity_type, entity_id}` (entity_type includes `bulk_scan_session`)
- `build_kitting_changed` — kitting progress updated; payload `{build_id, kitting_session_id}`
- `bulk_scan_changed` — bulk scan item enriched/errored/retried; payload `{session_id, item_id}`; targeted to session owner only

`GET /scan/session` response now includes `bulk_session: {id, item_count, state: "active"|"timed_out"} | null` for the most recent non-deleted, non-closed session for the authenticated user. State is `timed_out` when `last_item_at > 15 minutes ago`.

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
