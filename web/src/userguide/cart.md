# Shopping cart

The **shopping cart** is your team's persistent staging area for everything you intend to buy. There is exactly **one cart per team**, shared by the whole team. It persists indefinitely — nothing clears it except explicitly removing items, clicking **Clear cart**, or converting it to orders.

Everyone on the team can open the cart. Changing it — adding, editing quantities, refreshing, converting — requires the **purchaser** role; below that the page is read-only. See [Roles & permissions](roles-and-permissions.md).

---

## How items get into the cart

Items enter the cart from three distinct sources, each tracked separately.

### BOM shortfalls (build-driven demand)

When you submit a [build plan](builds.md) to pending status, partrack immediately recomputes the team's BOM shortfalls. The algorithm:

1. Collects every pending build whose **Include in cart** checkbox is enabled (`excluded_from_cart = false`).
2. Aggregates the total quantity needed per resolved part across all those builds, accounting for substitute selections, per-build designator exclusions, and per-build line-item overrides (`skip` or `use_available` lines are excluded from demand).
3. Subtracts current on-hand inventory **plus** any unallocated inbound quantity from existing open orders, so parts already on the way are not re-ordered.
4. If the post-build quantity would fall below a part's minimum stock level, the cart quantity is set to the part's reorder quantity (or the shortfall amount if no reorder quantity is configured). If there is simply not enough stock, the cart quantity is the raw shortfall.

The result is a set of `bom_shortfall` cart items. These items are **replaced entirely** every time shortfalls are recomputed — they are not additive. Shortfalls are recomputed automatically whenever:

- a build is submitted (draft → pending),
- a pending build's quantity or designator exclusions change,
- a pending build is cancelled,
- an order linked to pending builds is cancelled (builds revert to pending and shortfalls are refreshed), or
- a DigiKey order import is completed and received quantities update inventory.

Because multiple pending builds can demand the same part, a single `bom_shortfall` cart item covers combined demand across all included builds. The item detail panel shows a breakdown by project and revision so you can see exactly where the demand comes from.

### Inventory maintenance (low-stock replenishment)

Click **Add Low Stock** in the cart toolbar to add all parts that are currently below their minimum stock level. The quantity added for each part is the configured reorder quantity (if set) or the shortfall against the minimum. These items are tagged with source `inventory_maintenance`.

You can also preview which parts are below minimum stock without modifying the cart by going to the Inventory page. See [Parts & inventory](parts-inventory.md).

### Manual additions

Add any part to the cart directly from its part detail page using the **Add to cart** button. You can specify the quantity and optionally pin a specific distributor listing. These items are tagged `manual`.

You can also add a part to the cart from the cart page itself via the API if you know the part ID, though the UI path from the part detail page is the intended workflow.

### Merging rules

The cart never duplicates entries for the same part. When a part already has a `bom_shortfall` or `inventory_maintenance` item and you add a manual item for the same part, the two coexist as **separate records** internally. The cart UI shows them together as a single row: the demand-floor quantity (from the shortfall or maintenance item) is shown, and any manual excess on top is reflected in the editable quantity field and labelled "Manual" in the order breakdown. This design ensures the demand floor can never be accidentally removed by an edit.

When items from multiple sources share the same part, the listing chosen on the shortfall/maintenance item is used for the combined row.

---

## The cart page

Navigate to **Shopping Cart** in the team sidebar. The page has three sections.

### Pending builds

A table of all pending builds in the team, regardless of whether they currently have shortfalls. Each row shows:

| Column | What it shows |
|---|---|
| Include | Checkbox to toggle whether this build contributes to cart demand |
| Project | Project name |
| Revision | BOM revision label |
| Quantity | Number of boards planned |
| Est. Order Cost | Estimated cost based on current cart prices for this build's parts |

Unchecking **Include** for a build excludes it from demand and triggers a shortfall recompute so the cart immediately reflects the reduced need. Re-checking it recomputes again and adds its demand back. Requires the purchaser role.

If there are no pending builds, the section prompts you to create a build plan. The cart can still contain inventory-maintenance and manual items even with no pending builds.

### Order summary

A table grouped by distributor showing the number of line positions, total part count, parts subtotal, any fixed order cost (e.g. shipping), and order total for each distributor. A grand total row appears when there are two or more distributors.

Items with no assigned listing appear in an **Unassigned** row with a warning badge. These items cannot be converted to orders until a listing is assigned or the items are removed.

The fixed order cost shown is the highest fixed cost across all items in a distributor group (reflecting that a single shipping fee covers the whole order, not one fee per line item).

### Order details

A table of items grouped by distributor. Each row shows:

| Column | What it shows |
|---|---|
| Part MPN | Manufacturer part number |
| Description | Short description |
| SKU | Distributor SKU for the chosen listing |
| Order Qty | Effective quantity to order (after MOQ and price-break optimisation) |
| Distributor Stock | Cached stock available at the distributor (amber if below order qty, red if zero) |
| Unit Price | Effective unit price at the order quantity |
| Subtotal | Unit price × order quantity |

Click any row to expand a detail panel showing part specs, listing details (distributor, SKU, packaging, MOQ, stock, fixed cost, last refresh time), price breaks, and an order breakdown explaining how the final quantity was derived.

---

## Quantities and the demand floor

### Editing quantities

You can change the quantity of any cart item by editing the number field in the Order Qty column and pressing Enter or clicking away.

For `bom_shortfall` and `inventory_maintenance` items there is a **demand floor**: you cannot set the quantity below the computed shortfall or maintenance quantity. The floor is enforced by the server and the UI. If you want to order more than the minimum, type a higher number — the excess is tracked as a manual companion item behind the scenes and displayed together in the row.

For purely manual items the floor is 1; you can set any positive integer.

### MOQ enforcement

If the chosen listing has a minimum order quantity (MOQ) higher than the demand quantity, the effective order quantity is automatically raised to the MOQ. The item row shows the MOQ in the quantity hint area, and the expanded detail panel flags it in red if the combined demand is below the MOQ.

### Price-break optimisation

When listing price data is available, partrack automatically checks whether ordering up to a higher price break actually reduces the total cost (unit price × quantity + fixed cost). If a higher break quantity results in lower total spend, the order quantity is raised to that break quantity and the row shows a green "↑ N (price break)" hint. The optimised quantity is capped at 100× the cart demand quantity to avoid excessive over-ordering. You can still manually override by typing a quantity in the field.

---

## Choosing a distributor listing

Each cart item needs a distributor listing before it can be converted to an order. Listings are resolved in two ways:

- **Auto-resolved**: if the item has no explicitly chosen listing, partrack picks the cheapest available listing among the part's enabled distributor listings, factoring in price breaks, MOQ, and fixed order costs. Tie-breaks: preferred distributor wins; then alphabetical by distributor name. Auto-resolved items show `chosen: false` and are marked accordingly in the UI.
- **Explicitly chosen**: you can pin a specific listing when adding a manual item from the part detail page (select a listing from the table and use the "Add to cart" button on that row). Explicitly chosen listings are marked `chosen: true`.

You can also update the listing on an existing cart item. Items with no listing at all (neither auto-resolvable nor explicitly set) appear in the **Unassigned** group in the order summary and details, with a warning badge. The cart cannot be converted while any item has no listing — you must either assign a listing or remove those items.

Listings are looked up from the part's distributor listings that are enabled for the team. If a part has no listings or none are enabled, you will need to enrich the part first — see [Parts & inventory](parts-inventory.md).

---

## Refreshing prices and stock

Distributor listing data (price breaks, stock availability) is cached and may be stale. Before converting, click **Refresh Stock & Prices** in the cart toolbar. This button is visible when at least one cart item has a listing attached.

The refresh runs as a background job. A progress indicator tracks each listing as it is refreshed. When complete, a summary toast shows how many listings were updated, how many are now unavailable, and how many failed. The cart automatically reloads when the job finishes.

Listings are refreshed per-item: for items with an explicit listing, only that listing is refreshed; for auto-resolved items, all eligible listings for the part are refreshed so the auto-resolution can pick the best current option.

A listing is considered **unavailable** when the distributor API cannot find it or reports zero stock. Unavailable items are not automatically removed from the cart — you can still order them if you choose to, but the stock column will show 0 and the quantity may be highlighted in amber.

If no cart items have a distributor integration configured (e.g. all are from a distributor without API access), the refresh button returns a "nothing to refresh" notice.

---

## Removing items

- **Remove a single item**: click the trash icon on the item row in Order Details. For shortfall/maintenance items, removing the item does not suppress the underlying demand — the next time shortfalls are recomputed (e.g. on the next build submit), the item will return if the shortfall still exists.
- **Clear the entire cart**: use the **Clear cart** action (if present). This removes all items including shortfall and maintenance items. Shortfall items will return when shortfalls are recomputed.

Removing a bom_shortfall item is not the right way to defer a build's purchasing — instead, uncheck the build's **Include** checkbox in the Pending Builds section to exclude it from demand without losing the build record.

---

## Converting the cart to orders

When you are satisfied with the cart contents, click **Convert to Orders**. This button is visible when the cart has at least one item.

Convert performs the following atomically in a single database transaction:

1. **Validates** that the cart is not empty and that every item has a listing (either explicit or auto-resolved). If any item has no listing, the conversion fails with an error — resolve the missing listings first.
2. **Creates one draft order per distributor**, grouping all items that belong to that distributor. Each order line item records the listing, quantity, a unit price snapshot, and a fixed order cost snapshot.
3. **Clears the cart** completely.
4. **Links every pending build** (those that contributed to demand) to all newly created orders via a build-to-order link table.
5. **Creates soft allocations** (`build_part_allocations`) for each pending build, recording:
   - `quantity_needed` — the total needed for that build
   - `quantity_reserved` — how much is being reserved from current on-hand stock (capped by available free stock after accounting for other reservations)
   - `quantity_received` — starts at 0; updated as ordered items are received
6. If new orders do not fully cover a build's needs, the system also looks for unallocated quantity in existing open orders for the same parts and draws from those as well, linking the build to those orders.
7. **Transitions build status**:
   - If all parts for a build are covered entirely by current on-hand reservations (no orders needed), the build moves to **ready**.
   - Otherwise it moves to **ordered**.

After a successful conversion, if a single order was created you are navigated directly to that order's detail page. If multiple orders were created, you are navigated to the orders list. See [Orders](orders.md) for what to do next.

### Conversion error cases

| Condition | Error |
|---|---|
| Cart is empty | Conversion refused; add items first |
| One or more items have no listing (neither chosen nor auto-resolvable) | Conversion refused; assign listings or remove those items |

There is no partial conversion — the whole cart converts or nothing does.

---

## Live updates

The cart is a shared team resource. If another team member with the purchaser role modifies the cart while you are viewing it, your browser receives a `cart_changed` SSE event and the cart data is refreshed automatically. You will not see a notification, but the displayed quantities and items will update.

Similarly, when a build is submitted or cancelled by anyone on the team, the BOM shortfall items update automatically and a `cart_changed` event is broadcast to all cart viewers.

---

## Limitations and caveats

- **One cart per team.** There is no per-user cart or per-project staging area. The whole team shares a single cart.
- **Shortfall items are recomputed, not additive.** Each recomputation replaces all `bom_shortfall` items. Edits you have made to shortfall item quantities (adding manual excess above the floor) are preserved via the companion-item mechanism, but if the underlying demand disappears (e.g. a build is cancelled), those items are removed.
- **Manual items survive recomputation.** Items added manually that are not companions to a shortfall item persist through shortfall recomputes unless you remove them.
- **The cart does not reserve stock.** Items in the cart are not reserved — stock reservations (soft allocations) are only created at the moment you convert to orders. Two purchasers could simultaneously convert the cart and both succeed, each creating separate orders for the same parts.
- **Price/stock data may be stale.** Always refresh listings before converting if accurate pricing or availability matters.
- **Distributor without API integration.** If a distributor is not connected (no DigiKey/Mouser/LCSC credentials), its listings still appear in the cart and can be converted to orders, but the Refresh Stock & Prices action cannot update them.
- **Purchaser role required to change it.** Reading the cart is open to every role, so a member can watch the shortfall their build created land here; adding, editing, refreshing, and converting require the purchaser role.

---

## Related pages

- [Projects & BOM](projects-bom.md) — running a stock check, resolving BOM line items, and how shortfalls originate
- [Builds](builds.md) — the build lifecycle and how submitting a build drives cart demand
- [Orders](orders.md) — what happens to orders after conversion
- [Parts & inventory](parts-inventory.md) — enriching parts, distributor listings, and minimum stock settings
- [Roles & permissions](roles-and-permissions.md) — who can view and modify the cart
