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
| GET | `/version` | Running build metadata — `{"version","commit","date"}` (CalVer; `version:"dev"` for un-stamped local builds) |
| GET | `/changelog` | Curated user-facing changelog → `{"releases":[{version,date,entries[]}]}`. `?since=<version>` returns only releases newer than that version (drives the "what's changed" dialog). Generated from git `Changelog:` trailers, embedded at build time |
| GET | `/categories` | All categories (flat list; build tree from `parent_id`). Each carries `value_param` (the slug whose magnitude a bare value encodes; `null` ⇒ not parametric) and the ordered `primary_params` slug list — the curated class profile (see `/admin/categories/{id}/params`). |
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
| POST | `/admin/ldap/diagnose` | `{provider, username, password}` → runs the real search-then-bind + group evaluation and returns a report `{provider, password_verified, error?, steps[], identity?, authorization?, group_checks[], resolution?, warnings[]}`. `group_checks[]` = `{dn, purposes[], exists, exists_err?, member, member_via?}` (per referenced group: does it exist + is the user in it; `member_via` = "direct"/"nested via <dn>"). `resolution.team_memberships[]` = `{team, role, directory_managed}` (the user's current memberships). Membership counts nested groups. **Read-only** — never creates a user, identity, membership, or session. See [auth-ldap.md](auth-ldap.md). |
| POST | `/admin/ldap/inspect` | `{provider, username}` → same report as `diagnose` but resolved by username alone, **without binding as the user** (no password): search, identity, groups, authorization, group existence and account resolution. `password_verified` is `false`. For debugging why a specific account can/can't log in without the user present. **Read-only**. |

### Parameter Registry

| Method | Path | Description |
|--------|------|-------------|
| GET | `/admin/parameter-definitions` | List parameter definitions |
| POST | `/admin/parameter-definitions` | `{slug, display_name, unit?, unit_symbol?, value_type}`. `value_type` ∈ `numeric`/`range`/`text`/`enum`/`enum_set` (`enum_set` = list value, e.g. `CANbus, Ethernet`). |
| PATCH | `/admin/parameter-definitions/{id}` | Update definition fields. Changing `value_type` re-parses every part parameter mapped to this definition. Response includes `reparsed_count`. |
| DELETE | `/admin/parameter-definitions/{id}` | Delete definition; linked part parameters and distributor mappings have their `parameter_id` set to null |
| PATCH | `/admin/categories/{id}/params` | Curate a category's class profile. Body: `{value_param: string\|null, primary_params: string[], generic_code: string\|null}`. `value_param` is the slug whose magnitude a bare value field encodes (`null`/`""` ⇒ not parametric); `primary_params` is the ordered list of relevant parameter slugs. Every referenced slug must exist in the parameter registry (else `400`). `generic_code` is the short type code used to synthesize generic-part MPNs (e.g. `RES`/`CAP`/`IND`); it is trimmed + uppercased, capped at 8 chars, and **requires a `value_param`** (`400` otherwise). `null`/`""` ⇒ generics unsupported. Feeds generic-part interpretation and the LLM parsing prompt. Returns the updated category. |
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
| POST | `/bom/interpret-preview` | Parse mapped BOM rows into structured interpretations (no persistence), for live feedback on the import mapping screen. Body: `{team_id?, project_id?, stream?, items: [{refdes?, footprint?, value?, mpn?, sku?, row?: {<header>: <cell>}}]}` (max 5000). Returns `{items: [{route (`identity`/`parametric`/`ignore`/`unknown`), class, category, package, mounting, params: [{slug, raw, value_si}], flags, confidence, escalate, source, reused, ai?, mpn?, manufacturer?, sku?, distributor_id?}]}`. `source` is `auto` (fresh parse), `reused` (project interpretation memory), or `llm` (LLM fallback). `ai` is the **ephemeral** provenance of an LLM read (review-only, never persisted): `{model?, summary?, reasoning?, reasoning_tokens?}` — `reasoning` text is present only when the provider returns a trace. When `team_id` and `project_id` are supplied (caller must be a member; the project must belong to the team) the project's **remembered corrections** are layered over the fresh parse (`source:reused`). Interpretation memory is **project-scoped** (see §9.2), so `project_id` is required to see reuse. When the LLM fallback is configured (see `llm.*` config), rows the deterministic parser couldn't read cleanly (`escalate:true` — weird/partial value formats like `9k10%`, `5V/10uF`) **or couldn't resolve at all** (unknown / a bare identifier in the value, with no mapped MPN/SKU column) are sent to the model with the full `row` as context. The model either picks a curated category and extracts parameters (generic), or returns the part's **identity** — `manufacturer`+`mpn` and/or `distributor`+`sku`, kept only when those values literally appear in the row — which comes back on the `mpn`/`manufacturer`/`sku`/`distributor_id` fields. All escalations are `source:llm`, bounded (`llm.max_escalations_per_preview`), and cached in interpretation memory, so each row hits the LLM at most once. **Async mode** (`stream:true`): the deterministic/memory readings return immediately along with `job_id` and `pending` (the row indices being read by the LLM); each LLM result then streams over SSE as `bom_interpret_item {index, status:"ok"|"error", item?, error?}` (scoped to the requesting user via the team `/events` stream), terminated by `bom_interpret_complete {total, errors}`. Without `stream`, the LLM fallback runs inline before the response (used by single-row re-parse). |

### Parts Catalog (global)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/parts` | List parts; query: `team_id?`, `q?`, `category_id?`, `package?`, `mpn?`, `lifecycle_status?`, `scope?` (`global`/`team`), `stock_status?`, `param_{id}_min/max`, `param_{id}_values` (for `enum`/`text`/`enum_set` params; matches contains-any), `limit`, `offset`. When `category_id` set, includes a `parameters` object (slug → original unaltered display text, e.g. `"100 Ohm @ 1 MHz"`; normalization is used only for filtering). When `team_id` set, includes inventory fields (`inventory_entry_id`, `quantity_on_hand`, `minimum_stock_quantity`, `reorder_quantity`, `storage_location_label`, `storage_coordinate`) — `storage_coordinate` is the flat `number[]` cell address within the location's `grid_config`, so a list row identifies the actual compartment without a follow-up fetch. Items carry `image_url` (nullable) for thumbnail rendering. |
| POST | `/parts` | Create part. Body: `{scope, team_id?, mpn?, manufacturer?, description?, category?, category_id?, package?, datasheet_url?, lifecycle_status?}`. `scope` required (`global` requires admin; `team` requires team membership + `team_id`). Returns 409 `PART_EXISTS` with existing part if MPN+manufacturer match found. 201. `package` is also normalised into `canonical_package` for parametric matching. |
| POST | `/parts/generic` | Create a **generic (parametric) part** — a team-scoped part, a category, a canonical package, and value parameters. Body: `{team_id, category_id, package?, params: [{slug, value}]}`. Requires team membership. **Create-or-get**: if a generic part with the same category + canonical package + parameter signature exists it is returned (`200 {part_id, existing: true}`) instead of duplicating; otherwise created (`201 {part_id, existing: false}`). `package` is normalised (e.g. `R_0805_2012Metric` → `0805`); params are SI-parsed so they participate in value search. When the category defines a `generic_code` (admin-set; e.g. `RES`/`CAP`/`IND`), a human **identity is synthesized**: `manufacturer = "Generic"`, an MPN like `GEN-RES-10K-0402-1%` (RKM value + package + any secondary params, from `categories.value_param` + ordered `primary_params`), and a description like `10 kΩ ±1% 0402 Resistor`. Categories without a `generic_code` create the part bare (no synthesized identity). |
| POST | `/parts/generic/batch` | **Batch create-or-get generic parts** for one category + package across a value set. Body: `{team_id, category_id, package?, values, params?: [{slug, value}], dry_run?}`. `values` is either `{mode: "eseries", series, lower, upper}` (an IEC 60063 E-series — `E3`…`E192` — expanded across the inclusive SI-aware range, e.g. `lower: "10"`, `upper: "1M"`) or `{mode: "list", list: [...]}` (explicit value strings; comma/newline/`;`-separated entries are split and de-duplicated). `params` are **fixed secondary parameters applied to every part**. Each value becomes one create-or-get generic (same dedup as `/parts/generic`), so re-running never duplicates. Restricted to categories with a `generic_code` (else `400`); capped at 500 values per batch. With `dry_run: true` returns the **plan** without writing: `200 {dry_run: true, total, to_create, existing, items: [{mpn, description, package, value, existing, part_id?}]}`. Otherwise creates in one transaction: `201 {dry_run: false, total, created, existing, items}`. |
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
| GET | `/dashboard` | viewer | Stats, recent activity, alerts, outstanding orders, build pipeline. `recent_activity` is the top 10 **milestone** team-feed entries, not the raw feed — per-line order receipts, bulk part creation during a BOM import, and routine stock corrections are excluded (see `dashboardMilestones` in `internal/handler/dashboard.go`). The unfiltered feed is `/activity-log`. |
| GET | `/members` | admin | List members; each item includes `directory_managed` (true when maintained by LDAP group sync) |
| POST | `/members` | admin | `{user_id, role}` (manual membership) |
| PATCH | `/members/{userId}` | admin | `{role}`; a manual change pins the membership (`directory_managed=false`), preserving it against LDAP sync |
| DELETE | `/members/{userId}` | admin | Remove member |
| POST | `/members/{userId}/deactivate` | admin | Globally deactivate user; clears all sessions/tokens |
| GET | `/distributor-configs` | admin | List configs (secret credential values omitted; includes `priority` (sourcing rank, null if unranked) and `rate_limit` if available) |
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
| POST | `/inventory/bulk-assign-storage` | member | `{part_ids, storage_location_id, storage_coordinate?}` → `{assigned, created}`. Assigns one location (+ optional coordinate) to every part in the batch; parts without an inventory entry get a zero-quantity one. Location must belong to the team (404); every part must be team-visible (403). Drives the list panel's "Assign storage…" verb. |
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
| GET | `/projects/{projectId}/revisions/{revId}` | viewer | Revision detail with line items and substitutes. Each line item includes `resolution_state` (`auto_sku`/`auto_mpn`/`auto_prior_revision`/`auto_generic`/`ambiguous`/`manual`/`ignored`, plus new-part-pipeline states `importing`/`auto_import`/`import_ambiguous`/`import_no_result`) and, when a new part is being imported via the resolve pipeline, `resolve_item: {id, status, task_type, enrichment_data, error_message}`. Resolved-part display fields for rendering the part inline: `resolved_part_mpn`, `resolved_part_manufacturer`, `resolved_part_description`, `resolved_part_package`, `resolved_part_lifecycle_status`, `resolved_part_image_url`, `resolved_part_flag`, plus `quantity_on_hand` (team stock; `null` when no inventory entry exists). When `resolution_state` is `auto_prior_revision`, `origin_resolution_state` + `origin_revision_label` name the decision the reuse descends from, collapsed to a single hop at import time. |
| GET | `/projects/{projectId}/revisions/{revId}/substitutes` | viewer | All substitutes for revision (flat list) |
| GET | `/projects/{projectId}/revisions/{revId}/stock-check` | viewer | Stock check; query: `build_quantity` (required, positive int). Returns `{revision_id, build_quantity, fully_buildable, items}` where each item includes: `quantity_per_board`, `required_quantity`, `quantity_on_hand`, `quantity_reserved`, `quantity_free`, `quantity_on_order`, `shortfall`, `stock_status` (`in_stock`/`partial`/`out_of_stock`), `min_stock_qty`, `below_restock`, `selected_substitute_id/mpn`, `substitute_qty_on_hand`. |
| POST | `/bom/resolve-preview` | viewer | **What would importing this BOM do?** Body: `{project_id?, items: [...]}` where `items` is the same shape `POST /projects/{id}/revisions` accepts (max 1000 — each line walks a ladder of catalog queries). Walks the **real resolution tier ladder** (`internal/bomresolve`, shared with `JobBOMImport`) against the catalog and reports the outcome per line without writing anything — no revision, no line items, no `part_resolve_items`, no inventory entries. Returns `{items: [{index, reference_designators, state, tier, part?, candidates?, route, category, interpretation_source}], summary: {<state>: count}}`. `state` is the `resolution_state` the import would record (`auto_sku`/`auto_mpn`/`auto_mpn_fuzzy`/`auto_prior_revision`/`auto_generic`/`ambiguous`/`importing`/`""`); `tier` names the rung that decided; `candidates` is what an ambiguous line was choosing between. `interpretation_source` is `supplied` (the client sent an `interpretation`), `reused` (project memory), or `auto` (fresh parse). `project_id` enables the prior-revision tier and project memory — **without it those tiers are skipped**, so lines an earlier revision already resolved report as unresolved. Two limits are inherent: an `importing` line's real outcome depends on a distributor API call this does not make, and the forecast is against the catalog as it stands (parts created during an import can change what later lines match). |
| GET | `/projects/{projectId}/interpretation-memory` | viewer | The project's remembered BOM-line interpretations (doc/bom-import.md §9.2) — what future imports will reuse. Returns `{items: [{id, class, value_key, footprint_key, interpretation (the structured reading), created_by, created_by_name, created_at, updated_at}]}`, most-recently-confirmed first. |
| GET | `/parts/categories` | viewer | Categories with direct part counts for team → `{categories: [{id, count}]}`. Used to build the category filter; counts are direct (not recursive); callers aggregate up the tree client-side. |
| GET | `/categories/{categoryId}/parameter-schema` | viewer | A category's curated parameters (its `primary_params`) for value-type-aware editing in the BOM interpretation review UI. Returns `{category_id, slug, value_param, params: [{slug, display_name, unit_symbol, value_type, options}]}` where `options` is the catalog-derived value vocabulary (distinct enum/text values + packages in use, scoped to the team's global+team parts). `package` is an ordinary parameter here (no dedicated field). |
| POST | `/projects` | member | `{name, description?, preferred_distributor_id?, source_url?, project_manager_id?}` → 201 |
| PATCH | `/projects/{projectId}` | member | `{name?, description?, status?, active_bom_revision_id?, bom_import_config?, preferred_distributor_id?, source_url?, project_manager_id?}`. `bom_import_config`: absent=keep, null=clear, object=set. |
| POST | `/projects/{projectId}/revisions` | member | Import BOM → 202 `{job_id, bom_revision_id}`. Body: `{label, notes?, set_active, bom_import_config?, items: [{reference_designators, quantity, raw_description?, manufacturer?, mpn?, name?, sku?, distributor_name?, resolved_distributor_id?, value?, footprint?, interpretation?, interpretation_source?}]}`. `bom_import_config` may include `single_distributor_id` (applied to all SKU rows when the BOM has no distributor column). Each line's `(refdes, footprint, value, mpn, sku)` is run through the genspec parser and the structured **interpretation** is persisted on the line. **Interpretation reuse** (doc/bom-import.md §9.2): when no `interpretation` override is supplied, the line is seeded from prior knowledge — first the project's previous revision (a carried-over `user` edit wins, else the reviewed `auto` reading so a recurring part doesn't re-flag), then the project's **remembered corrections**; otherwise a fresh parse. Supplying an `interpretation` persists it as `user` (the review gate's correction) — or `llm` when `interpretation_source:"llm"` is set (a reading produced by the LLM fallback, see `/bom/interpret-preview`) — and records it as a project-scoped remembered correction for future imports of this project (inspect/prune via `…/projects/{projectId}/interpretation-memory`). If the interpretation carries identity fields (`mpn`/`manufacturer`/`sku`/`distributor_id`, from the LLM specific-part fallback), they are **overlaid onto the line's own columns when those are empty**, so the SKU/MPN satisfaction tiers resolve the line. Enqueues `JobBOMImport`, which resolves known parts inline (SKU/exact-MPN/prior-revision), then a **match-only parametric tier** (`auto_generic`: an existing catalog part in the line's category subtree, matched by canonical package + primary value within an epsilon band, or — for a line with no magnitude (a commodity like an LED or ferrite bead) — by package alone, or — for a package-less line (pin headers) — by its set of discriminating numeric parameters (positions/rows/pitch); a **unique** match resolves, **multiple** matches leave the line `ambiguous` for the review-gate candidate picker, and it never creates a part), then a **fuzzy local-MPN tier** (MPN/alias substring against the local library — the same search the review pane shows; a single hit resolves to it but is **flagged `auto_mpn_fuzzy`** — resolved yet surfaced in the review queue for a one-click confirm since a substring match is less certain than an exact one; multiple leave the line for review, e.g. BOM "AZ431A" → catalog "AZ431AN-ATRG1"), and only when the library has **no** match hands **new** identity parts to the distributor resolve pipeline (`part_resolve_items`); unambiguous new parts auto-commit, ambiguous/failed ones surface for review. |
| PATCH | `/projects/{projectId}/revisions/{revId}` | member | `{label?, notes?}` |
| PATCH | `.../revisions/{revId}/items/{itemId}` | member | `{resolved_part_id?, ignored?}`. Setting `resolved_part_id` marks resolution as `manual`. |
| POST | `.../items/{itemId}/enrich` | member | `{sku, distributor_id}` → `{part_id, part_mpn, part_manufacturer}`. Looks up locally first, falls back to distributor API. Creates inventory entry and resolves the BOM line item. Returns 422 if distributor not configured; 502 on API error. |

### Substitutes

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `.../items/{itemId}/substitutes` | viewer | List substitutes for a line item with inventory data (`{items: [{part_id, part_mpn, priority, manufacturer, description, lifecycle_status, quantity_on_hand}]}`) |
| GET | `.../items/{itemId}/substitutes/previous-revisions` | viewer | Revisions in the same project that have substitutes for this line item's part |
| GET | `.../items/{itemId}/generic-candidates` | viewer | For a line read as a generic (parametric) spec, the existing catalog parts that satisfy it — by category subtree + canonical package + primary value within the tolerance band, or, for a line with no magnitude, by package alone, or, for a package-less line, by its set of discriminating numeric parameters — the **same set** the import-time match-only tier considered (which auto-resolves only on a unique match). Powers the review-gate candidate picker for ambiguous lines. `{items: [{part_id, mpn, manufacturer, package, description, value_si, scope, quantity_on_hand}]}`, ordered by closeness to nominal (by MPN when the line carries no nominal); `value_si` is null for package-only / signature matches; `quantity_on_hand` is the active team's on-hand stock for the part (null when there's no inventory entry), so the picker can surface which candidates are in stock; empty for non-parametric lines or no match. Resolve via `PATCH .../items/{itemId}` with the chosen `part_id`. |
| POST | `.../items/{itemId}/substitutes` | member | `{part_id}` → 201. Returns 409 if already exists. |
| DELETE | `.../items/{itemId}/substitutes/{partId}` | member | 204 (idempotent) |
| PUT | `.../items/{itemId}/substitutes/order` | member | `{part_ids: [...]}` — reorder priorities (index 0 = highest); 204 |
| POST | `.../items/{itemId}/substitutes/import` | member | `{source_revision_id}` — copy substitutes from another revision; 204 |
| DELETE | `/projects/{projectId}/interpretation-memory/{memoryId}` | member | Forget one remembered interpretation (doc/bom-import.md §9.2) so future imports of this project re-parse that line fresh. 204; 404 if the id is unknown or belongs to another project. Does not alter already-imported revisions. |

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

File attachments (datasheets, images, office files, Touchstone S-parameters, …) pinned to a **part** or **project** and listed **separately from note markdown**. They reuse the blob/attachment layer: ownership is recorded on the `attachments` row (`owner_kind`/`owner_id`); a note-embedded image leaves the owner NULL and is not a document. Classification is by **file extension** (office/Touchstone files have no distinctive magic bytes); PDFs and raster images are **served inline** (and verified to actually sniff as an inline-safe type). `.html`/`.htm` (interactive BOMs) are also served inline but inside a **CSP sandbox** (`Content-Security-Policy: sandbox allow-scripts; …; connect-src 'none'`) — an opaque origin with scripts enabled but **no access to cookies/our API/the parent DOM and no network egress**, enforced even on direct navigation, so they execute only when contained, never in our origin. They're allowed only when `.html`/`.htm` is in `allowed_document_extensions` (omitted from the defaults — opt in). Every other type is **forced to download** so its bytes can't execute.

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/parts/{partId}/documents` | viewer | List the team's documents on the part (team-scoped; global part visible). Returns `{documents: [{id, filename, content_type, byte_size, viewable, uploaded_by?, uploaded_at?}]}`, newest first. `viewable` = PDF, raster image, or (sandboxed) HTML. 404 if the part isn't visible to the team. |
| POST | `/parts/{partId}/documents` | member | Upload a document. Body is the **raw file bytes**; `?filename=` is **required** (drives extension classification + the download name). 201 with the document row. 415 unsupported extension or content not matching an inline type; 413 over the cap (default 25 MiB). |
| GET | `/projects/{projectId}/documents` | viewer | List the project's documents. Same shape; 404 if not the team's. |
| POST | `/projects/{projectId}/documents` | member | Upload a document to the project. 404 if not the team's. |
| GET | `/documents/{docId}/content` | viewer | Serve the bytes — **access-controlled** (resolved team-scoped) and filename-aware, unlike `/blobs/{sha}`. `Content-Disposition: inline` for PDF/image/HTML, else `attachment`; HTML additionally gets the sandbox `Content-Security-Policy` header (see above); pass `?download=1` to force download with the original filename. Strong `ETag` (blob hash), `If-None-Match`→304, `nosniff`, `Cache-Control: private, immutable`. `HEAD` supported. 404 cross-team. |
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
| GET | `/builds/{buildId}` | viewer | Build details with `allocations`, `order_ids`, `substitute_selections`, `selections_stale`, `excluded_designators`, `line_item_overrides`, and (if assembled) `items` showing per-part deductions. Allocation and deduction items carry the full PartCell identity set — `part_mpn`, `part_manufacturer`, `part_lifecycle_status`, `part_image_url`, `part_description` (short description, falling back to full) |
| GET | `/builds/{buildId}/preview` | viewer | Preview for a saved draft/pending build; accounts for `excluded_designators` and `line_item_overrides`. Items include `resolved_part_description`, `effective_quantity_per_board`, `effective_total_quantity`, `excluded_designator_count`, `override` (`use_available`/`skip`/`null`), `available_quantity` |
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
| POST | `/cart/items/bulk` | purchaser | `{items: [{part_id, quantity, listing_id?}]}` → `{added}`. Upserts all items in one tx (quantities merge per part, `source=manual`); one `cart_changed` event. Used by "Add from list". |
| PATCH | `/cart/items/{itemId}` | purchaser | `{quantity?, listing_id?}` → 204. Demand floors enforced for `bom_shortfall`/`inventory_maintenance` sources. |
| DELETE | `/cart/items/{itemId}` | purchaser | Remove item; 204 |
| POST | `/cart/add-low-stock` | purchaser | Add items to reach reorder levels → `{added_count}` |
| POST | `/cart/refresh-listings` | purchaser | Enqueue refresh for all cart listings → 202 `{job_id, listing_count}` (or 204 if nothing to refresh). Progress streams over SSE: per-listing `listing_refresh_done` then a terminal `listing_refresh_complete` `{total, done, unavailable, failed}`. |
| POST | `/cart/convert` | purchaser | Convert to orders grouped by distributor; links pending builds, creates allocations, transitions pending→ordered/ready. Returns 422 if cart empty or items missing listings. → `{order_ids}` |
| GET | `/sourcing-policy` | purchaser | Team sourcing policy → `{strategy, cost_tolerance_pct, cost_tolerance_abs, distributors: [{distributor_id, distributor_name, enabled, priority}]}` |
| PUT | `/sourcing-policy` | purchaser | Replace policy. Body: `{strategy: "cost"\|"preferred_in_stock", cost_tolerance_pct?, cost_tolerance_abs?, distributor_priority: [distributorId,...]}`. `distributor_priority` is the ordered ranking (index+1 = priority); rank 1 also syncs the legacy `preferred` flag. Invalidates cached cart resolution. → same shape as GET |

**Listing auto-resolution.** Cart items with no explicit `listing_id` are resolved to a listing on read/convert. Under `cost` (default) the cheapest total-cost listing wins. Under `preferred_in_stock` the highest-ranked distributor that has the quantity in stock wins, falling through the ranking when a distributor is out of stock and finally to cheapest when none qualify; an optional cost guardrail (`cost_tolerance_pct`/`abs`) vetoes an over-expensive preferred pick. The resolved `listing` object carries `reason` (`cost`/`only_option`/`preferred`/`preferred_fallback`/`out_of_stock_fallback`/`over_tolerance`) and `preferred_stale` (preference honoured on missing/stale stock data).

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

### Part Collections ("Lists")

User-private within a team: every route filters to the acting user, so one member's
lists are invisible to teammates (foreign ids → 404). Items reference resolved catalog
parts only (global or this team's; foreign team-scoped parts → 403). All mutations
publish a `collection_changed` SSE event **targeted at the owner only** (delivered to
all of the owner's connections, nobody else's).

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/collections` | member | Own lists, recently-used first → `{collections: [{id, name, archived, collect_capture_qty, created_at, updated_at, item_count, total_quantity, unquantified_count}]}`. Query: `archived=true` includes archived. |
| POST | `/collections` | member | `{name?}` → 201. Blank name is auto-named ("Untitled list — Jul 17"). |
| GET | `/collections/{collectionId}` | member | List + items in insertion order → `{..., items: [{id, part_id, quantity, note, mpn, manufacturer, description, lifecycle_status, image_url, available_qty}]}`. `available_qty` = on-hand − soft reservations (null when the part isn't tracked); drives the row's stock dot. |
| PATCH | `/collections/{collectionId}` | member | `{name?, archived?, collect_capture_qty?}` → updated list |
| DELETE | `/collections/{collectionId}` | member | Hard delete; 204 (archive via PATCH is the soft path) |
| POST | `/collections/{collectionId}/items` | member | `{part_id, quantity?, note?}` → 201. Upsert-merge: an incoming quantity **sums** onto the row (NULL counts as 0); a bare add leaves the stored quantity untouched. Quantity must be > 0 when present (NULL = "no quantity recorded"). |
| POST | `/collections/{collectionId}/items/bulk` | member | `{items: [{part_id, quantity?, note?}]}` → `{added}`. Same merge rule, one tx + one event. Used by the panel's "Add from …" source verbs (cart, build, order, revision, storage location, low stock, other list). |
| POST | `/collections/{collectionId}/items/from-search` | member | Adds every part matching the current parts-search filters — same query params as `GET /parts` (`q`, `mpn`, `category_id`, `package`, `lifecycle_status`, `scope`, `stock_status`, `storage_location_id`, `storage_cells`, `param_*`) — as bare membership (no quantity), across all pages, one tx + one event → `{added, matched}`. Idempotent (upsert). Returns 400 `TOO_MANY_PARTS` above 2000 matches. Powers the list panel's "From the parts search" source verb (the parts view publishes its current search for the panel to consume). |
| DELETE | `/collections/{collectionId}/items` | member | Clear all rows (the "Clear items" verb) → `{removed}`. The collection itself survives. |
| PATCH | `/collections/{collectionId}/items/{itemId}` | member | `{quantity?, note?}` — set-if-present: explicit `null` clears the field, absent leaves it untouched |
| DELETE | `/collections/{collectionId}/items/{itemId}` | member | Remove row; 204 |

### Search & Events

| Method | Path | Role | Description |
|--------|------|------|-------------|
| GET | `/search?q=` | viewer | Full-text search across parts, projects, BOM revisions, orders, builds, storage (min 2 chars). Returns `{parts, projects, revisions, orders, builds, storage_locations}` |
| GET | `/events` | viewer | SSE stream. Events: `part_changed`, `inventory_changed`, `cart_changed`, `order_changed`, `storage_changed`, `project_changed`, `revision_changed`, `build_changed`. Events from the acting user's own actions are skipped. Exception: `collection_changed` is delivered ONLY to the owning user (all their connections, including the acting one) since collections are private. |

---

## Mobile Scanner

### Desktop-facing (session cookie / API token auth)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| POST | `/teams/{teamId}/scan-sessions` | member+ | Create scan session. Body: `{order_id?: string}`. Returns `{id, pairing_ticket, pairing_ticket_expires_in, expires_at}`. The durable token is NOT returned here — the phone obtains it by redeeming `pairing_ticket` at `POST /scan/pair`. Durable token TTL 24h. |
| POST | `/teams/{teamId}/scan-sessions/{sessionId}/pairing-ticket` | member+ | Mint a fresh single-use pairing ticket for an existing session (desktop refreshes the QR before the 60s TTL lapses). Returns `{pairing_ticket, pairing_ticket_expires_in}`. |
| GET | `/teams/{teamId}/scan-sessions/active` | member+ | Get current user's active scan session. Returns `{id, phone_connected, context_type, context_order_id, context_order_ref, phone_ip, phone_ua}` or 204 if none. |
| DELETE | `/teams/{teamId}/scan-sessions/{sessionId}` | purchaser+ | Delete scan session. |
| PATCH | `/teams/{teamId}/scan-sessions/{sessionId}/context` | member+ | Push a context to the phone (SSE, immediate). Body: `{order_id}` (receive flow) or `{collection_id}` (Collect flow). Collection pushes require the session to belong to the acting user (lists are private) and return `{ok, context_collection_name}`; order pushes return `{ok, context_order_ref}`. |
| POST | `/teams/{teamId}/scan-sessions/{sessionId}/kitting` | member+ | Start kitting session for a build. Body: `{build_id}`. Any non-terminal build status (kitting is advisory; 409 `INVALID_BUILD_STATUS` only for assembled/cancelled). Seeds confirmations from BOM demand (resolved line items + overrides + substitute selections × build qty); 409 `EMPTY_KITTING` if the build has no resolvable demand. Pushes `scan_context_changed` to phone. Returns `{kitting_session_id}`. |
| GET | `/teams/{teamId}/activity-log` | viewer+ | Paginated unified activity feed. Default scope = the caller's own activity; `?scope=team` (purchaser+) returns the team-wide feed (`feed='team'` rows). Query: `action` (comma-separated), `cursor_time`, `cursor_id`, `limit` (default 50). Each item: `{id, action, feed, source, actor_label, subject:{type,id,label}|null, summary, payload, created_at, part_*}`. Returns `{items:[...], next_cursor_time, next_cursor_id}`. |
| GET | `/teams/{teamId}/object-refs` | viewer+ | Batch-resolve object ids to display summaries. Query: one param per kind, each a comma-separated id list — `?part=<uuid>,<uuid>&order=<uuid>&build=<uuid>&project=<uuid>&revision=<uuid>&storage=<uuid>`. Max 200 ids per request (400 `TOO_MANY_IDS` beyond). Returns `{objects: {"<id>": {kind, id, label, qualifier?, status?, href?}}}`. `label` is the identity that reads in a sentence ("Blinky rev B", "DigiKey #PO-1042"); `qualifier` is the short disambiguator a dense row adds ("×10"). Ids the team cannot see, or that no longer exist, are simply absent — not an error. Formatting is shared with the activity feed and note chips (`internal/objectref`). |
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
| GET | `/scan/collections` | Token user's active lists (Collect picker). Returns `{collections:[{id, name, collect_capture_qty, item_count, total_quantity, updated_at}]}`. Member+ only. |
| POST | `/scan/collections` | Create a list from the phone. Body: `{name?}` (blank auto-names). Member+ only. |
| PATCH | `/scan/collections/{collectionId}` | Persist the quantity-capture toggle. Body: `{collect_capture_qty}`. Member+ only. |
| POST | `/scan/collect` | One Collect scan. Body: `{collection_id, barcode?}` or (after a quantity prompt) `{collection_id, part_id, quantity?}` — `quantity` absent = bare membership. **Resolve-only**: reuses the lookup chain but never creates inventory entries. Returns discriminated union on `status`: `added` (part, quantity_added, item_quantity, tally), `quantity_required` (capture on, barcode carried no qty; nothing written), `unknown` (`reason: unparsed\|not_in_catalog`, parsed fields). Publishes owner-targeted `collection_changed` + `scanner_activity`. Member+ only. |
| GET | `/scan/collections/{collectionId}/items` | Phone list view: rows + team stock + storage location → `{name, items:[{id, part_id, quantity, note, mpn, description, quantity_on_hand, storage_location:{id, coordinate}\|null}]}`. |
| POST | `/scan/check/target` | Resolve a filter target set once at session start. Body: `{source: {type:"collection", collection_id} \| {type:"build", build_id} \| {type:"order", order_ids:[...]}}` → `{ref, entries:[{part_id, mpn, description, qty_needed?, note?, designators?, substitute?, substitute_for?, substitute_for_part_id?, order_ref?}]}`. Build source = computeBuildDemand (overrides, exclusions, selections × qty) + approved substitutes; derived targets are never persisted. |
| POST | `/scan/check` | One Filter scan (strictly read-only). Body: `{barcode, source, progress?: {done, total}}` → `{verdict: match\|substitute\|miss\|unknown, part, entry?}`. Order sources match at listing level (SKU) first, then part level. The progress echo rides into an owner-targeted `scanner_activity` event. |
| POST | `/scan/check/finish` | Record the filter session summary. Body: `{ref, seen, total}` → 204. Writes one activity entry ("Filter finished: 28/40 parts of X seen") + a terminal `scanner_activity`. |
| GET | `/scan/builds` | Filter-source build picker: all builds, active first → `{builds:[{id, ref, status, quantity, created_at}]}`. |
| POST | `/scan/bulk/start` | Create bulk scan session. Returns `{session_id, item_count}`. |
| POST | `/scan/bulk/sessions/{sessionId}/items` | Add a scanned barcode to the bulk session. Body: `{barcode}`. Deduped by barcode within active items. Performs synchronous DB lookup; creates a resolve job for unknown parts. Returns `{scan_item_id, found_in_db: bool, task_type: string\|null, encoded_qty: int\|null, resolve_item_id: uuid\|null}`. |
| PATCH | `/scan/bulk/sessions/{sessionId}/items/{itemId}` | Set initial quantity. Body: `{initial_qty: int\|null}`. Returns 204. |
| POST | `/scan/bulk/sessions/{sessionId}/close` | Close session (exit bulk mode). Sets `closed_at`. Returns 204. |
| POST | `/scan/bulk/sessions/{sessionId}/navigate` | Push `scan_focus_requested` to desktop with `entity_type=bulk_scan_session`. Returns 204 or 409 if offline. |
| GET | `/scan/kitting/{kittingSessionId}` | Get kitting session info + progress counts. Returns `{session:{id,build_id,build_ref,status}, progress:{pending,confirmed,shortfall,skipped}}`. |
| GET | `/scan/kitting/{kittingSessionId}/items` | List kitting items with storage location. Returns `{items:[{part_id,part_mpn,part_manufacturer,part_description,quantity_needed,quantity_confirmed,status,storage_location_id,storage_location_path,storage_coordinate}]}`. |
| POST | `/scan/kitting/{kittingSessionId}/confirm` | Mark item confirmed. Body: `{part_id, quantity_confirmed}` (`quantity_confirmed` >= 0). Returns `{item, progress}`. All four kitting mutations 409 `KITTING_NOT_ACTIVE` if the session is not `in_progress`. |
| POST | `/scan/kitting/{kittingSessionId}/shortfall` | Mark item shortfall. Body: `{part_id, quantity_available}` (0 <= `quantity_available` < `quantity_needed`; 409 `NOT_A_SHORTFALL` if it meets/exceeds need — that's a confirm). Returns `{item, progress}`. |
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
