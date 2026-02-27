# BOM Import — Resolution & Review Design

This document describes the end-to-end BOM import flow in partrack, with a focus on
how BOM **line items are resolved to Parts** and how the user reviews and fixes
unclear mappings.

It is a **planning document** only. It defines behaviour, states, and
responsibilities for the frontend, backend handlers, background worker, and data
model, but does not prescribe concrete code.

Relevant background:

- [architecture.md](architecture.md) — overall system architecture
- [datamodel.md](datamodel.md) — conceptual entities (Projects, BOM Revisions,
  BOM Line Items, Parts, Distributor Listings, Inventory Entries, …)
- [userstory.md](userstory.md) — **Epic 1: BOM Import** and US-1.1/US-1.3
- `internal/handler/revisions.go` — `ImportRevision` handler
- `internal/worker/queue.go` — `JobBOMImport` / `handleBOMImport`
- `web/src/lib/components/bom/BOMImportDialog.svelte` — frontend CSV import UI

---

## 1. Goals & Non‑Goals

### 1.1 Goals

- Allow users to import BOMs from their EDA tools with **minimal friction**.
- Make **clear-cut matches fully automatic**:
  - Valid distributor + SKU with known listing → auto-resolve to Part.
  - Clear MPN match to an existing Part → auto-resolve.
  - When possible, import new Parts + Distributor Listings from APIs
    transparently.
- Surface **ambiguous or missing mappings** in a dedicated review UI where the
  user can:
  - Ignore virtual/non-procurable components.
  - Link to an existing Part.
  - Import a new Part via distributor APIs (SKU or MPN).
- Ensure that a BOM revision is marked **ready for stock checking** only when
  every line item is either:
  - Resolved to a primary Part, or
  - Explicitly marked as *ignored*.

### 1.2 Non‑Goals

- Fuzzy, parameter-based matching ("find me any 10k resistor") during the
  initial import job. That belongs in substitution tooling and manual review.
- Hard real-time resolution. Import runs as a background job and reports
  progress via SSE; users can leave and return.
- Handling multi-quantity / parametric optimization for order generation.
  BOM import only concerns *which Part* each line item refers to.

---

## 2. High‑Level Flow

The BOM import flow consists of three stages:

1. **Frontend parsing & mapping** (current)
2. **Backend analysis & auto‑resolution** (current, to be enhanced)
3. **Revision review & manual resolution** (new)

### 2.1 Stage 1 — Frontend CSV Parsing & Mapping

Responsible component:
- `web/src/lib/components/bom/BOMImportDialog.svelte`

Behaviour:

1. User selects a CSV file for a specific Project.
2. Frontend uses PapaParse to parse rows, with user-configurable options:
   - delimiter, quote/escape char, comments, header row, skip first N lines.
3. User maps CSV columns to roles (`ColumnRole`):

   ```ts
   type ColumnRole =
     | 'ignore'
     | 'designators'
     | 'quantity'
     | 'mpn'
     | 'description'
     | 'manufacturer'
     | 'name'
     | 'sku'
     | 'distributor';
   ```

4. If any `distributor` column is present, the dialog performs a **distributor
   mapping** step:
   - Extract unique textual distributor names from the BOM.
   - For each, let the user map it to a known `distributors.id` or choose
     "None" (do not use for resolution).
5. When the user confirms, the frontend POSTs to
   `POST /teams/{teamId}/projects/{projectId}/revisions` (`ImportRevision`)
   with:
   - `label` (revision label)
   - `set_active: true|false`
   - `bom_import_config` (all parse & column mapping options, plus
     distributor mappings) to be stored on the Project
   - `items[]` — the BOM Line Item imports built from the mapped columns.

See §3 for the exact item payload and how it maps into DB fields.

### 2.2 Stage 2 — Backend Import & Auto‑Resolution

Responsible components:

- `internal/handler/revisions.go` — `ImportRevision`
- `internal/worker/queue.go` — `JobBOMImport` / `handleBOMImport`
- `db/migrations/000002_bom_line_item_extra_fields.up.sql`
- `db/migrations/000003_bom_line_item_resolved_distributor.up.sql`

Behaviour (current, conceptualised):

1. `ImportRevision`:
   - Creates a `bom_revisions` row.
   - Inserts one `bom_line_items` row per frontend `item`.
   - Optionally sets the Project's active revision and persists
     `bom_import_config` JSON from the frontend.
   - Enqueues `JobBOMImport` with `{ bom_revision_id, project_id }`.
2. `JobBOMImport` loads all BOM line items for the revision and tries to
   resolve each to a Part in the background.
3. Progress is reported via SSE events of type `bom_import_progress` with
   `{resolved, total, status}`.

### 2.3 Stage 3 — Revision Review (New)

Responsible components (planned):

- Frontend: `RevisionDetail` page for a BOM revision
  (`web/src/routes/team/RevisionDetail.svelte` + BOM line item components).
- Backend: new endpoints on `revisions.go` / `items.go` for manual resolution
  actions (link/ignore/create).

Behaviour (desired):

1. Once `JobBOMImport` completes, the user opens the revision detail view.
2. The UI shows each BOM line item with a **resolution state** (see §5.1):
   - Auto-resolved (by SKU or MPN)
   - Ambiguous match
   - Unresolved
   - Ignored
3. The user works through unresolved/ambiguous items using a dedicated
   **review mode** until all items are either resolved or ignored.
4. Only then is the revision treated as **ready** for stock checking (see
   US-1.3 acceptance criteria).

---

## 3. Input & Persistence Model

### 3.1 Frontend → Backend — Import Payload

The frontend currently builds `items[]` in `BOMImportDialog.svelte` via
`buildItems()` and sends objects of the form:

```jsonc
{
  "reference_designators": "R1,R2",
  "quantity": 2,
  "raw_description": "10k 1% 0402",
  "mpn": "RC0402FR-0710KL",
  "manufacturer": "Yageo",
  "name": "RES SMD 10K OHM 1% 1/16W 0402",
  "sku": "311-10KLRTR-ND",
  "distributor_name": "Digi-Key",
  "resolved_distributor_id": "<UUID>|null"
}
```

Key points:

- `reference_designators` and `quantity` are **required**.
- All other fields are optional and come from mapped columns if available.
- `resolved_distributor_id` is set only when the user mapped the textual
  `distributor_name` to a known Distributor in the *distributor mapping* step.

### 3.2 Backend Persistence — `bom_line_items`

Relevant columns added for BOM import:

- `manufacturer TEXT`
- `mpn TEXT`
- `part_name TEXT`
- `sku TEXT`
- `distributor_name TEXT`
- `resolved_distributor_id UUID REFERENCES distributors(id)`

The import handler maps fields as follows:

| JSON field               | DB column              |
|--------------------------|------------------------|
| `reference_designators`  | `reference_designators`|
| `quantity`               | `quantity`             |
| `raw_description`        | `raw_description`      |
| `manufacturer`           | `manufacturer`         |
| `mpn`                    | `mpn`                  |
| `name`                   | `part_name`            |
| `sku`                    | `sku`                  |
| `distributor_name`       | `distributor_name`     |
| `resolved_distributor_id`| `resolved_distributor_id`|

Additionally, each line item can eventually reference:

- `resolved_part_id` — the primary Part chosen for this line item.

> Note: MPNs are **not globally unique**; they are unique only within a given
> manufacturer. Our matching logic therefore needs to consider `manufacturer`
> + `mpn` together when possible (see §4.2).

---

## 4. Auto‑Resolution Strategy (Backend Job)

The auto-resolution logic runs inside `JobBOMImport`. It should be deterministic
and conservative: only make decisions automatically when they are clear-cut,
and leave ambiguous cases for user review.

Terminology:

- **Line item** — a row in `bom_line_items`.
- **Explicit distributor mapping** — the combination of `sku` and
  `resolved_distributor_id` present on the line item.

### 4.1 Pre‑Filtering

For each line item in the revision:

1. If `resolved_part_id` is already set, the job **skips** the item (e.g. it
   was manually resolved in a previous import run).
2. Items with `quantity <= 0` are kept but never auto-resolved. They will
   appear in review and can be ignored.

### 4.2 Step 1 — Resolution by Distributor SKU

This is the most reliable signal: a concrete SKU from a known distributor.

Conditions:

- `sku` is non-empty, **and**
- `resolved_distributor_id` is non-null.

Resolution process:

1. **Local listing lookup**
   - Query `distributor_listings` by `sku` + `distributor_id` (case-insensitive
     SKU comparison).
   - If exactly one listing is found, resolve the BOM line to
     `listing.part_id` and mark the auto-resolution reason as *by SKU*.
   - Ensure an `inventory_entries` row exists for `(team, part)` (create with
     quantity 0 if missing).
2. **Listing import via Distributor API** (when no local listing exists)
   - If the team has an enabled `distributor_configs` row for this distributor
     and the distributor has an API client:
     - Use the distributor-specific client to look up the SKU.
       - For DigiKey, this would be a `RefreshListing`/pricing lookup with SKU.
     - If the API returns a product:
       - Create or update the corresponding `parts` row.
       - Create/update the `distributor_listings` row(s) for this SKU.
       - Resolve the BOM line to the Part and ensure an Inventory Entry exists.
   - If the SKU is **not found** via the API or credentials are missing:
     - Do **not** auto-resolve; leave the item for review.

Notes:

- This path **never** auto-resolves to multiple parts. Ambiguity is not
  expected for `(distributor, sku)`; if multiple listings somehow exist, the
  line is treated as ambiguous and left unresolved.
- If the BOM provided `manufacturer`/`mpn` as well, they are stored on the
  line item but are not required for SKU-based resolution.

### 4.3 Step 2 — Resolution by MPN (with optional Manufacturer)

If Step 1 did not yield a result, the job uses MPN-based matching.

Conditions:

- `mpn` is non-empty.

Resolution tiers:

1. **Exact MPN + Manufacturer match**
   - If `manufacturer` is present on the line item, search `parts` for
     candidates where:
       - `parts.mpn = line.mpn`, and
       - `parts.manufacturer` matches `line.manufacturer` (case-insensitive
         equality, stripping minor punctuation differences where reasonable).
   - If **exactly one** such Part exists → auto-resolve.
   - If more than one candidate exists → mark as **ambiguous** for review.
2. **Exact MPN match (ignore manufacturer)**
   - If step 1 did not resolve, search for `parts.mpn = line.mpn` regardless of
     manufacturer.
   - If exactly one Part exists → auto-resolve and record the reason as
     *MPN-only*. The mismatch between BOM `manufacturer` and Part
     `manufacturer` (if any) should be surfaced in the review UI as a warning.
   - If multiple Parts share this MPN → **ambiguous**; user must choose.
3. **No local MPN match — import from distributor APIs (optional)**
   - If the team has one or more enabled distributor configs:
     - For each enabled distributor, in deterministic order:
       - Call `EnrichPart(mpn)` on the distributor client.
       - If a Part is found/created, stop at the first success and use that
         Part.
       - Persist the Part and its Distributor Listings in the catalog.
   - If a Part was imported:
     - Resolve the BOM line to the new Part.
     - Create an Inventory Entry for `(team, part)`.
   - If no distributor returns a match, the line item remains unresolved.

Notes:

- Distributor MPN lookups may be ambiguous (multiple manufacturers with the
  same MPN). The initial implementation can rely on the distributor client to
  pick a best guess, but the BOM review UI should make it clear which
  manufacturer was chosen.
- To avoid duplicates, the enrichment path **must** check for an existing Part
  with the same `(manufacturer, mpn)` before inserting a new one.

### 4.4 Step 3 — Fallbacks & Virtual Components

If a line item is still unresolved after SKU and MPN resolution:

- The job does **not** attempt fuzzy matching against local Parts.
- The line item proceeds to the review stage with status **unresolved**.

Typical unresolved cases:

- Virtual / non-procurable components:
  - Test points, mounting holes, mechanical outlines, logos.
  - Design-specific things like "DO NOT POPULATE", jumpers, configuration
    links.
- BOM lines without MPN/SKU (e.g. only a human-readable description).

Heuristics such as auto-classifying common virtual components (e.g. based on
keywords in `raw_description` like "TP", "TESTPOINT", "FIDUCIAL") can be added
later, but the final decision remains with the user via the review UI.

### 4.5 Inventory Entry Creation

Whenever a BOM line item becomes resolved (automatically or later via manual
actions), the backend should ensure that an `inventory_entries` row exists for
the `(team, part)` pair (see [datamodel.md](datamodel.md)):

- If `GetInventoryEntryByPartID(team, part)` returns `no rows`, create a new
  entry with quantity 0.
- This enables users to immediately configure storage location, preferred
  listing, min stock, and reorder quantity for new parts discovered via BOM
  import.

---

## 5. Review Stage & Manual Resolution

The review stage is where the user resolves ambiguous or unresolved BOM line
items and confirms or corrects automatic assignments.

### 5.1 Line Item Resolution State

Each `bom_line_items` row carries a `resolution_state TEXT` column and an
`ignored BOOLEAN` flag (both added in migration `000004`). The five states form
a four-tier priority ladder:

| Tier | `resolution_state` | `resolved_part_id` | Meaning |
|------|--------------------|--------------------|---------|
| 0 | `auto_sku` | set | Worker resolved the item via an exact `(distributor, sku)` lookup against `distributor_listings`. Highest confidence — no user action needed. |
| 1 | `auto_mpn` | set | Worker found **exactly one** `parts` row matching the item's `mpn` (within global + team scope). Single-candidate sign-off — no user action needed. |
| 2 | `ambiguous` | **null** | Worker found **more than one** Part matching the MPN. `resolved_part_id` is intentionally null until the user picks a candidate in the review UI. |
| 3 | `null` | **null** | No match found yet. Item needs user attention: ignore it, link it to an existing Part, or import from a distributor. |
| — | `manual` | set | User explicitly linked the item to a Part in the review UI. |
| — | `ignored` | **null** | `ignored = TRUE`; item is excluded from stock checks and shortfall calculations. Can be toggled back by the user. |

Notes:

- A `null` `resolution_state` with `ignored = FALSE` means the item is
  **unresolved** (Tier 3).
- `ambiguous` items remain unresolved (`resolved_part_id = null`) until the user
  selects one candidate; at that point `resolution_state` is updated to `manual`.
- An item is “ready” when `resolved_part_id IS NOT NULL OR ignored = TRUE`.

### 5.2 Review UI — General Behaviour

The revision detail view should support:

- **Filtering & grouping**
  - Tabs or filters for: All, Unresolved, Ambiguous, Auto-resolved,
    Ignored.
  - Optional grouping by category, manufacturer, or whether the Part exists
    in inventory.
- **Per-line item summary**
  - Reference designators & quantity.
  - Raw description / name.
  - Manufacturer + MPN (from BOM).
  - Current resolution (Part name + MPN, if resolved) and reason.
  - Quick actions: *Ignore*, *Change Part*, *Resolve*.

### 5.3 Actions on a BOM Line Item

For any non-ignored line item, the UI should offer these actions.

#### 5.3.1 Ignore Component

Use when:

- The line item represents a virtual component or something that should not be
  procured or tracked.

Behaviour:

- Mark the line as **ignored**.
- Do not require or attempt `resolved_part_id`.
- Exclude the item from stock checks and shortfall → cart flows.
- Display ignored items distinctly (e.g. greyed out) so users can revisit.

#### 5.3.2 Link to Existing Part

Use when:

- The Part already exists in the catalog (global or team-scoped) but was not
  auto-detected, or when the user wants to correct an auto-resolution.

UX flow:

1. The user opens a **"Select Part" modal** for the line item.
2. The modal offers search across the **local Part catalog** and may also
   surface **distributor-backed search suggestions**:
   - Local search by:
     - MPN
     - Manufacturer
     - Name / description
     - Category, package, etc. (as available)
   - Optional remote search using the distributor search API (see
     `internal/distributor.Client.SearchParts` and the DigiKey
     `/products/v4/search/keyword` endpoint) to propose candidate parts from
     distributor catalogs alongside local results.
3. The search box should be pre-populated with a reasonable default:
   - Prefer `mpn` if present.
   - Else prefer `name` or `raw_description`.
4. When the user selects a Part:
   - Set `resolved_part_id` on the line item.
   - If no Inventory Entry exists for `(team, part)`, create it with quantity 0.

#### 5.3.3 Import / Create Part via Distributor

Use when:

- The BOM provides a valid distributor + SKU, or at least an MPN, but no
  matching local Part exists yet. No this is not right. The review should run after the automatic linking job has gone through all parts, and thus we are only reviewing things which need eyeballs on them. If the user supplied a valid distributor + SKU, there are no questions left to answer here.

UX flow:

- **If SKU + distributor are available** (either `resolved_distributor_id` or a
  mapping selected in the modal):
  1. Offer an action like “Import from &lt;Distributor&gt; by SKU”.
  2. Backend uses the distributor client to look up the SKU and either
     reuses an existing Part or creates a new one.
  3. On success, resolve the BOM line to the resulting Part and create an
     Inventory Entry if necessary.

  
- **If only MPN (and possibly manufacturer) is available:**
  1. Offer actions like **“Search distributors by MPN”** and **“Search
     distributors by keyword”**.
  2. For discovery, backend calls the distributor search API with a
     `SearchQuery` built from the line item:
     - When manufacturer is known, use `"{manufacturer} {mpn}"` as
       `Keywords`.
     - Otherwise, use the raw `mpn`, `name`, or `raw_description` as
       free-text keywords (e.g. `"10k 1% 0402 resistor"`).
     - For DigiKey, this maps to `POST /products/v4/search/keyword` (see
       `doc/distributor/distributor-digikey.md`).
  3. Present the returned `SearchResult.Products` to the user as distributor
     candidates (with manufacturer, MPN, brief description, package, stock,
     and price preview when available).
  4. When the user selects a distributor product:
     - Perform a follow-up enrichment call (e.g. DigiKey `productdetails`
       via `EnrichPart`) to create/merge the canonical Part and associated
       Listings.
     - Resolve the BOM line to that Part and ensure an Inventory Entry
       exists.
  5. If multiple conflicting candidates remain (e.g. several manufacturers
     share the same MPN), the modal behaves like a choose-from-candidates UI;
     no automatic choice is made without explicit user confirmation.

These flows align BOM review with the existing **Part Enrichment** behaviour
documented in `distributor/distributor-*.md`.

### 5.4 Completion Criteria for a BOM Revision

A BOM revision is considered **ready for stock checking** when, for every
`bom_line_items` row:

- Either `resolved_part_id` is non-null, or
- The line is explicitly marked as **ignored**.

The UI should surface this as a status badge (e.g. “Ready” / “Needs
resolution”) and potentially block stock-check actions on revisions that are
still in “needs resolution” state, or at least warn the user.

---

## 6. Integration with Stock Checking & Downstream Flows

Once a revision is ready:

- **Stock checks**:
  - Operate only on non-ignored, resolved line items.
  - Merge line items for the same Part when computing required vs on-hand
    quantities (as per US-1.1 / US-2.1).
- **Shortfall → Cart**:
  - Uses the resolved Part IDs to add Cart Line Items.
  - If an Inventory Entry has a preferred Distributor Listing, that listing is
    used when populating the cart; otherwise cart items start without a
    listing and must be resolved in the cart UI.
- **Order creation / receipt**:
  - Function independently of how the BOM was imported; BOM import’s job is
    purely to establish accurate `BOM Line Item → Part` links.

---

## 7. SSE & Progress Reporting

`JobBOMImport` should continue to publish `bom_import_progress` SSE events as it
processes items. Over time, these events can be extended to provide richer
information for the frontend, for example:

- Initial event:

  ```jsonc
  {
    "status": "starting",
    "resolved": 0,
    "total": 42,
    "job_id": "…"
  }
  ```

- Periodic updates:

  ```jsonc
  {
    "status": "progress",
    "resolved": 17,
    "total": 42,
    "job_id": "…"
  }
  ```

- Final event (includes per-tier breakdown):

  ```jsonc
  {
    "status": "complete",
    "resolved_sku": 10,
    "resolved_mpn": 27,
    "ambiguous": 2,
    "unresolved": 3,
    "total": 42,
    "job_id": "…"
  }
  ```

The final event breakdown maps directly onto the §5.1 tier table:
`resolved_sku` (Tier 0) + `resolved_mpn` (Tier 1) are auto-resolved;
`ambiguous` (Tier 2) and `unresolved` (Tier 3) need review.

---

## 8. Edge Cases & Open Questions

This section captures intentionally unresolved details for future iterations.

1. **DNP / DNI semantics**
   - Many BOMs contain a “DNP” column. Do we want a dedicated column/flag on
     `bom_line_items` to model this, or treat it as a specific flavour of
     “ignored” set during review?
2. **Heuristics for virtual components**
   - We may later add heuristics to pre-mark common virtual components as
     “probably ignored”, but the final decision should remain in the user’s
     control.
3. **Multiple SKUs per line item**
   - Some BOMs contain multiple supplier columns (SKU1, SKU2, …). The current
     UI supports a single `sku` + `distributor`; future work might extend the
     mapping stage to allow multiple potential SKUs per line item.

---

## 9. Summary

- BOM import is split into a **frontend parsing/mapping stage**, a **backend
  auto‑resolution job**, and a **manual review stage**.
- Clear-cut cases (valid distributor SKU, unambiguous MPN) are resolved
  automatically, optionally importing new Parts and Distributor Listings via
  distributor APIs.
- All ambiguous or unresolved cases are surfaced in a revision review UI where
  the user can ignore components, link to existing Parts, or import new Parts.
- A revision is ready for stock checking only when every line item is either
  resolved or ignored, ensuring downstream stock and procurement workflows
  operate on solid, user-validated data.

