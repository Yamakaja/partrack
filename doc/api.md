# partrack API

> REST API reference for partrack. Consumed by the Svelte frontend and by external API token clients.

POTENTIALLY OUTDATED!

---

## 1. Conventions

### Base path

All endpoints are prefixed with `/api/v1`.

### Authentication

Two credential mechanisms are accepted on every authenticated request:

| Mechanism | How |
|---|---|
| Session cookie | HTTP-only `session` cookie set after login. Used by the browser client automatically. |
| Bearer token | `Authorization: Bearer <token>` header. Accepts both session tokens and API tokens. Used by programmatic clients. |

Unauthenticated requests to protected endpoints return `401`.

### Team context

Every team-scoped resource carries the team ID in the URL path:

```
/api/v1/teams/{teamId}/inventory
/api/v1/teams/{teamId}/projects
```

The caller must be a member of `{teamId}` (any role), otherwise `403` is returned.

### Role abbreviations

Roles are strictly ordered — each is a superset of the one below.

| Abbreviation | Role | Capabilities |
|---|---|---|
| `V` | viewer+ | Read-only access to all team resources |
| `M` | member+ | Inventory ops, BOM import, builds, checkouts |
| `P` | purchaser+ | Full procurement lifecycle (cart, orders, receipt) |
| `A` | admin | Team/user management, distributor configuration |

### Pagination

List endpoints accept `limit` (default 50, max 200) and `offset` (default 0) as query parameters and return a standard envelope:

```json
{
  "total": 142,
  "limit": 50,
  "offset": 0,
  "items": [ ... ]
}
```

### Timestamps

RFC 3339 format in UTC, e.g. `"2024-03-15T10:30:00Z"`.

### Monetary values

Prices and costs are JSON strings encoding a fixed-point decimal, e.g. `"10.4500"`. This avoids IEEE 754 precision loss.

### UUIDs

Lowercase hex with dashes: `"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`.

### Errors

```json
{
  "error": "Human-readable description of what went wrong.",
  "code": "MACHINE_READABLE_CODE"
}
```

### HTTP status codes

| Code | Meaning |
|---|---|
| 200 | Successful read or update |
| 201 | Resource created; `Location` header points to new resource |
| 202 | Accepted — async job started; body: `{ "job_id": "uuid" }` |
| 204 | Success, no response body (used on DELETE) |
| 400 | Malformed request or validation failure |
| 401 | No valid session or token |
| 403 | Authenticated but insufficient role |
| 404 | Resource not found |
| 409 | Conflict (e.g. duplicate unique value) |
| 422 | Semantically invalid (e.g. received quantity exceeds ordered quantity) |

---

## 2. Authentication

### `POST /api/v1/auth/login`

Internal username/password login. On success, sets an HTTP-only `session` cookie and returns the user object.

**Request body**
```json
{ "email": "user@example.com", "password": "..." }
```

**Response `200`**
```json
{
  "user": {
    "id": "uuid",
    "display_name": "Alice",
    "email": "user@example.com",
    "is_active": true,
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

**Errors:** `401` — wrong credentials or inactive account.

---

### `POST /api/v1/auth/logout`

Invalidates the current session. Clears the cookie.

**Min role:** authenticated

**Response `204`**

---

### `GET /api/v1/auth/oidc/{provider}`

Initiates an OIDC authorization flow. Redirects (302) to the provider's authorization endpoint. `{provider}` matches the slug configured in the application config file.

---

### `GET /api/v1/auth/oidc/{provider}/callback`

OIDC authorization code callback. Exchanges the code, identifies or provisions the user, creates a session, sets the cookie, and redirects to the frontend.

---

### `GET /api/v1/auth/me`

Returns the current user's profile and team memberships.

**Min role:** authenticated

**Response `200`**
```json
{
  "user": {
    "id": "uuid",
    "display_name": "Alice",
    "email": "user@example.com",
    "is_active": true,
    "created_at": "2024-01-01T00:00:00Z"
  },
  "memberships": [
    { "team_id": "uuid", "team_name": "Acme Hardware", "role": "admin" }
  ]
}
```

---

## 3. Session & Token Management

### `GET /api/v1/sessions`

List the current user's active sessions.

**Min role:** authenticated

**Response `200`** — paginated list of:
```json
{
  "id": "uuid",
  "created_at": "...",
  "expires_at": "...",
  "last_seen_at": "...",
  "user_agent": "Mozilla/5.0 ...",
  "ip_address": "203.0.113.1",
  "is_current": true
}
```

---

### `DELETE /api/v1/sessions/{sessionId}`

Revoke a session. Users can only revoke their own sessions.

**Min role:** authenticated

**Response `204`**

---

### `GET /api/v1/tokens`

List the current user's API tokens. Token hashes are never returned.

**Min role:** authenticated

**Response `200`** — paginated list of:
```json
{
  "id": "uuid",
  "name": "CI pipeline",
  "created_at": "...",
  "last_used_at": "...",
  "expires_at": null
}
```

---

### `POST /api/v1/tokens`

Create an API token. The raw token is returned exactly once — it cannot be retrieved again.

**Min role:** authenticated

**Request body**
```json
{ "name": "CI pipeline", "expires_at": "2025-01-01T00:00:00Z" }
```
`expires_at` is optional.

**Response `201`**
```json
{
  "id": "uuid",
  "name": "CI pipeline",
  "token": "ptk_...",
  "created_at": "...",
  "expires_at": null
}
```

---

### `DELETE /api/v1/tokens/{tokenId}`

Revoke an API token.

**Min role:** authenticated

**Response `204`**

---

## 4. User Preferences

User preferences are a per-user opaque JSON object. The backend stores and returns the blob verbatim — it never inspects individual keys. Content is not confidential. Only data the user may freely modify should be stored here (UI state, locale, theme, column preferences, etc.).

### `GET /api/v1/preferences`

Returns the current user's preferences. Returns `{}` if no preferences have been saved yet.

**Min role:** authenticated

**Response `200`**
```json
{ "preferences": { "theme": "dark", "locale": "en-GB" } }
```

---

### `PUT /api/v1/preferences`

Replaces the entire preferences blob. The body must be a valid JSON object.

**Min role:** authenticated

**Request body** — any valid JSON object:
```json
{ "theme": "dark", "locale": "en-GB", "bom_default_qty_col": "Qty" }
```

**Response `200`** — same shape as GET:
```json
{ "preferences": { "theme": "dark", "locale": "en-GB", "bom_default_qty_col": "Qty" } }
```

**Errors:** `400` — body is not a JSON object (arrays, primitives, and `null` are rejected).

---

## 5. Teams & Membership

### `GET /api/v1/teams`

List teams the current user is a member of.

**Min role:** authenticated

**Response `200`** — paginated list of:
```json
{
  "id": "uuid",
  "name": "Acme Hardware",
  "description": "...",
  "created_at": "...",
  "role": "admin"
}
```

---

### `POST /api/v1/teams`

Create a team. The caller is automatically added as `admin`.

**Min role:** authenticated

**Request body**
```json
{ "name": "Acme Hardware", "description": "Optional description" }
```

**Response `201`**

---

### `GET /api/v1/teams/{teamId}`

Get team details.

**Min role:** V

**Response `200`**
```json
{
  "id": "uuid",
  "name": "Acme Hardware",
  "description": "...",
  "created_at": "..."
}
```

---

### `PATCH /api/v1/teams/{teamId}`

Update team name or description.

**Min role:** A

**Request body** (all fields optional)
```json
{ "name": "New Name", "description": "New description" }
```

**Response `200`** — updated team object.

---

### `GET /api/v1/teams/{teamId}/members`

List team members with their roles.

**Min role:** V

**Response `200`** — paginated list of:
```json
{
  "user_id": "uuid",
  "display_name": "Alice",
  "email": "alice@example.com",
  "role": "admin",
  "joined_at": "..."
}
```

---

### `POST /api/v1/teams/{teamId}/members`

Add an existing user to the team.

**Min role:** A

**Request body**
```json
{ "user_id": "uuid", "role": "member" }
```

**Response `201`**

**Errors:** `404` — user not found. `409` — user already a member.

---

### `PATCH /api/v1/teams/{teamId}/members/{userId}`

Change a member's role.

**Min role:** A

**Request body**
```json
{ "role": "purchaser" }
```

**Response `200`** — updated membership object.

---

### `DELETE /api/v1/teams/{teamId}/members/{userId}`

Remove a member from the team.

**Min role:** A

**Response `204`**

---

### `POST /api/v1/teams/{teamId}/members/{userId}/deactivate`

Globally deactivates the user account (`is_active = false`). The user cannot log in to any team after this. Their historical records are preserved.

**Min role:** A

**Response `204`**

---

## 6. Distributors & Configuration

### `GET /api/v1/distributors`

List all distributors known to the system.

**Min role:** authenticated

**Response `200`** — paginated list of:
```json
{
  "id": "uuid",
  "name": "DigiKey",
  "website": "https://www.digikey.com",
  "api_type": "digikey_v3"
}
```
`api_type` is `null` for distributors with no API integration.

---

### `GET /api/v1/teams/{teamId}/distributor-configs`

List the team's distributor configurations.

**Min role:** A

**Response `200`** — list of:
```json
{
  "distributor_id": "uuid",
  "distributor_name": "DigiKey",
  "enabled": true,
  "preferred": false,
  "cache_duration_seconds": 3600,
  "has_credentials": true
}
```
`api_credentials` is never returned.

---

### `PUT /api/v1/teams/{teamId}/distributor-configs/{distributorId}`

Create or replace the team's config for a distributor.

**Min role:** A

**Request body**
```json
{
  "enabled": true,
  "preferred": false,
  "cache_duration_seconds": 3600,
  "api_credentials": { "client_id": "...", "client_secret": "..." }
}
```
`api_credentials` is optional — omit to leave existing credentials unchanged, send `null` to clear them.

**Response `200`**

---

### `DELETE /api/v1/teams/{teamId}/distributor-configs/{distributorId}`

Remove the team's configuration for a distributor.

**Min role:** A

**Response `204`**

---

## 7. Parts Catalog

Parts are either `global` (visible to all teams) or `team`-scoped (visible only to that team's members).

### `GET /api/v1/parts`

Search the parts catalog. To include team-scoped parts, supply `team_id`.

**Min role:** authenticated

**Query parameters**

| Param | Description |
|---|---|
| `team_id` | Include team-scoped parts for this team (must be a member). |
| `q` | Full-text search across MPN, description, manufacturer. |
| `mpn` | Exact MPN match. |
| `category` | Filter by category. |
| `package` | Filter by package. |
| `lifecycle_status` | Filter by lifecycle status. |
| `scope` | `global` or `team`. |
| `limit`, `offset` | Pagination. |

**Response `200`** — paginated list of part summaries:
```json
{
  "id": "uuid",
  "scope": "global",
  "mpn": "RC0402FR-0710KL",
  "manufacturer": "Yageo",
  "description": "RES 10K OHM 1% 1/16W 0402",
  "category": "Resistor",
  "package": "0402",
  "lifecycle_status": "active"
}
```

---

### `POST /api/v1/parts`

Create a part.

**Min role:** M (for `team`-scoped); authenticated (for `global`, but see note)

> Global parts are typically created by the system during BOM import and enrichment. Manual creation of global parts is restricted to avoid pollution of the shared catalog. The exact restriction is an implementation concern.

**Request body**
```json
{
  "scope": "team",
  "team_id": "uuid",
  "mpn": "MY-CUSTOM-PART",
  "manufacturer": "In-house",
  "description": "Custom wound inductor",
  "category": "Inductor",
  "package": "Custom",
  "datasheet_url": null,
  "lifecycle_status": "active"
}
```

**Response `201`** — created part object.

---

### `GET /api/v1/parts/{partId}`

Full part detail: metadata, all parameters, and all distributor listings.

**Min role:** authenticated

**Response `200`**
```json
{
  "id": "uuid",
  "scope": "global",
  "team_id": null,
  "mpn": "RC0402FR-0710KL",
  "manufacturer": "Yageo",
  "description": "RES 10K OHM 1% 1/16W 0402",
  "category": "Resistor",
  "package": "0402",
  "datasheet_url": "https://...",
  "lifecycle_status": "active",
  "enrichment_source_id": "uuid",
  "enriched_at": "...",
  "preferred_parameter_source_id": null,
  "created_at": "...",
  "parameters": [ ... ],
  "listings": [ ... ]
}
```

---

### `PATCH /api/v1/parts/{partId}`

Update part metadata. All fields are optional.

**Min role:** M

**Request body**
```json
{
  "description": "Updated description",
  "category": "Resistor",
  "package": "0402",
  "datasheet_url": "https://...",
  "lifecycle_status": "nrnd",
  "preferred_parameter_source_id": "uuid"
}
```

**Response `200`** — updated part object.

---

### `POST /api/v1/parts/{partId}/enrich`

Trigger re-enrichment of a part from distributor APIs. Fetches fresh metadata, parameters, and listings from all enabled distributors with an API integration.

**Min role:** M

**Response `202`**
```json
{ "job_id": "uuid" }
```

Progress arrives on the SSE stream as `part_enrichment_progress` events.

---

### `GET /api/v1/parts/{partId}/parameters`

List all parameters for a part, grouped by source.

**Min role:** authenticated

**Response `200`**
```json
{
  "user_specified": [
    { "id": "uuid", "name": "resistance", "value": "10k", "unit": "Ω" }
  ],
  "distributor_sourced": [
    {
      "id": "uuid",
      "name": "resistance",
      "value": "10k",
      "unit": "Ω",
      "source_distributor_id": "uuid",
      "source_distributor_name": "DigiKey"
    }
  ]
}
```

---

### `POST /api/v1/parts/{partId}/parameters`

Add or update a user-specified parameter. If a user-specified parameter with the same `name` already exists, it is replaced.

**Min role:** M

**Request body**
```json
{ "name": "resistance", "value": "10k", "unit": "Ω" }
```

**Response `201`** — created/updated parameter object.

---

### `DELETE /api/v1/parts/{partId}/parameters/{parameterId}`

Delete a user-specified parameter. Distributor-sourced parameters cannot be deleted via the API.

**Min role:** M

**Response `204`**

---

### `GET /api/v1/parts/{partId}/listings`

List all distributor listings for a part.

**Min role:** authenticated

**Response `200`** — list of:
```json
{
  "id": "uuid",
  "distributor_id": "uuid",
  "distributor_name": "DigiKey",
  "sku": "311-10KARCT-ND",
  "packaging_type": "Cut Tape",
  "unit_price": "0.0120",
  "stock_available": 50000,
  "moq": 1,
  "fixed_order_cost": "0.0000",
  "last_refreshed_at": "..."
}
```
`unit_price` and `stock_available` are `null` before the first refresh.

---

### `POST /api/v1/parts/{partId}/listings/{listingId}/refresh`

Trigger a price and stock refresh for a specific distributor listing.

**Min role:** M

**Response `202`**
```json
{ "job_id": "uuid" }
```

---

## 8. Inventory

### `GET /api/v1/teams/{teamId}/inventory`

List inventory entries for the team.

**Min role:** V

**Query parameters**

| Param | Description |
|---|---|
| `q` | Search by part MPN, description, or manufacturer. |
| `storage_location_id` | Filter by storage location. |
| `below_min_stock` | `true` to return only entries below their minimum stock quantity. |
| `limit`, `offset` | Pagination. |

**Response `200`** — paginated list of:
```json
{
  "id": "uuid",
  "part_id": "uuid",
  "part_mpn": "RC0402FR-0710KL",
  "part_description": "RES 10K OHM 1% 1/16W 0402",
  "quantity_on_hand": 150,
  "storage_location_id": "uuid",
  "storage_location_label": "A3",
  "minimum_stock_quantity": 100,
  "reorder_quantity": 500
}
```

---

### `POST /api/v1/teams/{teamId}/inventory`

Start tracking a part. Creates an inventory entry with `quantity_on_hand = 0`.

**Min role:** M

**Request body**
```json
{
  "part_id": "uuid",
  "storage_location_id": "uuid",
  "preferred_listing_id": "uuid",
  "minimum_stock_quantity": 100,
  "reorder_quantity": 500
}
```
All fields except `part_id` are optional.

**Response `201`** — created inventory entry object.

**Errors:** `409` — entry already exists for this part in this team.

---

### `GET /api/v1/teams/{teamId}/inventory/{entryId}`

Full inventory entry detail.

**Min role:** V

**Response `200`**
```json
{
  "id": "uuid",
  "team_id": "uuid",
  "part_id": "uuid",
  "part": { ... },
  "quantity_on_hand": 150,
  "storage_location_id": "uuid",
  "storage_location_label": "A3",
  "preferred_listing_id": "uuid",
  "minimum_stock_quantity": 100,
  "reorder_quantity": 500
}
```

---

### `PATCH /api/v1/teams/{teamId}/inventory/{entryId}`

Update inventory entry preferences. All fields optional.

**Min role:** M

**Request body**
```json
{
  "storage_location_id": "uuid",
  "preferred_listing_id": "uuid",
  "minimum_stock_quantity": 200,
  "reorder_quantity": 1000
}
```
Pass `null` to clear an optional field.

**Response `200`** — updated entry object.

---

### `GET /api/v1/teams/{teamId}/inventory/{entryId}/adjustments`

List stock adjustment history for an inventory entry, newest first.

**Min role:** V

**Response `200`** — paginated list of:
```json
{
  "id": "uuid",
  "delta": -5,
  "adjusted_at": "...",
  "reason": null,
  "created_by_user_id": "uuid",
  "created_by_display_name": "Alice",
  "source_type": "build",
  "source_id": "uuid"
}
```
`source_type` is one of `order_receipt`, `build`, `general_checkout`, or `manual`. `source_id` is the relevant entity's ID; both are `null` for manual adjustments.

---

### `POST /api/v1/teams/{teamId}/inventory/{entryId}/adjustments`

Record a manual stock adjustment.

**Min role:** M

**Request body**
```json
{ "delta": -3, "reason": "Damaged components discarded" }
```
`delta` may be positive or negative. `reason` is required for manual adjustments.

**Response `201`** — created adjustment object, plus updated `quantity_on_hand`.

---

### `GET /api/v1/teams/{teamId}/inventory/replenish-preview`

List inventory entries that are below their minimum stock quantity and would be added to the cart by a replenish action. Read-only.

**Min role:** V

**Response `200`** — list of:
```json
{
  "inventory_entry_id": "uuid",
  "part_id": "uuid",
  "part_mpn": "RC0402FR-0710KL",
  "quantity_on_hand": 30,
  "minimum_stock_quantity": 100,
  "shortfall": 70,
  "reorder_quantity": 500,
  "quantity_to_add_to_cart": 500,
  "preferred_listing_id": "uuid"
}
```
`quantity_to_add_to_cart` is `reorder_quantity` if set, otherwise `shortfall`.

---

## 9. Storage Locations

### `GET /api/v1/teams/{teamId}/storage-locations`

List all storage locations for the team.

**Min role:** V

**Response `200`** — list of:
```json
{ "id": "uuid", "label": "A3", "description": "Top drawer, left shelf" }
```

---

### `POST /api/v1/teams/{teamId}/storage-locations`

Create a storage location.

**Min role:** M

**Request body**
```json
{ "label": "A3", "description": "Top drawer, left shelf" }
```

**Response `201`**

**Errors:** `409` — label already exists for this team.

---

### `PATCH /api/v1/teams/{teamId}/storage-locations/{locationId}`

Update a storage location's label or description.

**Min role:** M

**Response `200`** — updated location object.

---

### `DELETE /api/v1/teams/{teamId}/storage-locations/{locationId}`

Delete a storage location.

**Min role:** M

**Response `204`**

**Errors:** `422` — one or more inventory entries reference this location.

---

## 10. Projects & BOMs

### `GET /api/v1/teams/{teamId}/projects`

List projects.

**Min role:** V

**Query parameters:** `status` (`active` / `archived` / `completed`), `limit`, `offset`.

**Response `200`** — paginated list of:
```json
{
  "id": "uuid",
  "name": "Motor Controller Rev C",
  "description": "...",
  "status": "active",
  "active_bom_revision_id": "uuid",
  "active_bom_revision_label": "Rev C",
  "created_by_display_name": "Alice",
  "created_at": "..."
}
```

---

### `POST /api/v1/teams/{teamId}/projects`

Create a project.

**Min role:** M

**Request body**
```json
{ "name": "Motor Controller Rev C", "description": "..." }
```

**Response `201`** — created project object.

---

### `GET /api/v1/teams/{teamId}/projects/{projectId}`

Project detail, including a summary of the active BOM revision.

**Min role:** V

**Response `200`**
```json
{
  "id": "uuid",
  "name": "Motor Controller Rev C",
  "description": "...",
  "status": "active",
  "active_bom_revision_id": "uuid",
  "active_bom_revision_label": "Rev C",
  "bom_import_config": {
    "reference_designators_column": "Designator",
    "quantity_column": "Qty",
    "description_column": "Comment",
    "skip_rows": 0
  },
  "created_by_display_name": "Alice",
  "created_at": "..."
}
```
`bom_import_config` is `null` until the first BOM import that includes a config.

---

### `PATCH /api/v1/teams/{teamId}/projects/{projectId}`

Update project fields. All fields optional.

**Min role:** M

**Request body**
```json
{
  "name": "...",
  "description": "...",
  "status": "archived",
  "active_bom_revision_id": "uuid",
  "bom_import_config": { "reference_designators_column": "Designator", "quantity_column": "Qty" }
}
```
Pass `null` for `bom_import_config` to clear the saved configuration.

**Response `200`** — updated project object.

---

### `GET /api/v1/teams/{teamId}/projects/{projectId}/revisions`

List BOM revisions for a project, newest first.

**Min role:** V

**Response `200`** — list of:
```json
{
  "id": "uuid",
  "label": "Rev C",
  "imported_at": "...",
  "notes": "Added motor driver",
  "imported_by_display_name": "Alice",
  "item_count": 47,
  "resolved_count": 45,
  "unresolved_count": 2,
  "is_active": true
}
```

---

### `POST /api/v1/teams/{teamId}/projects/{projectId}/revisions`

Import a new BOM revision. The CSV is parsed and column-mapped entirely in the browser; the frontend submits pre-parsed JSON.

**Min role:** M

**Request body**
```json
{
  "label": "Rev C",
  "notes": "Added motor driver",
  "set_active": true,
  "bom_import_config": {
    "reference_designators_column": "Designator",
    "quantity_column": "Qty",
    "description_column": "Comment",
    "skip_rows": 0
  },
  "items": [
    {
      "reference_designators": "R1, R3, R7",
      "quantity": 3,
      "raw_description": "10k 0402 1%"
    },
    {
      "reference_designators": "C1",
      "quantity": 1,
      "raw_description": "100nF 0402 10V X5R"
    }
  ]
}
```

`raw_description` is the verbatim text from the source BOM, kept for traceability. The backend attempts MPN/SKU resolution and triggers distributor API lookups for unresolved items.

When `bom_import_config` is present, the server saves it to `projects.bom_import_config` atomically with creating the revision. The frontend should include it on every import so the config stays up to date; it is used to pre-populate the column mapping UI on the next import.

**Response `202`**
```json
{ "job_id": "uuid", "bom_revision_id": "uuid" }
```

The `bom_revision_id` is created synchronously; line item resolution continues in the background. Progress streams on the SSE endpoint as `bom_import_progress` events.

---

### `PATCH /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}`

Update a BOM revision's label or notes.

**Min role:** M

**Request body**
```json
{ "label": "Rev C", "notes": "Updated notes" }
```

**Response `200`** — updated revision object.

---

### `GET /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}`

Revision detail with all line items and their resolution status.

**Min role:** V

**Response `200`**
```json
{
  "id": "uuid",
  "label": "Rev C",
  "imported_at": "...",
  "notes": "...",
  "imported_by_display_name": "Alice",
  "is_active": true,
  "items": [
    {
      "id": "uuid",
      "reference_designators": "R1, R3, R7",
      "quantity": 3,
      "raw_description": "10k 0402 1%",
      "resolved_part_id": "uuid",
      "resolved_part_mpn": "RC0402FR-0710KL",
      "substitutes": [
        { "part_id": "uuid", "part_mpn": "CRCW040210K0FKED" }
      ]
    }
  ]
}
```
`resolved_part_id` is `null` for unresolved line items.

---

### `PATCH /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/items/{itemId}`

Manually resolve or re-resolve a BOM line item.

**Min role:** M

**Request body**
```json
{ "resolved_part_id": "uuid" }
```
Pass `null` to clear the resolution.

**Response `200`** — updated line item object.

---

### `POST /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes`

Add an approved substitute part for a line item.

**Min role:** M

**Request body**
```json
{ "part_id": "uuid" }
```

**Response `201`**

**Errors:** `409` — already an approved substitute.

---

### `DELETE /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes/{partId}`

Remove an approved substitute.

**Min role:** M

**Response `204`**

---

## 11. Stock Check

### `GET /api/v1/teams/{teamId}/projects/{projectId}/stock-check`

Run a stock check against the project's active BOM revision. The `build_qty` parameter is ephemeral — it is not stored.

**Min role:** V

**Query parameters**

| Param | Required | Description |
|---|---|---|
| `build_qty` | Yes | Number of boards to check stock for. |

**Response `200`**
```json
{
  "project_id": "uuid",
  "bom_revision_id": "uuid",
  "build_qty": 10,
  "summary": {
    "in_stock": 40,
    "partial": 4,
    "out_of_stock": 3,
    "unresolved": 0
  },
  "items": [
    {
      "bom_line_item_id": "uuid",
      "reference_designators": "R1, R3, R7",
      "resolved_part_id": "uuid",
      "resolved_part_mpn": "RC0402FR-0710KL",
      "required_qty": 30,
      "available_qty": 22,
      "shortfall": 8,
      "stock_status": "partial",
      "substitute_part_ids_counted": ["uuid"]
    }
  ]
}
```

`stock_status` is one of `in_stock`, `partial`, `out_of_stock`, or `unresolved`.

---

## 12. Shopping Cart

### `GET /api/v1/teams/{teamId}/cart`

Get the team's cart with all line items and per-distributor cost totals.

**Min role:** P

**Response `200`**
```json
{
  "items": [
    {
      "id": "uuid",
      "part_id": "uuid",
      "part_mpn": "RC0402FR-0710KL",
      "quantity": 500,
      "listing_id": "uuid",
      "listing_sku": "311-10KARCT-ND",
      "listing_distributor_name": "DigiKey",
      "listing_unit_price": "0.0120",
      "source": "bom_shortfall",
      "source_bom_line_item_id": null
    }
  ],
  "pending_builds": [
    {
      "bom_revision_id": "uuid",
      "revision_label": "v1.2",
      "project_name": "Main Board",
      "build_quantity": 10
    }
  ]
}
```

`pending_builds` lists the BOM revisions currently driving `bom_shortfall` cart items. `source_bom_line_item_id` is `null` when the item's shortfall is aggregated across multiple pending builds.

---

### `DELETE /api/v1/teams/{teamId}/cart`

Clear all items from the cart. Also clears all pending builds.

**Min role:** P

**Response `204`**

---

### `POST /api/v1/teams/{teamId}/cart/items`

Add an item to the cart. If a cart item for `part_id` already exists, the quantities are merged.

**Min role:** P

**Request body**
```json
{ "part_id": "uuid", "quantity": 100, "listing_id": "uuid" }
```
`listing_id` is optional.

**Response `201`** — created or updated cart item object.

---

### `PATCH /api/v1/teams/{teamId}/cart/items/{itemId}`

Update a cart item's quantity or assigned listing.

**Min role:** P

**Request body** (all fields optional)
```json
{ "quantity": 200, "listing_id": "uuid" }
```
Pass `null` for `listing_id` to unassign.

**Response `200`** — updated cart item object.

---

### `DELETE /api/v1/teams/{teamId}/cart/items/{itemId}`

Remove an item from the cart.

**Min role:** P

**Response `204`**

---

### `POST /api/v1/teams/{teamId}/cart/add-shortfall`

Register a BOM revision as a pending build and recompute all `bom_shortfall` cart items.
Demand is aggregated across every pending build before subtracting on-hand inventory,
so overlapping parts are never double-counted. Calling this endpoint again with the same
`bom_revision_id` replaces its `build_quantity` and recomputes. `manual` and
`inventory_maintenance` cart items are not affected.

**Min role:** P

**Request body**
```json
{ "bom_revision_id": "uuid", "build_quantity": 10 }
```

**Response `200`**
```json
{ "added_count": 3 }
```

---

### `DELETE /api/v1/teams/{teamId}/cart/pending-builds/{revisionId}`

Remove a BOM revision from the pending-builds list and recompute all `bom_shortfall` cart items.
`manual` and `inventory_maintenance` items are not affected.

**Min role:** P

**Response `200`**
```json
{ "added_count": 2 }
```

---

### `POST /api/v1/teams/{teamId}/cart/add-low-stock`

Add all inventory entries below their minimum stock quantity to the cart. Merges existing cart items.

**Min role:** P

**Response `200`** — updated cart.

---

### `POST /api/v1/teams/{teamId}/cart/convert`

Convert the cart to draft orders. Items are grouped by distributor; one `Draft` order is created per distributor. The cart items and all pending builds are cleared on success.

**Min role:** P

All items must have an assigned `listing_id`. Returns `422` if any item is unassigned.

**Response `200`**
```json
{
  "orders_created": [
    {
      "order_id": "uuid",
      "distributor_id": "uuid",
      "distributor_name": "DigiKey",
      "item_count": 5,
      "estimated_total": "47.2500"
    }
  ],
  "items_converted": 5,
  "items_remaining_in_cart": 2
}
```

---

## 13. Orders

### `GET /api/v1/teams/{teamId}/orders`

List orders.

**Min role:** V

**Query parameters:** `status`, `distributor_id`, `limit`, `offset`.

**Response `200`** — paginated list of:
```json
{
  "id": "uuid",
  "distributor_id": "uuid",
  "distributor_name": "DigiKey",
  "status": "placed",
  "external_reference": "DK-1234567",
  "created_at": "...",
  "item_count": 5
}
```

---

### `GET /api/v1/teams/{teamId}/orders/{orderId}`

Order detail with all line items.

**Min role:** V

**Response `200`**
```json
{
  "id": "uuid",
  "distributor_id": "uuid",
  "distributor_name": "DigiKey",
  "status": "placed",
  "submission_method": "manual",
  "external_reference": "DK-1234567",
  "external_ref_linked_at": "...",
  "received_at": null,
  "notes": "...",
  "created_by_display_name": "Alice",
  "created_at": "...",
  "items": [
    {
      "id": "uuid",
      "listing_id": "uuid",
      "part_mpn": "RC0402FR-0710KL",
      "sku": "311-10KARCT-ND",
      "packaging_type": "Cut Tape",
      "quantity_ordered": 500,
      "quantity_received": 0,
      "unit_price_snapshot": "0.0120",
      "fixed_order_cost_snapshot": "0.0000",
      "line_total": "6.0000"
    }
  ],
  "order_total": "47.2500"
}
```

---

### `PATCH /api/v1/teams/{teamId}/orders/{orderId}`

Update order notes. Line item quantities may also be adjusted, but only while status is `draft`.

**Min role:** P

**Request body**
```json
{
  "notes": "Updated notes",
  "items": [
    { "id": "uuid", "quantity_ordered": 1000 }
  ]
}
```

**Response `200`** — updated order object.

**Errors:** `422` — attempt to adjust items on a non-draft order.

---

### `DELETE /api/v1/teams/{teamId}/orders/{orderId}`

Cancel an order. Allowed for any status except `received`. Transitions to `cancelled`.

**Min role:** P

**Response `204`**

---

### `GET /api/v1/teams/{teamId}/orders/{orderId}/export`

Generate a CSV file for manual order placement. Transitions the order to `exported` status. Returns the CSV as an attachment.

**Min role:** P

**Response `200`**
- `Content-Type: text/csv`
- `Content-Disposition: attachment; filename="order-{orderId}.csv"`

**Errors:** `422` — order is not in `draft` or `exported` status.

---

### `POST /api/v1/teams/{teamId}/orders/{orderId}/submit`

Submit the order via the distributor's API. Transitions to `submitted`, or directly to `placed` if the distributor returns a reference number immediately.

**Min role:** P

**Response `200`** — updated order object (status `submitted` or `placed`).

**Errors:** `422` — distributor has no API integration, or integration is not enabled for this team. `502` — distributor API call failed.

---

### `POST /api/v1/teams/{teamId}/orders/{orderId}/reference`

Link an external distributor order reference. Transitions the order to `placed`, enabling API-based status tracking if the distributor supports it.

**Min role:** P

**Request body**
```json
{ "external_reference": "DK-1234567" }
```

**Response `200`** — updated order object.

**Errors:** `422` — order is not in `draft`, `exported`, or `submitted` status.

---

### `POST /api/v1/teams/{teamId}/orders/{orderId}/items/{itemId}/receive`

Receive a full or partial quantity of an order line item. Creates a stock adjustment for the corresponding inventory entry. Automatically transitions the order to `partially_received` or `received`.

**Min role:** P

**Request body**
```json
{ "quantity_received": 250 }
```

`quantity_received` is the additional quantity received in this transaction (not a cumulative total).

**Response `200`**
```json
{
  "order_line_item": { ... },
  "order_status": "partially_received",
  "stock_adjustment_id": "uuid",
  "new_quantity_on_hand": 400
}
```

**Errors:** `422` — would exceed `quantity_ordered`.

---

## 14. Builds

### `GET /api/v1/teams/{teamId}/builds`

List builds.

**Min role:** V

**Query parameters:** `project_id`, `limit`, `offset`.

**Response `200`** — paginated list of:
```json
{
  "id": "uuid",
  "project_id": "uuid",
  "project_name": "Motor Controller Rev C",
  "bom_revision_id": "uuid",
  "bom_revision_label": "Rev C",
  "boards_built": 10,
  "built_at": "...",
  "notes": "...",
  "recorded_by_display_name": "Alice"
}
```

---

### `POST /api/v1/teams/{teamId}/builds/preview`

Preview the stock deductions a build would make, without committing any changes.

**Min role:** M

**Request body**
```json
{
  "project_id": "uuid",
  "bom_revision_id": "uuid",
  "boards_built": 10,
  "substitute_selections": {
    "bom_line_item_id_1": "part_id_A"
  }
}
```

`substitute_selections` maps each line item ID that has approved substitutes to the chosen part ID to deduct stock from. Required for any line item that has substitutes; omit for line items with no substitutes.

**Response `200`**
```json
{
  "deductions": [
    {
      "bom_line_item_id": "uuid",
      "reference_designators": "U1",
      "part_id": "uuid",
      "part_mpn": "STM32G031J6M6",
      "inventory_entry_id": "uuid",
      "delta": -10,
      "quantity_on_hand_after": 15
    }
  ],
  "warnings": []
}
```

`warnings` contains messages for line items with insufficient stock.

---

### `POST /api/v1/teams/{teamId}/builds`

Record a build and commit all stock deductions. Same request body as `builds/preview`.

**Min role:** M

**Response `201`**
```json
{
  "build": { ... },
  "deductions_made": 12,
  "stock_adjustments_created": ["uuid", "uuid", ...]
}
```

**Errors:** `422` — insufficient stock for one or more line items (if strict deduction is enforced; behaviour TBD).

---

### `GET /api/v1/teams/{teamId}/builds/{buildId}`

Build detail.

**Min role:** V

**Response `200`** — full build object with deductions list.

---

## 15. General Checkouts

### `GET /api/v1/teams/{teamId}/checkouts`

List general checkouts.

**Min role:** V

**Query parameters:** `limit`, `offset`.

**Response `200`** — paginated list of:
```json
{
  "id": "uuid",
  "performed_at": "...",
  "reason": "Prototype repair",
  "performed_by_display_name": "Bob",
  "item_count": 3
}
```

---

### `POST /api/v1/teams/{teamId}/checkouts`

Record a general checkout. Deducts stock for each item and creates stock adjustments.

**Min role:** M

**Request body**
```json
{
  "reason": "Prototype repair",
  "items": [
    { "part_id": "uuid", "quantity": 2 },
    { "part_id": "uuid", "quantity": 5 }
  ]
}
```

`reason` is optional.

**Response `201`**
```json
{
  "checkout": { ... },
  "stock_adjustments_created": ["uuid", "uuid"]
}
```

---

### `GET /api/v1/teams/{teamId}/checkouts/{checkoutId}`

Checkout detail with line items and linked stock adjustments.

**Min role:** V

**Response `200`**
```json
{
  "id": "uuid",
  "team_id": "uuid",
  "performed_at": "...",
  "reason": "Prototype repair",
  "performed_by_display_name": "Bob",
  "items": [
    {
      "part_id": "uuid",
      "part_mpn": "STM32G031J6M6",
      "quantity": 2,
      "stock_adjustment_id": "uuid"
    }
  ]
}
```

---

## 16. Server-Sent Events

### `GET /api/v1/teams/{teamId}/events`

SSE stream for the team. The client should connect once on login and keep the connection alive. Standard `EventSource` browser API is used.

**Min role:** V

**Response:** `text/event-stream`

### Event types

#### `bom_import_progress`
Emitted during BOM revision import while the system resolves line items against the parts catalog and distributor APIs.

```json
{
  "job_id": "uuid",
  "bom_revision_id": "uuid",
  "status": "running",
  "total_items": 47,
  "resolved": 38,
  "unresolved": 3,
  "pending": 6,
  "error": null
}
```
Final event: `status` becomes `done`. On failure: `status` is `failed`, `error` carries a message.

#### `part_enrichment_progress`
Emitted during part re-enrichment.

```json
{
  "job_id": "uuid",
  "part_id": "uuid",
  "status": "done",
  "error": null
}
```

#### `listing_refresh_done`
Emitted when a distributor listing price/stock refresh completes.

```json
{
  "job_id": "uuid",
  "listing_id": "uuid",
  "status": "done",
  "unit_price": "0.0120",
  "stock_available": 45000,
  "error": null
}
```

#### `order_status_update`
Emitted when background order status polling detects a change.

```json
{
  "order_id": "uuid",
  "previous_status": "placed",
  "new_status": "partially_received"
}
```
