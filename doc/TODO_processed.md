# partrack TODOs (Processed)

This is a reorganized and annotated version of `TODO.md`. Items are grouped by theme and ordered by dependency — foundational work comes first, features that depend on it follow.

---

## Phase 1: Foundations & Infrastructure

These items are either prerequisites for later features, or independent improvements that unblock multiple workstreams.

### 1.1 Generic Advanced Table Component

**Priority: High — many features downstream depend on this.**

A reusable table component for the frontend, replacing the current per-page table implementations. Not all tables should use it (e.g. PriceBreaksTable stays as-is), but parts lists, distributor listings, BOM line items, orders, stock adjustments, etc. would all benefit.

**Architecture:** Split into a DataSource interface and a Presentation layer.
- DataSource variants: Local (full dataset in frontend) and Remote (paged, backend-driven).
- Presentation layer is shared and handles sort/filter state, driven by column specs from the DataSource.

**Core requirements:**
- Sorting by arbitrary column
- Column filtering (by available values or ranges)
- Pagination (remote sources)
- CSV export (subtle icon, not prominent; local sources export in-browser, remote sources may need a backend endpoint)
- Row click actions
- Detail panels (expandable rows, cf. CartItemDetail)
- Actions column (buttons, no header)

**DataSource interface (rough sketch):**
- `Get` — full local dataset
- `GetPaged(page, pageSize)` — for remote sources
- `GetExport` — separate call allowing the source to provide additional/different data for export
- Filtering and sorting can be default implementations on the base classes, overridable per source
- Column specs define: data type (numeric, currency, enum/options, freetext), optional unit info, which columns are sortable/filterable
- All data calls are async (loading spinner while waiting)

**General search bar over the table:** Leave this to individual DataSource implementations — the semantics vary too much between use cases to generalize.

**Caching:** TanStack Query already caches backend calls. Column specs (available filter values/ranges) should also be cached or memoized to avoid recomputation.

> **Annotation:** This is well thought out. The DataSource/Presentation split is the right call. I'd suggest starting with one concrete table migration (e.g. the parts list) to validate the interface before building out all data sources. The "style overrides" deferral is wise — resist the urge to add them until a real need surfaces.
>
> **Question:** Should the remote DataSource handle cursor-based pagination as well as offset-based? (Yes)

---

### 1.2 Events and Query Invalidation Simplification

**Priority: Medium — architectural hygiene, improves developer experience.**

Currently the server sends SSE invalidation events to other team members but not to the acting user. The acting user relies on the handler response + manual `invalidateQueries` calls.

Proposal: Send events to the acting user as well, and use events as the sole invalidation mechanism. This would:
- Simplify handler code (no need for manual invalidation after mutations)
- Surface multi-user invalidation bugs immediately in single-client testing
- Unify the invalidation path

> **Annotation:** This is a good simplification. The main concern is latency — currently the UI updates instantly from the mutation response, while waiting for SSE would add a round-trip. The standard pattern is optimistic updates from the mutation response + SSE for cross-tab/cross-user sync. Consider whether you want to give up the instant feedback, or keep the mutation response for optimistic updates while also delivering the SSE event for consistency.
>
> **Question:** Would it be acceptable to keep the mutation response for immediate UI feedback but also deliver the SSE event to the acting user? That way invalidation logic is unified (always via SSE) without sacrificing responsiveness.

---

### 1.3 Additional Parts Information (Basic)

**Priority: Medium — small scope, improves data quality.**

Store and surface additional part metadata that we already receive from DigiKey but currently discard:

- **Short + long description:** Store both. Short description remains the default in tables; long description shown in detail views. Search should cover both.
- **Part aliases** (DigiKey: `OtherNames`): Store for reference. May assist manual resolution during BOM review, but should not be used for automatic matching.
- **URLs:** Distributor product page URL, datasheet URL (may already be stored?), product image URL.

> **Annotation:** This is straightforward and can be done incrementally. The alias storage is useful groundwork for improving BOM resolution later. Verify what's already stored — I believe datasheet URL may already be captured during enrichment.

---

## Phase 2: Categorization & Parameters

These two features form a dependency chain: categories must exist before parameters can be meaningfully organized, and both are prerequisites for the advanced search.

### 2.1 Category Handling

#### Part A: Data Model & Backend

**Priority: High — foundational for parameters and search.**

Introduce a partrack-native category system that maps distributor-specific categories into a common hierarchy.

**Key design decisions:**
- Partrack categories are first-class, stable entities. IDs never change or get reused. Hierarchy is supported (parent-child).
- Distributor categories are second-class — stored on distributor listings for reference, mapped to partrack categories.
- Mapping is [1 partrack] → [N distributor categories]. Unmapped distributor categories fall back to parent category mapping. Parts with no mapping at all remain uncategorized.
- Categories define a set of **primary parameters** (e.g. Resistance + Tolerance for resistors, Capacitance + Dielectric + Tolerance for capacitors). Package is always a primary parameter.
- Categories and mappings are configurable. Default mappings ship with the application; overrides can be defined in config.

**Bootstrap process:**
1. Query DigiKey categories API to get the full category tree (write a script for this, don't run inline — output will be large)
2. Define the partrack category hierarchy (likely close to DigiKey's, with specialty categories removed)
3. Create the default mapping

**Storage considerations:**
- Category hierarchy doesn't need to be in the DB as a recursive structure — a single `category_id` column on parts, with the hierarchy defined in application config/code, is sufficient. The server resolves parent lookups at query time.
- ID type: UUIDs are overkill for <1M expected categories. A simple integer or short string ID would be more efficient and readable.

> **Annotation:** The "config file vs database" question for category definitions is worth resolving early. Categories change rarely and apply globally, which argues for config. But if you ever want an admin UI for managing categories (which you probably will), database storage is much more practical. I'd lean toward database with a seed/migration for the defaults.
>
> **Question:** Should the category-to-part assignment be automatic (based on distributor category mapping during enrichment) or require manual confirmation? Automatic is more practical at scale, but could misclassify edge cases.

#### Part B: Frontend

- Display category as a string in part views (minimal change to API)
- Add hierarchical category browser/navigator for the parts catalog
- Category selection feeds into the advanced search (Phase 3)

> **Annotation:** The category browser is a natural fit for a sidebar or dropdown tree in the parts page. Keep it simple initially — just a flat list with indentation for hierarchy is fine before investing in a full tree widget.

---

### 2.2 Parameter Handling

**Priority: High — depends on 2.1, required for search.**

Rework parameters from display-only text to structured, searchable data.

**Core problem:** Parameters currently come in as distributor-specific text. To enable search-by-parameter, we need:
1. A partrack-native parameter registry (mapped from distributor parameters)
2. Parsed numeric values (not just text) stored in the database
3. Unit normalization (SI prefix handling)

**Design outline:**
- Partrack parameters are canonical. Mapping is [0..1] partrack param ↔ [0..1] distributor param per distributor. A distributor "Resistance" maps to partrack "Resistance", but not every distributor parameter needs a partrack equivalent, and vice versa.
- Numeric parsing with SI prefix handling: `10kΩ` → `(UNIT_OHM, 10000)`, `100nF` → `(UNIT_FARAD, 1e-7)`, `2.4GHz` → `(UNIT_HERTZ, 2.4e9)`. Need to investigate existing Go/JS packages for SI prefix parsing.
- Parsing configuration is per-distributor, per-parameter (different distributors format values differently).
- Store both the original text value and the parsed numeric value + unit in the parameter table.
- Parameters are linked to categories: the set of searchable parameters in a category is derived from the parameters present on parts in that category.

**Mapping management:**
- Ship with a default mapping for DigiKey's common parameters.
- Unknown/unmapped parameters are collected and flagged for admin review.
- User involvement: admins can create new partrack parameters and link them to distributor parameters. Regular users should not need to interact with this.

> **Annotation:** This is the most complex item in the list. I'd suggest breaking it into sub-phases:
> 1. Parameter registry + mapping config (no parsing yet, just mapping names)
> 2. Numeric parsing + unit normalization (start with the most common: resistance, capacitance, inductance, voltage, frequency)
> 3. Database schema for searchable numeric values
> 4. Admin UI for managing mappings (can be simple initially)
>
> **Question:** For value ranges (e.g. tolerance "±5%"), do you want to store min/max as separate fields, or is a single nominal value + tolerance sufficient? This affects both schema design and search UX.
>
> **Reference:** DigiKey parameter structure at `jq .definitions.ParameterValue doc/distributor/digikey/ProductSearch.json`

---

## Phase 3: Search & Discovery

### 3.1 Advanced Part Search

**Priority: High — depends on 2.1 + 2.2 + 1.1. Central UX feature.**

Unified search component replacing the separate implementations in:
- Parts page "Add Part" flow
- BOM review dialog part matching

**Requirements:**
- Default: search local catalog. Option to extend to distributor APIs.
- Local searches are live (as-you-type). Distributor searches require explicit confirmation (button press) — respect rate limits.
- Category selector (hierarchical navigation) narrows the search scope.
- When a category is selected, parameter filters appear based on that category's parameter set.
- Results table is structurally identical for local and distributor results. Distributor results also show local stock info if the part exists locally.
- Pre-configurable search parameters (enables reuse for BOM review and substitute search).
- User preferences for default parameter display (persistent, per-user).

**Distributor API considerations:**
- DigiKey's parameter search may work differently from partrack's (e.g. discrete value selection vs. numeric range). May need a two-step process: first query available parameter values, then filter and search.
- Cache distributor API responses aggressively (in-memory). Rate limits are a real constraint.
- Defer investigation of Mouser/Farnell/LCSC search APIs.

> **Annotation:** This is a major UI component. I'd suggest building it in layers:
> 1. Basic text search across local catalog (replaces current "Add Part")
> 2. Add category filtering
> 3. Add parameter filtering (once 2.2 is done)
> 4. Add distributor search integration
> 5. Make it embeddable (for BOM review, substitute search)
>
> The pre-configuration / embedding aspect is critical for reuse. Design the component's props/API carefully upfront even if you only implement basic search first.

---

## Phase 4: Build & Order Workflow

### 4.1 Part Substitution Handling

**Priority: Medium — depends on 3.1 for the substitute search UI.**

Allow BOM line items to have approved substitute parts. Substitutes are used when the primary part is out of stock.

**Rules:**
- Substitutes are defined per BOM line item.
- A substitute is only used when it can fully cover the line item demand (no split fulfillment).
- During stock check, the system checks primary → substitutes (in order) for sufficient inventory.
- Substitute selection is saved on the build but remains editable while in Draft status. Changing the selection triggers recalculation of stock and pricing.
- Once the build leaves Draft (→ Pending), the selection is locked.
- Part arrivals are booked to the correct build line item regardless of whether a primary or substitute was chosen.

**UI: "Manage Substitutes" per line item in BOM detail view:**
- View/reorder/remove current substitutes
- Import substitutes from a previous revision (only show revisions that included the same part with substitutes)
- Add new substitutes via the advanced search component (pre-populated with category primary parameters)

> **Annotation:** The "no split fulfillment" rule is a good simplification. Consider whether substitute priority order matters (it sounds like it does — "in order"). If so, the ordering in the manage-substitutes UI is load-bearing and should be explicit.
>
> **Question:** Should the stock check results UI clearly show when a substitute was selected and which one? This affects the StockCheckDialog and build detail views.

---

### 4.2 Cart — Manual Modifications

**Priority: Low — small feature, independent.**

When a user manually adjusts the order quantity for a build-required part, the adjustment should be tracked as a separate manual addition in "Section 2: Manual Requests" of the cart. The build's demand remains as a lower bound on the total order quantity.

> **Annotation:** Straightforward. The key UX decision is whether the user can reduce below the build demand (probably not — the manual section is additive only) or if the build demand is truly a hard floor.

---

### 4.3 Order Detail Improvements

**Priority: Medium — polish, partially blocked by 1.1.**

Several fixes and enhancements for the order detail view:

- [ ] **Progress modal for cart conversion:** Currently no feedback when the user presses "Convert". Add a progress modal similar to BOM import.
- [ ] **Purchase order number field:** User-provided PO number (alphanumeric, no assumptions about format). Separate from partrack's internal order ID.
- [ ] **Distributor order number field:** To link partrack orders to the distributor's own order tracking.
- [ ] **Order summary/details panel:** Add a header section summarizing the order.
- [ ] **Line item totals:** Show totals row.
- [ ] **Unit price precision:** Display 4 decimal places for unit prices.
- [ ] Will use the generic table component (1.1) once available — defer table rework until then.

> **Annotation:** The PO number and distributor order number are important for real-world usability. These are simple schema additions (`purchase_order_number` and `distributor_order_number` on the orders table) and could be done quickly independent of other work. The progress modal is also an independent fix.

---

## Phase 5: Platform Features

### 5.1 Builds Dashboard

**Priority: Medium — independent, good UX improvement.**

A dashboard card showing the builds pipeline across all stages. Full page width, with a scrollable section per stage: `[Draft] → [Pending] → [Ordered] → [Ready]`. Cancelled builds excluded.

Each build entry should be compact — take inspiration from the search result rows in BomReviewDialog.svelte (~line 348). Show: project name, revision, quantity, created date, assignee.

> **Annotation:** This is self-contained and could be implemented anytime. Good candidate for a quick win when you want a break from the heavier items.

---

### 5.2 Persistent Notifications

**Priority: Low — important eventually, but not blocking anything.**

A notification system beyond ephemeral toasts. Use cases: part arrivals, new pending cart items, build status changes.

**Components:**
- Notification dropdown in the user menu (accessible from all pages)
- Dashboard notification list
- Email delivery (deferred)
  - Debouncing/batching to avoid spam filter issues
  - Importance-based: not everything needs an email

> **Annotation:** This requires both backend infrastructure (notification storage, delivery) and frontend work. The scope is large if you include email. I'd suggest starting with in-app notifications only (DB table + dropdown) and deferring email entirely until the notification types and importance levels are well understood from real usage.

---

### 5.3 Advanced DigiKey API Integration (Lists & Quotes)

**Priority: Low — depends on ordering workflow maturity.**

Connect partrack's ordering flow to DigiKey's Lists and Quotes APIs. Not full ordering automation, but an alternative flow where users can push their cart/order to DigiKey's system for quoting.

> **Annotation:** This should wait until the basic ordering workflow (cart → order → receive) is solid and well-tested. The DigiKey Lists/Quotes APIs add complexity and are only valuable when the rest of the pipeline works smoothly.

---

### 5.4 Global Navigation / Search

**Priority: Unknown — no details provided in TODO.md.**

> **Question:** What's the vision here? A command-palette style search (Cmd+K) that searches across parts, orders, projects, builds? Or something else? This is just a header in the original TODO — needs requirements.

---

## Summary: Dependency Graph & Suggested Order

```
Phase 1 (Foundations)
  1.1 Generic Table ─────────────────────────────┐
  1.2 Event Invalidation (independent)           │
  1.3 Additional Part Info (independent)          │
                                                  │
Phase 2 (Categorization)                          │
  2.1 Categories ──► 2.2 Parameters               │
                         │                        │
Phase 3 (Search)         │                        │
                         ▼                        ▼
                     3.1 Advanced Part Search (needs 2.1 + 2.2 + 1.1)
                         │
Phase 4 (Workflows)      │
  4.1 Substitutions ◄────┘
  4.2 Cart Manual Mods (independent)
  4.3 Order Details (partially needs 1.1)

Phase 5 (Platform)
  5.1 Builds Dashboard (independent)
  5.2 Notifications (independent)
  5.3 DigiKey Advanced API (needs mature ordering)
  5.4 Global Search (needs requirements)
```

**Suggested implementation order for maximum unblocking:**
1. 1.1 (Generic Table) + 1.3 (Part Info) + 1.2 (Event Invalidation) — can be parallel
2. 2.1 (Categories)
3. 2.2 (Parameters)
4. 3.1 (Advanced Search) — at this point the table component should be mature
5. 4.1 (Substitutions) + 4.3 (Order Details) — can be parallel
6. 5.1 (Builds Dashboard) + 4.2 (Cart Mods) — can be done anytime as breather tasks
7. 5.2 (Notifications) + 5.3 (DigiKey API) — later, when core workflows are stable
