# partrack — Parts Inventory Tracking System: User Stories

## Overview

partrack is a personal/team electronics parts inventory management system aimed at PCB designers. It bridges the gap between a PCB design workflow and the physical procurement and storage of components. Its primary focus is on a specific, curated dataset — unlike broad distributor platforms, partrack is tailored to the needs of a hardware developer managing their own inventory.

---

## Actors

- **User** — A PCB designer or hardware developer who manages their own component inventory.
- **Distributor API** — An external API provided by an electronics distributor (e.g. DigiKey, Mouser, LCSC) used to enrich part data and retrieve pricing/availability.

---

## Epic 1: BOM Import

### US-1.1 — Import a BOM from a PCB design tool
> As a User, I want to import a Bill of Materials (BOM) exported from my PCB design tool, so that I don't have to manually enter every part needed for my project.

**Acceptance criteria:**
- The system accepts common BOM formats (e.g. CSV, with configurable column mapping).
- Each BOM line item is associated with a project.
- Duplicate parts across BOM line items (same part number) are detected and consolidated for stock checking.
- After import, the system automatically attempts to resolve each line item to an existing Part (by MPN or distributor SKU), or creates a new Part by querying distributor APIs if a valid MPN is present.

### US-1.2 — Associate a BOM with a project
> As a User, I want each imported BOM to be linked to a named project, so that I can track which parts are needed for which design.

**Acceptance criteria:**
- A project has a name, optional description, and an associated BOM.
- Multiple BOMs (design revisions) can be associated with a single project.
- The active BOM revision for a project drives its stock requirements.

### US-1.3 — Review and resolve BOM line items
> As a User, after importing a BOM, I want to review each line item and confirm or manually set the component link, so that all parts are correctly resolved before I check stock or create orders.

**Acceptance criteria:**
- The system presents a review view showing each BOM line item alongside its auto-resolution result: resolved (with the matched Part), auto-created (Part was imported from a distributor API), or unresolved (no match found).
- For unresolved items, the user can search the Part catalog or a distributor API and link the line item to an existing or newly created Part.
- The user can correct an incorrect auto-resolution at any time.
- The user can approve additional Parts as valid substitutes for any line item.
- Once all line items are resolved (or explicitly marked as not applicable), the BOM revision is considered ready for stock checking.

---

## Epic 2: Stock Visibility

### US-2.1 — View stock status for a BOM
> As a User, I want to see which parts from my BOM are currently in stock and which are not, so that I know what I need to order before I can build my project.

**Acceptance criteria:**
- The user specifies a build quantity (number of boards to assemble) when opening the stock check view. This quantity is ephemeral — it is a parameter to the check, not stored on the project.
- For each BOM line item, the system displays: required quantity (per-board quantity × build quantity), quantity in stock, and shortfall (if any).
- Parts are categorised as: **In Stock**, **Partial** (some but not enough), or **Out of Stock**.
- Approved substitute parts are included in the available stock calculation.

### US-2.4 — Add BOM shortfalls to the shopping cart
> As a User, after checking stock for my BOM, I want to add all shortfalls to the shopping cart in one action, so that I can quickly build an order for the missing parts.

**Acceptance criteria:**
- A single action adds all shortfall quantities to the cart.
- Parts that already have a preferred Distributor Listing set will be added against that listing; others are added without a listing assignment and must be resolved in the cart.
- If a shortfall item is already in the cart, the quantities are merged rather than duplicated.
- Minimum reorder quantities (from Inventory Entry, if set) are respected: the quantity added to the cart is at least `max(shortfall, reorder_quantity)`.

### US-2.2 — View the global inventory
> As a User, I want to browse my entire parts inventory, so that I can see what components I have on hand without reference to a specific project.

**Acceptance criteria:**
- Inventory can be searched and filtered by part number, description, category, package, value, and manufacturer.
- Each inventory entry shows quantity on hand, storage location (if set), and a summary of key parameters.

### US-2.3 — Find suitable inventory parts for a project
> As a User, I want to see whether any existing inventory parts are suitable substitutes for a BOM line item, so that I can avoid unnecessary orders.

**Acceptance criteria:**
- The system can suggest inventory parts that match key parameters (e.g. value, package, voltage rating) for a given BOM component.
- The User can manually approve a substitution for a specific BOM line item.

---

## Epic 3: Supplier & Distributor Management

### US-3.1 — View supplier options for a part
> As a User, I want to see which distributors carry a specific part and at what price/availability, so that I can make informed purchasing decisions.

**Acceptance criteria:**
- Supplier data (distributor name, part number, unit price, stock, minimum order quantity, packaging) is stored per part.
- Supplier data can be refreshed on demand via Distributor APIs.

### US-3.2 — Manage distributor integrations
> As a User, I want to configure which distributor APIs are active and provide my API credentials, so that the system can retrieve live pricing and availability data.

**Acceptance criteria:**
- Supported distributors can be enabled or disabled.
- API credentials (keys, tokens) are stored securely in local configuration.
- The system reports clearly if a distributor API call fails.

---

## Epic 4: Shopping Cart & Order Management

### US-4.1 — Manage the shopping cart
> As a User, I want a persistent shopping cart where I can collect parts I need to order from various sources, so that I can review and adjust everything before placing any orders.

**Acceptance criteria:**
- The cart persists across sessions.
- Each cart line item shows: part, quantity, optional assigned Distributor Listing (SKU, unit price, packaging), and the source of the item (BOM shortfall, inventory maintenance, or manual addition).
- The user can add items manually, adjust quantities, remove items, and assign or change the Distributor Listing for each item.
- The cart calculates a total cost per distributor (unit price × quantity + fixed order costs) based on assigned listings.

### US-4.2 — Convert the cart to draft orders
> As a User, I want to convert my cart contents into one or more draft orders grouped by distributor, so that I can review them before deciding how to submit each one.

**Acceptance criteria:**
- Cart items without an assigned Distributor Listing must be resolved before conversion.
- The system groups cart items by distributor and creates one Order per distributor, each starting in **Draft** status.
- The draft order displays: line items with distributor SKU, quantity, unit price, packaging type, fixed order cost, and a total cost summary.
- Once converted, the corresponding cart items are removed from the cart.
- The user can adjust quantities or remove line items from a draft order before submitting.

### US-4.3 — Export an order as CSV for manual placement
> As a User, I want to export a draft order as a CSV file, so that I can place it through my organisation's procurement process or directly on the distributor's website without using an API integration.

**Acceptance criteria:**
- The export contains at minimum: distributor SKU, quantity, unit price, and any other fields required by the specific distributor's upload format.
- The order moves to **Exported** status after the CSV is generated, indicating that the user has taken responsibility for placing it externally.
- The order remains in Exported status until the user provides the distributor's order reference or marks it as Placed.

### US-4.4 — Submit an order via distributor API
> As a User, I want to submit a draft order directly to a distributor through their API, so that I don't have to leave the system to place the order.

**Acceptance criteria:**
- Available only for distributors with an enabled API integration that supports order submission.
- On successful submission, the distributor's order reference number is automatically recorded against the Order.
- The order moves to **Placed** status and API-based status tracking is enabled.
- If the API submission fails, the order remains in Draft status and the error is reported clearly.

### US-4.5 — Link a manually-placed order to the system
> As a User, after placing an order through an external process (website, phone, procurement system), I want to record the distributor's order reference in the system, so that I can track its status and eventually receive it.

**Acceptance criteria:**
- The user can enter the distributor's order reference number against any order in Draft or Exported status.
- On entry, the order moves to **Placed** status.
- If an API integration is available for the distributor, the system can begin polling for shipment status and estimated delivery once the reference is linked.
- Parts in a Placed order are shown as **On Order** in stock views.

### US-4.6 — Receive an order into inventory
> As a User, I want to mark an order (or individual line items) as received, so that the parts are added to my inventory automatically.

**Acceptance criteria:**
- Individual order line items can be received independently; the user confirms the quantity actually received (which may be less than ordered).
- Receiving a line item increases the Inventory Entry quantity for the corresponding part.
- Received parts are enriched with detailed data pulled from the distributor API (datasheet links, parameters, lifecycle status, etc.).
- When all line items are fully received, the order is automatically marked as **Received**.
- Partial receipt leaves the order in **Partially Received** status; the remaining items remain On Order.

---

## Epic 5: Part Data Enrichment

### US-5.1 — Enrich a part with distributor data
> As a User, I want the system to automatically pull detailed part information from distributor APIs when I receive parts, so that my inventory has rich, accurate component data without manual entry.

**Acceptance criteria:**
- Enriched data includes: manufacturer part number, manufacturer name, description, datasheet URL, package/footprint, key electrical parameters, lifecycle status.
- Data is attributed to its source distributor and timestamped.
- The User can trigger a manual re-enrichment for any part.

### US-5.2 — View detailed part information
> As a User, I want to view all stored information for a part in my inventory, so that I can evaluate whether it is suitable for a design without leaving the system.

**Acceptance criteria:**
- The detail view shows all enriched parameters, supplier listings, quantity on hand, storage location, and which projects reference this part.

---

## Epic 6: Inventory Maintenance

### US-6.1 — Manually add or edit a part in inventory
> As a User, I want to manually add a part to inventory or edit an existing entry, so that I can handle parts not sourced through tracked orders (e.g. salvaged components, samples).

**Acceptance criteria:**
- A part can be added with at minimum a part number and quantity.
- All fields editable via the UI.

### US-6.2 — Record a storage location for a part
> As a User, I want to assign a physical storage location to each inventory part (e.g. drawer, bin label), so that I can quickly find components in my workspace.

**Acceptance criteria:**
- Storage location is a free-form label or a structured identifier (e.g. `A3`, `Shelf-2/Bin-5`).
- Inventory search can filter by storage location.

### US-6.3 — Adjust stock quantities manually
> As a User, I want to manually adjust the quantity of a part, so that I can correct the inventory after using parts in a build or discovering a counting error.

**Acceptance criteria:**
- An adjustment records the delta, a timestamp, and an optional reason note.
- Adjustment history is viewable per part.

### US-6.4 — Set minimum stock levels and reorder quantities
> As a User, I want to set a minimum stock level and a standard reorder quantity for parts I always want to keep on hand, so that the system can help me maintain a healthy stock of common components.

**Acceptance criteria:**
- Each Inventory Entry can have an optional minimum stock quantity and an optional reorder quantity.
- These values are visible in the inventory browser and on the part detail view.

### US-6.5 — Replenish low-stock parts via the cart
> As a User, I want to check my entire inventory against minimum stock levels and add all low-stock parts to the cart at once, so that I can efficiently reorder consumables without reviewing my BOM.

**Acceptance criteria:**
- A single action scans all Inventory Entries that have a minimum stock quantity set.
- Any part whose quantity on hand falls below its minimum stock level is added to the cart.
- The quantity added is the reorder quantity if set; otherwise it is the difference between the minimum stock level and the current quantity on hand.
- Parts already in the cart are merged rather than duplicated.
- The user can preview the list before it is added to the cart.

---

## Epic 7: Build & Checkout

### US-7.1 — Record a PCB build
> As a User, I want to record that I have assembled N boards from a project, so that the components consumed are automatically deducted from my inventory.

**Acceptance criteria:**
- The user specifies a project, the active BOM revision, and the number of boards built.
- The system computes the total consumption for each BOM line item (per-board quantity × boards built) and presents it for confirmation.
- If substitutes are in play for a line item, the user specifies which Part's stock to deduct.
- On confirmation, stock is deducted and the build is recorded as an event with a timestamp and optional notes.
- The adjustment history for each affected part references the build event.

### US-7.2 — Check out parts for general use
> As a User, I want to record parts being taken from inventory for purposes not tied to a specific project (e.g. prototyping, repair, samples), so that my inventory stays accurate.

**Acceptance criteria:**
- The user specifies one or more parts and the quantities being taken.
- An optional reason or description can be recorded.
- Stock is deducted and a checkout event is recorded with a timestamp.
- The adjustment history for each affected part references the checkout event.
