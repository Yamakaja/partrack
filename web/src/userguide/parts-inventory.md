# Parts & inventory

This chapter covers everything about parts, their parameters and distributor listings, your team's inventory profile for each part, the immutable stock ledger, and the general checkout workflow. Stock movements tied to orders or builds are documented in [orders.md](orders.md) and [builds.md](builds.md).

---

## Understanding the separation between parts and inventory

In partrack these are deliberately different things:

- A **part** is a catalog entry. It describes a component — its identity, technical attributes, and distributor purchasing options. It exists in the catalog whether or not your team has ever stocked it.
- An **inventory entry** is your team's profile for a specific part. It records where you normally keep it, how much you want to hold, and the complete history of every stock movement.
- **Quantity on hand** is never a field you write directly. It is always derived from an append-only ledger of stock adjustments.

This design means you can reference a part in a BOM long before you receive any stock, and you always have a full audit trail of how a quantity reached its current level.

---

## Parts

### Browsing and searching the Parts table

Navigate to **Parts** in the sidebar to see all parts visible to your team. The table includes both global parts (shared across the whole instance) and team-scoped parts (private to your team).

**Filtering options:**

- **Search bar** — free-text search across MPN, description, and manufacturer.
- **Category** — filter to a single category; when a category is selected, that category's characteristic parameters appear as additional columns and filter controls.
- **Lifecycle status** — multi-select: Active, NRND, Obsolete, Deprecated, or No Longer Available.
- **Stock status** — multi-select: In stock, Low stock, Out of stock, or Not tracked.
- **Storage location** — filter by the storage location (or a cell within it) assigned to the inventory entry.
- **Parameter filters** — when a category is selected, numeric parameters show a min/max range popover; enum parameters show a value picker. All active parameter filters are intersected (AND logic).

Clearing a filter chip removes it; clicking a category link on a part's detail page navigates back to the Parts table pre-filtered to that category.

**Bulk actions scoped to the current search:**

- **Refresh listings** — re-fetches current stock and price data from distributor APIs for every listing belonging to the filtered parts (max 1,000 listings per operation; narrow your search if the limit is exceeded).
- **Refresh parameters** — re-enriches every part in the filtered results from the distributor that provided its primary listing (max 1,000 parts per operation).

Both bulk actions run as background jobs and show progress. They require at least the **member** role.

### Part identity

Parts are identified by:

- **Scope** — `global` (visible to every team on the instance) or `team` (private to your team).
- **MPN** (manufacturer part number) and **manufacturer** — the primary human identity. MPN alone is not guaranteed globally unique; the combination of MPN + manufacturer is used for deduplication checks.
- **Description** and **short description** — the full description and a compact variant used in tight UI spaces.
- **Category** and **package/footprint** — category is a structured classification; package is a free-form string (e.g. `0402`, `SOT-23`). partrack internally normalises the package into a canonical form used for parametric matching.
- **Datasheet URL** and **product page URL**.
- **Aliases** — alternative MPN strings (e.g. the same part under a different brand). Shown on the part detail page; the first three are visible inline with a "show more" expansion.

### Lifecycle status

Parts are **never deleted**. When a part is end-of-life or no longer used, set its lifecycle status instead. This preserves all BOM, order, and inventory references.

| Status | Meaning |
|---|---|
| Active | Current production part; recommended for new designs |
| NRND | Not recommended for new designs; still available but approaching end of life |
| Obsolete | Manufacturer has discontinued production |
| Deprecated | Your team has decided not to use this part further |
| No Longer Available | Part cannot be sourced from any known distributor |

You can filter the Parts table by lifecycle status to find parts in each state. The lifecycle badge is shown in the part detail topbar and in the Parts table Status column.

### Part scope and who can create parts

| Action | Required role |
|---|---|
| Create a team-scoped part | member |
| Edit any part (description, category, package, datasheet, lifecycle status) | member (for team parts or parts visible to your team) |
| Create a global part | instance admin only |

When you click **Add Part**, a dialog opens that first tries to resolve the part via distributor APIs. This dialog is described in [Adding a part](#adding-a-part) below.

### Part detail page

Click any row in the Parts table to open the part detail page. It shows:

- **Overview card** — MPN, manufacturer, description, category, package, datasheet and product-page links, a part image (when one was retrieved during enrichment), and a lifecycle badge.
- **Inventory summary** — quantity on hand, quantity reserved by pending builds, quantity available (on hand minus reserved), storage location (inline editable), minimum stock level, and reorder quantity. If the part is not yet tracked, a "Start Tracking" button creates an inventory entry with zero stock.
- **Listings** — all distributor listings for this part (see [Distributor listings](#distributor-listings)).
- **Parameters** — technical attributes split into user-specified and distributor-sourced tabs (see [Parameters](#parameters)).
- **Notes** — a rich-text (WYSIWYG markdown) notes panel, editable by members and above.
- **Documents** — file attachments (images and other documents).
- **Reservations** — active soft-allocations from pending and ordered builds.
- **History** — the full stock-adjustment ledger (when tracked).

The topbar provides buttons to:
- **Edit Part** — change description, category, package, datasheet URL, and lifecycle status.
- **QR Code** — generate a scannable QR code encoding the MPN and manufacturer for use with the mobile scanner.
- **Add to Cart** — add the part to the team cart (purchaser or admin only).
- **Edit Settings** — update storage location, minimum stock, and reorder quantity.
- **Adjust Stock** — open the stock adjustment dialog (member and above).
- Delete part icon — visible to instance admins only; permanently removes the part record.

---

## Adding a part

Click **Add Part** on the Parts page. The dialog walks through a resolution flow:

1. **Entry** — enter either a distributor SKU + distributor selection, or an MPN (and optionally manufacturer). At least one is required.
2. **Resolving** — partrack fans out queries across your team's configured distributors and waits for results (typically a few seconds).
3. **Review** — the dialog shows what it found and what it proposes to do:
   - **Unknown part import** — the MPN/SKU was not in your catalog. Review distributor results, pick the primary listing (which determines whose data populates the part record), optionally include additional listings from other distributors, optionally set an initial stock quantity and storage location, then click **Commit** to create the part and inventory entry.
   - **Known part — create entry** — the part already exists globally but your team is not tracking it yet. Optionally enter an initial quantity and storage location, then commit.
   - **Known part — adjust stock** — the part is already tracked. Adjust the quantity in the review step.
4. **Done** — confirmation screen; you can dismiss or start another import.

If resolution fails entirely (unrecognised identifiers, distributor errors), the dialog falls back to a **manual entry** form where you can enter scope, MPN, manufacturer, description, and initial stock without any distributor lookup.

Duplicate detection: if you enter an MPN + manufacturer combination that already exists in the catalog, the API returns the existing part record with a conflict notice rather than creating a duplicate.

Required role: **member** or above.

---

## Generic and parametric parts

partrack supports **generic parts** — parts identified by a parametric specification rather than a specific MPN (for example, a "10 nF 50 V X7R 0603 capacitor" that any compatible manufacturer can satisfy).

Generic parts appear in the Parts table alongside specific parts. They have no MPN from a manufacturer; instead, they have a synthesized MPN and description generated from their category, package, and parameter values (e.g. `CAP-0603-10N`). They are always team-scoped.

**Creating generic parts:**

- **Batch Generics** button on the Parts page — opens a dialog that creates a whole set of generic parts in one go: you pick a category (must have a generic code configured), a package, a value set (either an E-series range such as E24 from 100 Ω to 100 kΩ, or an explicit list), and optional fixed secondary parameters (tolerance, voltage rating, dielectric, etc.). A dry-run preview shows exactly which parts will be created and which already exist before you commit.
- Individual generic parts can also be created via BOM import when a BOM line matches a parametric specification. See [projects-bom.md](projects-bom.md) for how generic BOM lines are interpreted.

Generic part creation is idempotent: the same category + package + parameter signature always resolves to the same existing generic part rather than creating a duplicate.

Required role: **member** or above.

---

## Parameters

Parameters are the technical attributes of a part (resistance, capacitance, voltage rating, tolerance, package, etc.).

### Parameter sources

A part can have parameters from multiple sources simultaneously:

- **User-specified** — values you enter manually in the UI. These always take precedence over distributor data for display, search, and filtering. You cannot overwrite them by re-enriching.
- **Distributor-sourced** — values fetched from distributor APIs during enrichment. Different distributors may report different values for the same parameter; all are stored and labelled with their source.

The **Parameters** tab on the part detail page has two sub-tabs: one for user-specified, one for distributor-sourced. On the combined (distributor) view, parameters from the currently preferred source are highlighted.

### Preferred parameter source

When two distributors disagree on a value, you can designate one as the **preferred parameter source** (set via the "Edit Part" dialog's lifecycle/source section or via the `PATCH /parts/{id}` API). When a preferred source is set, only that distributor's values are used when the system needs a single authoritative value (e.g. for parameter-based filtering). When no preferred source is set, all distributor values are visible but conflicts may be ambiguous.

User-specified values always win regardless of this setting.

### Adding or overriding a parameter

In the Parameters section of the part detail page, click the **+** icon (or the edit pencil on an existing distributor parameter) to enter or override a value. Fields:
- **Name** — the parameter name. If it matches a known canonical parameter (or an existing distributor parameter name on this part), the system resolves it to the canonical registry entry automatically.
- **Value** — the raw text value. SI prefixes are understood (`10k`, `4.7µ`, `100n`). Range parameters accept `low ~ high` notation.
- **Unit** — optional; inherit from the canonical definition when matched.

On save, the value is canonicalized and numerically parsed into base-SI units so it participates in value-based search and filtering exactly as distributor values do. For example, a resistance of `10k` becomes 10,000 Ω internally. For `enum_set` parameters (comma-separated feature lists like `CANbus, EBI/EMI, Ethernet`), the individual members are indexed separately so you can filter by any single member.

To delete a user-specified parameter, click the trash icon next to it. Distributor-sourced parameters can only be removed by deleting the listing that provided them.

Required role: **member** or above.

### Value types

| Type | What it stores | Example value |
|---|---|---|
| numeric | Single SI-parsed number | `4.7µ` (stored as 4.7×10⁻⁶) |
| range | SI-parsed min and max | `1V ~ 5.5V` |
| text | Free text | `N-Channel MOSFET` |
| enum | Single choice from a defined set | `X7R` |
| enum_set | Comma-separated list of members | `CANbus, EBI/EMI, Ethernet` |

---

## Distributor listings

A **distributor listing** represents one purchasable offering of a part: a specific distributor × packaging type combination with its own SKU, pricing, and ordering constraints. A single part can have many listings (different distributors, different packaging, different MOQ).

### What a listing contains

- **Distributor name** and **SKU** (the distributor's order code)
- **Packaging type** (Cut Tape, Tape & Reel, Tray, Bulk, etc.)
- **Unit price** — cached from the distributor API; may be stale
- **Stock available at distributor** — cached
- **Minimum order quantity (MOQ)** — the smallest quantity the distributor will ship
- **Fixed order cost** — a one-time surcharge per line item, not per unit (e.g. a full-reel setup fee; zero for most listings)
- **Price breaks** — tiered pricing table (expand a listing row to see the full table)
- **Last refreshed** timestamp — when the cached data was last updated

### Refreshing listing data

Because price and stock are cached, they go stale. To update a single listing, expand its row in the Listings section and click the refresh icon. To refresh all listings on a part at once, click **Refresh all** in the Listings section header. From the Parts table, the **Refresh listings** bulk action refreshes all listings across the current search results.

Refresh runs as a background job. When it completes, the page updates automatically via SSE.

The cache duration is configured per-distributor per-team in the Distributor Configurations settings (team admin only). Refreshes that find the distributor unavailable or the SKU delisted update the listing's last-refreshed timestamp without changing the stale data.

### Adding a listing to an existing part

In the Listings section on the part detail page, click **Add Listing**. Enter the distributor and SKU. This triggers an enrichment job that fetches the listing data and any new parameters from that distributor. The enrichment runs in the background.

To remove a listing, click the delete icon on the listing row. Removing a listing also removes all distributor-sourced parameters that came exclusively from that listing's distributor. Parameters from other distributors or user-specified parameters are not affected.

Required role: **member** or above.

### Enriching a part from a specific SKU

To pull full parameter and listing data from a distributor SKU onto an already-known part, click the sparkle (re-enrich) icon on any listing row. This queues a background enrichment job for that SKU. When complete, parameters and listing data are updated and the page refreshes.

### Preferred listing

Your team can designate one listing as the **preferred listing** for a part — the default purchasing option. Click the star icon on a listing row to set it. The preferred listing is used by the cart to pre-select a source when adding parts to replenish low stock. The star icon is only shown when the part has an inventory entry.

Required role: **member** or above.

---

## Inventory entries

An **inventory entry** is your team's profile for a specific part. There is at most one entry per part per team.

An entry is created automatically the first time a part becomes relevant to your team:
- When a BOM line resolves to that part during import
- When you click "Start Tracking" on the part detail page
- When you commit a part import via the Add Part dialog
- When an order for that part is received (as a fallback)

An entry starts with zero quantity. Having an entry does not mean you have stock.

### Inventory entry settings

The inventory settings (editable via **Edit Settings** on the part detail page, or via the Inventory entry page) contain:

- **Storage location** — where this part is normally stored. One location per part (optionally with a grid coordinate within that location). See [storage.md](storage.md) for how hierarchical storage and grid locations work.
- **Preferred listing** — the default distributor listing used when ordering this part (also settable via the star icon on a listing row).
- **Minimum stock quantity** — when on-hand falls below this level, the part is flagged as "Low stock" in the Parts table and appears in inventory filters. If not set, the part is never flagged as low.
- **Reorder quantity** — when adding a part to the cart for replenishment, the cart defaults to this quantity rather than just the shortfall. If not set, the shortfall quantity is used.

Required role: **member** or above.

### Inventory page

The **Inventory** page (accessible from the sidebar) shows all inventory entries for your team in a searchable, paginated list. Filters:

- **Search** — free-text search by MPN or description.
- **Storage location** — filter by assigned location.
- **Below min stock** — toggle to show only parts whose on-hand quantity is below their minimum stock quantity.

Clicking a row opens that part's detail page.

You can also create a new inventory entry from this page via the **+** button — useful for starting to track a part your team already owns before any import or order.

### Replenishment / low-stock cart

From the **Cart** page, the **Add Low Stock** action gathers all inventory entries that are below their minimum stock level and adds them to the cart for ordering, using each entry's reorder quantity (or the shortfall if no reorder quantity is set) and preferred listing. See [cart.md](cart.md) for the full cart workflow.

---

## The stock ledger

Quantity on hand is computed from an append-only **stock adjustment ledger**. You cannot edit a past quantity; you can only add new adjustments.

### Ledger entry sources

| Source | Created by | When |
|---|---|---|
| Order line item receipt | System (on order receipt) | Parts received from an order |
| Build | System (on build assembly) | Parts consumed assembling boards |
| General checkout | Member action (Checkouts page) | Ad-hoc withdrawal not tied to a build |
| Manual correction | Member action (Adjust Stock) | Stocktake correction, no other source |

Every ledger entry records:
- **Delta** — positive (stock added) or negative (stock consumed/written off)
- **Adjusted at** timestamp
- **Reason** — free-text note; required for manual corrections
- **Created by** — the user responsible
- **Source** — a link to the order, build, or checkout that caused the change (null for manual corrections)

The History section on the part detail page shows the full ledger. Each row displays date, delta (green for positive, red for negative), reason, source (linked to the relevant order or build), and who made the change.

### Adjusting stock manually

Click **Adjust Stock** on the part detail page (requires a tracked inventory entry). The dialog offers two modes:

- **Delta mode** — enter a positive or negative change (e.g. `+50` to add 50, `-5` to write off 5). The delta must be non-zero.
- **Set mode** — enter the new absolute quantity. Internally this computes the delta against the current on-hand and creates a ledger entry for the difference.

A reason field is optional for positive adjustments and strongly recommended for negative ones. You cannot adjust stock below zero — the system rejects any adjustment that would take on-hand negative.

Positive manual adjustments automatically re-evaluate pending builds: if any ordered build now has enough stock reserved, it transitions to the "ready" state.

Required role: **member** or above.

---

## General checkouts

A **general checkout** records an ad-hoc withdrawal of parts that is not tied to a project or build — repairs, bench testing, samples, shipping from your own shelf, etc. It is the correct way to deduct stock for miscellaneous consumption.

### Creating a checkout

On the **Checkouts** page, click **New Checkout**. In the dialog:

1. Optionally enter a **reason** (e.g. "Prototype for client demo").
2. Add one or more line items. For each item, type in the MPN search field to find the part by MPN — results show the part's current on-hand quantity. Select the part, then enter the quantity to withdraw.
3. You can add as many line items as needed by clicking **Add Item**.
4. Click **Record Checkout**.

If the system shows less stock than you are trying to withdraw, a **Stock Count Mismatch** dialog appears. You can enter the actual physical count you observe (which must be at least the quantity you want to check out). The system will apply a reconciliation adjustment first (to correct the discrepancy), then apply the checkout deduction. This prevents the system from blocking a legitimate checkout just because its records are slightly off.

The checkout creates a negative stock adjustment for each line item, linked to the checkout record as its source.

Checkout records appear in the Checkouts list. Click a row to see its details (date, who performed it, line items, and the stock adjustment records it created).

Required role: **member** or above.

### Reservation warnings

If you check out a quantity that overlaps with stock reserved by active builds, the system records the checkout but returns a warning listing which parts were affected, how much was reserved, and how much was actually free. The checkout is not blocked — it is your judgment call whether to proceed. If you do proceed and the reserved stock is now short, the affected builds will be unable to fully kit without new stock arriving.

---

## Role summary for parts & inventory

| Action | Viewer | Member | Purchaser | Admin |
|---|:---:|:---:|:---:|:---:|
| View parts, parameters, listings | ✓ | ✓ | ✓ | ✓ |
| View inventory entries and stock history | ✓ | ✓ | ✓ | ✓ |
| Add or edit a part (team-scoped) | | ✓ | ✓ | ✓ |
| Add or override parameters | | ✓ | ✓ | ✓ |
| Add, refresh, or delete a listing | | ✓ | ✓ | ✓ |
| Start tracking (create inventory entry) | | ✓ | ✓ | ✓ |
| Edit inventory settings (location, min stock, reorder qty) | | ✓ | ✓ | ✓ |
| Adjust stock manually | | ✓ | ✓ | ✓ |
| Create a general checkout | | ✓ | ✓ | ✓ |
| Create generic parts (individual or batch) | | ✓ | ✓ | ✓ |
| Add a part to the cart | | | ✓ | ✓ |
| Create a global part | | | | Instance admin |
| Delete a part | | | | Instance admin |
| Configure distributor integrations | | | | Team admin |

---

## Things you cannot do

- **Edit a past stock adjustment** — the ledger is immutable. To correct a wrong count, add a new adjustment in the opposite direction (or use Set mode in the Adjust Stock dialog, which does this automatically).
- **Take stock below zero** — manual adjustments and checkouts that would result in negative on-hand are rejected.
- **Delete a part** — only instance admins can remove a part record. Standard practice is to set the lifecycle status to Obsolete or Deprecated.
- **Have two inventory entries for the same part** — each team has at most one inventory entry per part. Attempting to create a duplicate returns a conflict error.
- **Bulk-refresh more than 1,000 listings or re-enrich more than 1,000 parts at once** — narrow the search filter to reduce the scope.
- **Create a global part as a regular user** — global parts can only be created by instance admins.

---

## Related chapters

- [storage.md](storage.md) — how to set up hierarchical storage locations and grid coordinates
- [projects-bom.md](projects-bom.md) — how BOM import resolves line items to parts and creates inventory entries
- [cart.md](cart.md) — adding parts to the cart, replenishment from low stock
- [orders.md](orders.md) — how orders receive stock into inventory
- [builds.md](builds.md) — how assembled builds deduct stock through the ledger
- [scanning.md](scanning.md) — mobile scanning and QR code workflows for parts and storage locations
- [roles-and-permissions.md](roles-and-permissions.md) — full role reference
