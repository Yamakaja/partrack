# Builds

A **build** is an assembly event — it records that your team built a specific number of boards for a specific project revision. Builds are what ultimately deduct parts from stock; they also drive purchasing demand while they are in progress by contributing shortfalls to the [shopping cart](cart.md).

You need at least the **member** role to create and manage builds. The cart-inclusion toggle requires the **purchaser** role. See [Roles and Permissions](roles-and-permissions.md) for role definitions.

---

## The Build Lifecycle

A build moves through six states. Only the four "active" states (draft, pending, ordered, ready) appear in the pipeline view; assembled and cancelled are terminal.

| State | Meaning | How you get here |
|---|---|---|
| **draft** | Created; no stock is touched or reserved | You create a build plan |
| **pending** | Submitted; its shortfalls feed the cart | You submit a draft (when stock does not fully cover it) |
| **ordered** | Parts to cover it have been placed on one or more orders; soft allocations track incoming stock | Cart is converted to orders ([see Orders](orders.md)) |
| **ready** | All required parts are on hand or allocated; the build can be assembled | Submit (if fully covered by stock) or orders received to cover the last shortfall |
| **assembled** | Boards built; stock has been deducted via the ledger | You check out the build |
| **cancelled** | Abandoned; allocations released | You cancel a non-terminal build |

A build whose parts are **fully covered by on-hand stock at submit time** skips pending and jumps straight to **ready**. Soft reservations are created immediately when this happens.

---

## The Builds Page

Navigate to **Builds** in the sidebar. The page has two sections:

- **Active Builds pipeline** — a four-column Kanban-style board showing every non-terminal build grouped by state (draft, pending, ordered, ready). Your own builds appear first in each column. Builds excluded from the cart appear muted and italicised in the Pending column.
- **All Builds table** — a paginated list of every build including assembled and cancelled ones. You can filter by status, sort by project, revision, quantity, status, or created date, and filter by creator using the funnel icon in the Creator column header.

Click any row or pipeline card to open the build detail page.

---

## Creating a Build Plan

Click **Create Build Plan** in the top-right corner of the Builds page.

The dialog has two steps:

1. **Select project and revision** — choose the project, then choose which BOM revision to build against. The active revision is pre-selected.
2. **Set quantity and notes** — enter how many boards you plan to build (must be ≥ 1) and optionally add free-text notes.

When you confirm, the build is created in **draft** status. No stock is touched. Substitute selections are computed asynchronously in the background (see [Substitutes](#substitutes-and-substitute-selections) below). You are taken to the build detail page immediately.

**What you can set on a draft:**
- Quantity and notes (editable while draft or pending)
- Excluded designators
- Per-line-item overrides (skip / use available)
- Substitute selections

---

## Build Detail Page

The build detail page shows:

- **Metadata card** — project name, BOM revision label, planned quantity, assembled quantity (once assembled), creation date and creator, assembly date and assembler.
- **Status badge** — current lifecycle state.
- **Parts section** — varies by state:
  - *Draft / Pending* — a **Required Parts** table showing each BOM line item with its part MPN, reference designators, quantity per board (effective, after any designator exclusions), total needed, on-hand quantity, stock status badge, and substitute selector.
  - *Ordered / Ready* — an **Ordered Parts** table showing allocations: each part's quantity needed, reserved, and received, plus a Covered/Partial/Missing badge.
  - *Assembled* — a **Consumed Parts** table listing every part deducted and how many units were taken.
- **Linked Orders** section — links to any orders created to cover this build.

---

## Editing Quantity and Notes

While a build is in **draft** or **pending** status you can change its planned quantity and notes. Open the build detail page and use the edit controls (the UI provides inline editing for quantity and notes in these states).

Changing the quantity on a pending build immediately recomputes the cart shortfall so purchasing demand stays accurate.

You **cannot** change the quantity or notes once a build is **ordered** or **ready**.

---

## Excluded Designators

Sometimes you want to skip certain board positions — for example, DNP (do not place) components or positions you are populating from a different stock source. You can exclude specific reference designators from the deduction calculation.

**How to exclude designators (draft or pending only):**

- Click any row in the Required Parts table to expand it. A chip list of all designators for that line item appears. Click individual chips to toggle them in or out. Use **All**, **None**, and **Invert** buttons to bulk-select.
- Use the **Exclude designators…** button above the table to paste a comma-, space-, or newline-separated list (e.g. from a KiCad or Altium DNP export). Only designators that actually appear in the BOM are applied.

Excluded designators reduce the effective quantity per board for the affected line item. If all designators for a line item are excluded, that line item is dropped entirely from the build and shows an "Excluded" badge.

**You can only change excluded designators while the build is in draft or pending status.** Changing them on a pending build also immediately recomputes cart demand.

---

## Per-Line-Item Overrides

When a line item is short on stock and you cannot or do not want to wait for the part, you can apply an override to that specific line item:

| Override | Effect |
|---|---|
| **Use available** | Deducts however much is actually on hand (may be less than needed). Shown as a "Partial" badge. Appears as a button when stock is partial but nonzero. |
| **Skip** | Deducts nothing for this line item. Shown as a "Skipped" badge. Appears as a button when stock is zero. |

Overrides are available from the action column in the Required Parts table (visible when the build is draft or pending). You can undo an override with the **undo** link that appears once it is set.

Overrides participate in the checkout stock check: a skipped item is not deducted; a use-available item caps its deduction at actual on-hand quantity at the moment of checkout.

**Overrides can only be set while the build is in draft or pending status.**

---

## Substitutes and Substitute Selections

If a BOM line item has approved substitutes defined (see [Projects and BOMs](projects-bom.md)), partrack computes which substitute — or the primary part — has the best stock coverage for this specific build quantity. This computation runs as a background job immediately after you create a draft build.

You can see and override the selection in the **Using** column of the Required Parts table. Each row with substitutes shows a dropdown; select "Primary" to revert to the original part, or pick a specific substitute by MPN and on-hand count.

**Staleness warning:** If inventory moves after the selections were computed, a yellow banner appears on the draft build page indicating that selections may be out of date. Click **Refresh** to trigger a recomputation. The system blocks submission until selections are fresh because stale selections can cause reservation math to use incorrect stock counts.

**Submit is blocked** until substitute selections have been computed (the blue spinner banner is shown while computation is pending).

---

## Submitting for Fulfilment

Once you have configured the build (quantity, excluded designators, overrides, substitute selections), click **Submit for Fulfilment** to move it from draft to active.

On submission:
1. The system checks whether free stock — on-hand quantity minus what is already reserved by other builds — fully covers this build's demand.
2. **If fully covered:** soft allocations are created earmarking that stock, and the build moves to **ready**.
3. **If not fully covered:** the build moves to **pending** and its shortfall is added to the team cart. No allocations are created yet.

Submission triggers a cart recompute, so the cart immediately reflects the new demand.

**Submit is disabled if substitute selections have not yet been computed, or if they are stale.** Refresh them first.

---

## Kitting with a Mobile Scanner

When a mobile scanner is active and connected, a **Send to Scanner** button appears on the build detail page for any build that is not yet assembled (or cancelled) — including drafts. This starts a **kitting session** — a guided workflow on the mobile scanner that walks through each required part and asks you to confirm the quantity you are placing in the kit. The pick list comes from the build's BOM (honouring excluded designators, line-item overrides, and substitute selections), so it works at any stage of the build's lifecycle.

While a kitting session is active, the build detail page shows a **Kitting in Progress** panel with a progress bar and per-part status (confirmed, shortfall, pending). Parts are sorted so pending items appear first.

Kitting sessions are for physical pick-and-place verification. They do not deduct stock — that happens at checkout. See [Scanning](scanning.md) for the mobile scanner workflow.

---

## Soft Allocations and How Orders Fill the Kit

Once a build is ordered or ready, the **Ordered Parts** table shows its allocations:

- **Quantity needed** — total units required by this build.
- **Quantity reserved** — units earmarked from current on-hand stock (set at submit time or when the order was converted).
- **Quantity received** — units that have arrived and been received against linked orders and credited to this build.
- **Coverage badge** — Covered (reserved + received ≥ needed), Partial, or Missing.

When you receive an order line item (see [Orders](orders.md)), partrack allocates the received quantity to linked builds in order. As builds receive their full allocation, their status transitions from ordered to **ready** automatically.

Cancelling a build releases all its allocations, making that reserved stock available to other builds or manual adjustments. Linked orders are **not** cancelled automatically — cancel those separately if needed.

---

## Checking Out (Recording Assembly)

When a build reaches **ready** status (or is still in **draft** and all stock is on hand), you can check it out to record the actual assembly and deduct stock.

Click **Checkout** to open the checkout dialog. The dialog shows:
- A quantity field pre-filled with the planned quantity. You can reduce this to record a **partial checkout** — for example if only 8 of 10 boards were fully assembled.
- A table summarising each part, quantity per board, total needed, on-hand quantity, and status (OK / Short / Partial / Skipped / Excluded).

Click **Confirm Checkout** to proceed.

**What happens at checkout:**
- For each line item (excluding skipped items and fully-excluded items), the system deducts the effective quantity from the corresponding inventory entry, creating an immutable [stock ledger](parts-inventory.md#the-stock-ledger) entry sourced to this build.
- Items marked **use available** are capped at actual on-hand quantity under a database lock, so the deduction reflects what is physically there at the moment of checkout, not what was on hand when the override was set.
- Soft allocations are cleared.
- The build moves to **assembled**.

**If stock is insufficient at checkout time** — and the item has no override — the checkout is rejected with an "Insufficient stock" error. Apply a **use available** or **skip** override to that line item, then retry.

### Partial Checkout

If you enter a quantity lower than the planned quantity, partrack records assembly for that many boards, deducts the proportional stock, and leaves the build in the pipeline with its quantity decremented. The remaining quantity retains its allocations (adjusted for what was consumed) and the system re-evaluates whether the remainder is ready or still ordered.

The assembled quantity is visible on the build detail page after a full checkout.

### Checkout from Draft

Draft builds that happen to have full stock can be checked out directly without going through submit. The checkout button appears on a draft build when every line item in the preview table is sufficient (or overridden/excluded). This is a convenience shortcut — stock is deducted the same way.

---

## Excluding a Build from Cart Demand

A **purchaser** can toggle whether a pending build contributes to the cart shortfall calculation. This is useful when a build is on hold and you do not want it inflating what needs to be purchased.

On the build detail page, the cart-inclusion toggle is available in the build metadata area. In the pipeline view, builds excluded from the cart appear muted and italicised in the Pending column.

Toggling this setting immediately recomputes cart demand, so the change is reflected in the cart without any further action.

This toggle only affects **pending** builds. Ordered and ready builds are already allocated and do not drive further cart demand.

---

## Cancelling a Build

You can cancel a build in any non-terminal state (draft, pending, ordered, ready). Click **Cancel Build** and confirm the dialog.

Cancellation:
- Moves the build to **cancelled** (terminal — cannot be undone).
- Deletes all soft allocations for this build, freeing the reserved stock for other builds.
- If the build was pending, recomputes the cart shortfall so the cancelled demand is removed immediately.
- Does **not** cancel linked orders. If you placed orders specifically for this build, cancel them separately from the [Orders](orders.md) page.

Assembled builds cannot be cancelled. The stock deduction from an assembled build is permanent; correct the inventory with a manual adjustment if needed (see [Parts and Inventory](parts-inventory.md)).

---

## Role Summary

| Action | Minimum role |
|---|---|
| View builds and build pipeline | Viewer |
| Create, submit, edit, cancel, check out builds | Member |
| Set excluded designators and overrides | Member |
| Refresh / patch substitute selections | Member |
| Start a kitting session on a scanner | Member |
| Toggle a build's cart inclusion | Purchaser |

---

## Related Pages

- [Projects and BOMs](projects-bom.md) — importing revisions, resolving line items, adding substitutes, running stock checks
- [Cart](cart.md) — how pending-build shortfalls flow into the cart
- [Orders](orders.md) — converting the cart to orders, receiving items, and how received stock fills build allocations
- [Parts and Inventory](parts-inventory.md) — the stock ledger and how build deductions appear as immutable entries
- [Scanning](scanning.md) — mobile scanner and kitting sessions
