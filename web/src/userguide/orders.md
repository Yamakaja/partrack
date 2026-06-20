# Orders

An **order** is a purchase from a single distributor, created when you convert the [shopping cart](cart.md). Each cart conversion produces one order per distributor represented in the cart, so a mixed cart containing DigiKey and Mouser items produces two separate orders.

Orders are the bridge between buying parts and getting them into stock. Once an order arrives, you receive its line items and partrack credits the stock ledger, automatically satisfying any builds that were waiting on those parts.

---

## Who can manage orders

| Action | Required role |
|---|---|
| View orders and line items | Viewer |
| Check distributor order status | Viewer |
| Export CSV / mark placed / receive items / cancel | Purchaser |
| Import a DigiKey order / push to DigiKey / request quote | Purchaser |
| Edit notes or PO number | Purchaser |

See [Roles and permissions](roles-and-permissions.md) for a full role description.

---

## The Orders list

Navigate to **Orders** in the sidebar to see your team's complete order history. The table shows:

- **ID** — the first 8 characters of the internal UUID; click a row to open the full order detail.
- **Distributor** — the distributor this order was placed with.
- **Status** — current lifecycle stage (see below).
- **# Items** — number of line items on the order.
- **Total** — sum of (unit price × quantity) for all line items, if prices are captured.
- **Created By** and **Created** date.

You can filter by distributor or status using the column filter controls, and sort by any column. The table is exportable to CSV via the toolbar.

The **Import DigiKey Order** button in the top-right opens a dialog to pull in an existing DigiKey sales order as a new partrack order. See [Importing a DigiKey order](#importing-a-digikey-order) below.

---

## Order statuses

| Status | Meaning |
|---|---|
| **Draft** | Created from the cart; not yet sent to the distributor. You can edit line item quantities, notes, and PO number. |
| **Exported** | A CSV has been downloaded from this order (the export action transitions it automatically). |
| **Submitted** | Reserved for future distributor-API submission (not yet implemented). |
| **Placed** | Linked to a real distributor order number. Items can now be received. |
| **Partially Received** | At least one line item has been partially or fully received, but the order is not yet complete. |
| **Received** | All line items are fully received. This is a terminal status; a received order cannot be cancelled. |
| **Cancelled** | The order was cancelled before it was fully received. |

---

## The order detail page

Open an order from the list to see its full detail view. The page is divided into sections:

### Metadata card

The top card shows:

- **Status badge** — current lifecycle status.
- **Distributor** — the distributor name.
- **PO #** — your internal purchase order number. Click the pencil icon to edit it inline; press Enter or click away to save. This is separate from the distributor's order number.
- **Distributor Order #** — the external reference number assigned by the distributor (set when you mark the order as placed).
- **Created / Created by** — when the order was created and by whom.
- **Items** — count of line items.
- **Total** — order value at snapshot prices.
- **Received** — date the last item was received (once the order reaches Received status).
- **Placed** — date the external reference was linked.
- **Submission** — how the order was submitted (`api` or `manual`), if applicable.
- **Notes** — free-text notes. Click the pencil to edit inline.

### Linked builds

If this order was created by converting the cart while builds were pending, the order is linked to those builds. The **Linked Builds** section shows each linked build's project name, BOM revision, quantity, fulfillment progress (parts received vs. total), and current build status. Click a build to navigate to its detail page.

This linkage is the foundation of the allocation-aware receiving flow described below.

### Line items table

The table lists every part on the order:

| Column | Description |
|---|---|
| SKU | The distributor's stock-keeping unit (part number). Click the MPN to go to the part page. |
| Part MPN | Manufacturer part number. |
| Manufacturer | Part manufacturer. |
| Description | Part description. |
| Packaging | Packaging type (tape/reel, cut tape, tube, etc.). |
| Unit Price | Price per unit at the time the order was created (snapshot). |
| Quoted Price | Appears if a DigiKey quote has been requested; shows the confirmed price. |
| Total | Unit price × quantity ordered. |
| Ordered | Quantity ordered. |
| Received | Quantity received so far. |
| Status | Per-line receipt status: **Pending**, **Partial**, or **Received**. |

When you enter receiving mode, two additional columns appear: **Receive Qty** (editable input pre-filled with the outstanding quantity) and a **Receive** button per row. See [Receiving items](#receiving-items) below.

If a DigiKey order status has been fetched, rows where the DigiKey-confirmed quantity or price differs from the snapshot are highlighted in amber. Hover the amber cell to see the DigiKey value. Expand a row's detail panel to see per-shipment tracking information.

---

## Placing an order (the manual flow)

After converting the cart you have a Draft order. The typical workflow is:

1. **Review line items** — check quantities and prices. You can adjust line item quantities or update notes while the order is in Draft. Quantities are edited inline in the receiving-mode input or via the `PATCH /orders/{orderId}` endpoint; currently there is no dedicated edit button in the UI for quantities, so most teams simply use the Draft status as a record and proceed directly.
2. **Export CSV** — click **Export CSV** to download a spreadsheet. The file is named `order-{distributor}-{date}.csv` and contains SKU, MPN, packaging, quantity, unit price, and fixed order cost. Exporting automatically transitions the order to **Exported** status. You can re-export from Exported status; the button label changes to "Re-export CSV".
3. **Place the order on the distributor's website** using the CSV (or a DigiKey List / quote — see [DigiKey integration](#digikey-integration)).
4. **Mark as Placed** — click the **Mark as Placed** button. A text field appears where you can optionally enter the distributor's order reference number (e.g., a DigiKey sales order ID or Mouser order number). Click **Confirm Placed** to save. The order transitions to **Placed** and the external reference is recorded with a timestamp. Entering the reference is optional; you can mark an order as placed without one.

Once an order is Placed, the **Receive Items** button becomes available.

---

## Receiving items

You can receive items on any order in **Placed** or **Partially Received** status. Purchaser role required.

### Manual receiving

1. Open the order detail and click **Receive Items** (top-right). The table enters receiving mode and shows a Receive Qty input and Receive button for each line item.
2. Each input is pre-filled with the outstanding quantity (ordered minus already received). Adjust if the delivery was short.
3. Click **Receive** for each line item as you process it. You can receive items one at a time, in any order, across multiple sessions.
4. If you enter a quantity that would take the received total above the ordered quantity, a confirmation dialog appears warning you that the extra stock will still be added to inventory.
5. Once all line items are fully received, the order automatically transitions to **Received** and receiving mode closes.

### Mobile / barcode scanning

You can also receive by scanning barcodes. From a placed order's detail page, click the **Scan** button (next to Receive Items) to open the scan modal. Scan distributor barcodes to identify items and confirm quantities. See [Scanning](scanning.md) for details on the scanning workflow.

### What receiving does to stock and builds

Receiving is allocation-aware. When you record a received quantity for a line item, partrack:

1. Creates an immutable stock adjustment entry in the ledger (source: the order line item), adding the received quantity to inventory. This is recorded against the part's [inventory entry](parts-inventory.md#the-stock-ledger).
2. Checks whether this order is linked to any builds that have a pending allocation for this part.
3. If linked builds are waiting on this part, the received quantity is applied to those builds first — in the order the builds were created — until each build's allocated need is met.
4. Any quantity left after satisfying build allocations goes into general inventory.
5. For each build that received enough of all its parts, partrack automatically transitions that build from **Ordered** to **Ready**, without you needing to do anything. You will see the build's status badge update in the Linked Builds section.

This means receiving a delivery can fully unblock one or more builds in a single action. See [Builds](builds.md) for the build lifecycle.

**Prerequisite:** The part must already have an inventory entry for your team. If the part has no inventory entry, partrack cannot record the stock adjustment and receiving will fail with an error. Create an inventory entry for the part first (see [Parts and inventory](parts-inventory.md)).

---

## Editing an order

While an order is in **Draft** status, purchasers can:

- Update the **notes** field (inline pencil edit on the detail page).
- Update the **purchase order number** (inline pencil edit).
- Adjust **line item quantities** (PATCH endpoint; no dedicated UI edit button per item — the quantity in the table is read-only until receiving mode).

Once an order moves past Draft (Exported, Placed, etc.), notes and PO number remain editable, but line item quantities are locked.

---

## Cancelling an order

Click **Cancel Order** (top-right on the order detail page) and confirm. You can cancel an order in any status **except Received** — a fully-received order cannot be cancelled.

When an order is cancelled:

- The order status changes to **Cancelled**.
- Any linked builds that were in **Ordered** or **Ready** status are reverted to **Pending** — their part allocations are deleted and they re-enter the waiting state.
- The cart's BOM shortfalls are recomputed; the parts those builds still need will reappear as shortfall in the [cart](cart.md).
- Stock already received before cancellation is **not** reversed — any stock adjustments already recorded are permanent entries in the immutable ledger.

Cancellation is irreversible.

---

## DigiKey integration

When the order's distributor has DigiKey's API type configured, a **DigiKey** card appears on the order detail page. This card provides API-driven actions beyond the basic CSV export flow.

DigiKey requires **OAuth authorization** (3-legged OAuth). The first time you attempt any DigiKey API action, partrack redirects you to DigiKey to authorize access. After authorization, the token is stored for your team. If the token expires, you will be redirected again.

Configure DigiKey credentials in [Administration](administration.md#distributor-configuration-team-admins). The DigiKey card is only visible on orders where the distributor is DigiKey.

### Push to DigiKey List

Click **Push to DigiKey List** to create a **MyList** on the DigiKey website containing all line items from this order. partrack names the list `partrack-{order-id-prefix}`.

- On success, a link to the DigiKey list is displayed in the card and a browser tab opens to the list.
- If the list was already created (a list ID is stored), the button changes to **Re-push List** to update it.
- The order transitions to **Exported** status.
- If some parts could not be added (e.g., a SKU not available in your region), the push partially succeeds: the list is created and the list ID saved, but a warning is shown for the items that failed.

### Request Quote

Click **Request Quote** to create a confirmed-pricing quote on DigiKey. This is useful when you need DigiKey to lock in prices before placing the order.

- On success, a quote ID and name are stored and displayed in the card, along with a table of quoted prices, available stock, and any obsolescence/discontinuation flags per SKU.
- If a quote already exists, the button changes to **Re-request Quote**.
- The **Quoted Price** column becomes visible in the line items table, showing the confirmed price alongside the snapshot price.
- The order transitions to **Exported** status.
- DigiKey may reject a quote request if your account is not set up for quoting; the error message will describe the reason.

### Check Order Status

Once an order has an external reference (distributor order number) and is in **Placed**, **Partially Received**, or **Received** status, the **Check Order Status** button fetches live order status from DigiKey using that reference.

The DigiKey card updates with:

- **Order Status** and detail string from DigiKey.
- **Order Total**, **Currency**, **Ship Method**, shipping address, and contact.
- **Shipments** table — grouped by tracking number, with ship date, expected delivery, and SKUs in each shipment. Tracking numbers link to the carrier's tracking page where recognized.

In the line items table, expanding a row's detail panel shows that item's DigiKey-specific data: quantity shipped, backorder, reserved, individual shipment records, and scheduled release dates. Cells where DigiKey's confirmed quantity or price differs from partrack's snapshot are highlighted in amber.

### Unresolved import items

If an order was imported from DigiKey (see below) and some SKUs could not be enriched (the part catalog lookup failed), those items appear in the **Unresolved Import Items** section within the DigiKey card. Each row shows the SKU, MPN, quantity, unit price, and failure reason (API error, not in catalog, or not found).

If the order is linked to an external reference and not cancelled, a **Retry unresolved** button appears. Click it to enqueue a background job that re-attempts enrichment for each unresolved SKU. Progress is shown in real time. When complete, resolved items move to the line items table and any that still could not be resolved remain in the unresolved section.

---

## Importing a DigiKey order

If you placed an order directly on the DigiKey website (outside partrack) or want to pull an existing DigiKey sales order into partrack, use the import flow.

### Import as a new order

From the Orders list, click **Import DigiKey Order**. Enter your DigiKey sales order ID (the numeric ID from your DigiKey order confirmation). partrack enqueues a background job that:

1. Fetches the order from DigiKey's API.
2. For each line item, looks up the part in partrack by SKU. If found, reuses the existing part; if not, calls the DigiKey enrichment API to create a new part and distributor listing.
3. Creates a new partrack order in **Placed** status with the DigiKey order as its external reference.

A progress dialog shows how many items have been resolved. When complete, you are offered a link to the new order. Any SKUs that could not be resolved are listed; use **Retry unresolved** on the order's DigiKey card to re-attempt them.

If you try to import an order that has already been imported, partrack rejects the request (each DigiKey order ID can only be imported once per team).

### Link a DigiKey order to an existing partrack order

If you already have a Draft (or Exported / Submitted / Placed) partrack order and want to connect it to a real DigiKey sales order — for example because you placed the order from a CSV and now want to attach the DigiKey reference for status tracking:

1. Open the order detail page.
2. In the DigiKey card, click **Link DigiKey Order**.
3. Enter the DigiKey sales order ID and click **Link Order**.

partrack performs a pre-flight check against DigiKey's data:

- **Mismatch warning** — if more than 10% of SKUs or 10% of quantity is absent from the DigiKey order, a warning is shown with the missing SKUs. You can proceed anyway or go back.
- **Allocation conflict resolution** — if DigiKey confirmed fewer units than you ordered (e.g., items were canceled at DigiKey's end), and linked builds have allocations for those parts, a conflict resolution screen appears. For each affected build, partrack shows which parts are short and how many units DigiKey actually confirmed. You decide which builds to **Keep ordered** (they retain their allocation within the confirmed quantity) and which to **Revert to pending** (their allocation is dropped and their shortfall goes back to the cart). Partrack pre-suggests a greedy assignment (oldest builds first). After you confirm your choices, the link proceeds.

After linking:

- The order's external reference is set to the DigiKey sales order ID and the order moves to Placed status.
- Line item quantities and prices are updated to DigiKey's confirmed values.
- Any extra items in the DigiKey order that were not in the partrack order are added as new line items (a count is shown in the result dialog).
- Any SKUs that could not be enriched appear as unresolved items.

---

## Distributor integrations overview

partrack integrates with three distributors plus a test client:

| Distributor | API type | Capabilities |
|---|---|---|
| DigiKey | `digikey` | Part enrichment, listing refresh, part search, push to MyList, request quote, live order status, order import/link. Requires OAuth (3-legged). |
| Mouser | `mouser` | Part enrichment, listing refresh, part search. API key credential. |
| LCSC | `lcsc` | Part enrichment, listing refresh, part search. API key credential. |
| Test client | `testclient` | All operations; simulated responses. For evaluation and development. |

All integrations provide:

- **Part enrichment** — given a distributor SKU, fetch full product metadata (MPN, manufacturer, description, parameters, package, datasheet URL, lifecycle status) and all pricing/availability listings. Enrichment uses the SKU, not the MPN — use the distributor search first to find the right SKU if you don't already have it. See [Parts and inventory](parts-inventory.md).
- **Listing refresh** — update price breaks and stock availability for an existing listing without a full re-enrichment.
- **Part search** — keyword search returning MPNs, descriptions, primary SKUs, and prices; used when adding parts from a distributor.

DigiKey additionally implements ordering APIs (MyList creation, quoting, order status) through a separate `OrderingClient` interface. Mouser and LCSC do not currently support API ordering actions; use Export CSV for those distributors.

Setting up distributor credentials, OAuth tokens, and per-distributor cache durations is an admin task — see [Administration](administration.md#distributor-configuration-team-admins).

---

## Limitations and restrictions

- **One order per distributor per cart conversion** — partrack groups all cart items for the same distributor into a single order. You cannot split a distributor's cart items into multiple orders at conversion time.
- **Received orders are permanent** — a fully-received order cannot be cancelled. The stock adjustments are immutable ledger entries. If you received too much or the wrong items, use a manual stock adjustment (see [Parts and inventory](parts-inventory.md#adjusting-stock-manually)).
- **Over-receiving is allowed** — if you receive more than the ordered quantity, partrack asks you to confirm and then adds the excess to inventory normally.
- **DigiKey ordering APIs require OAuth** — you must authorize via DigiKey's OAuth flow before any DigiKey API ordering action. The token is team-scoped; any purchaser on the team can trigger re-authorization.
- **Direct API submission not implemented** — the **Submit Order** action (via distributor API) is not yet available. Use Export CSV + Mark as Placed instead.
- **Inventory entry required before receiving** — each part must have an existing inventory entry for your team before you can receive it. If a part on an order has no inventory entry, receiving that line item will fail. Create the entry first from the [inventory screen](parts-inventory.md).
- **Prices are snapshots** — unit prices on an order are captured at cart conversion time from the distributor listing's price break table. They are not automatically updated if the listing price changes later. Use Request Quote (DigiKey) or re-export to get current pricing.
- **Cancelling reverses build status, not stock** — cancelling an order that had already partially received stock does not reverse the stock. Only the build allocations for unreceived parts are reverted.
