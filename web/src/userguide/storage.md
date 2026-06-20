# Storage

The **Storage** page is your map of where parts physically live. Storage
locations are **hierarchical** — you can nest them as deeply as your physical
setup requires — and each inventory entry has exactly **one** preferred storage
location so there is always a single, unambiguous answer to "where is this part?"

## The location tree

The left panel of the Storage page shows your full hierarchy as an expandable
tree. Root nodes are always visible; child nodes expand and collapse with the
chevron beside them. Selecting any node opens its detail view on the right.

### Anatomy of a location

Every location has:

- **Label** — the name shown in the tree and breadcrumb (required).
- **Description** — an optional free-text note shown below the label in the
  detail view.
- **Grid layout** (optional) — turns the location into an addressable grid of
  cells rather than a plain container. See [Grid layouts](#grid-layouts) below.

A location is identified internally by a UUID, so you can rename it freely
without breaking any references.

### Two kinds of node

| Kind | Has grid config | Children allowed |
|---|:---:|:---:|
| Named / container | No | Yes |
| Grid location | Yes | No |

A grid location is a leaf node. Once you enable a grid layout, the "Add Child"
button disappears for that location — its sub-positions are the grid cells, not
separate named children. Plain named locations can have unlimited child
locations.

## Navigating the hierarchy

Click any row in the tree to open that location. The detail view shows:

- A **breadcrumb** — the full path from root to the current location; clicking
  an ancestor navigates to it.
- The optional **description**.
- The **grid visual** if the location has a grid config (see
  [Grid layouts](#grid-layouts)).
- A **Sub-locations** list of direct named children (only for non-grid
  locations); clicking a child navigates to it.
- A **Parts in this location** table — all inventory entries whose preferred
  storage location is this location *or any descendant*, paginated up to 200
  entries. Columns: MPN, description, quantity on hand, sub-location path.
  Clicking a row navigates to that part.

When a grid location is active and you have selected one or more cells, the
parts table filters to only entries whose storage coordinate matches the
selection.

### Deep-linking

Each location has a permanent URL (`/teams/{teamId}/storage/{locationId}`).
You can bookmark locations or paste the link into a browser to jump directly to
them. A grid cell can also be deep-linked by appending `?at=<flat-coordinate>`
(e.g., `?at=0,2`).

## Creating locations

**Role required: member or higher.** Viewers can browse but cannot create,
edit, or delete locations.

### Adding a root location

Click the **+** button in the "Locations" panel header. Fill in a label and an
optional description, configure a grid layout if needed, then click **Create**.
Root locations sit at the top level of the tree.

### Adding a child location

Two entry points:

1. **From the tree row** — hover over any non-grid location and click the **+**
   icon that appears. This opens the "Add Child Location" dialog with the parent
   pre-filled.
2. **From the detail view** — open the parent location and click **Add Child**
   in the top-right action bar (only shown for non-grid locations).

Labels must be unique among siblings in the same parent. If you try to create a
child with a label that already exists at that level, the request fails with a
conflict error.

## Editing a location

Open the location and click the **pencil icon** in the action bar. You can
change the label, description, and grid configuration. Rename a location freely —
the UUID link between the location and its inventory entries never changes.

Note: if you are adding a grid config to a location that already has child
locations, the UI will not show the "Add Child" option once the grid config is
enabled. Existing children remain in the database; remove them first if you want
a clean grid-only node.

## Deleting a location

Open the location and click the **trash icon**. A confirmation dialog appears.

**You cannot delete a location that has inventory entries pointing to it.** The
delete is blocked server-side with a `LOCATION_IN_USE` error, and the UI shows
"Cannot delete: inventory items are stored here." You must first move all
inventory entries to a different location (or clear their storage location
field) before deletion succeeds.

Deleting a location does not automatically delete its children — remove or
re-parent children before deleting their parent, or delete children first.
After deletion the UI navigates automatically to the parent, or to the storage
list root if no parent exists.

## Grid layouts

A grid layout turns a location into an addressable matrix of cells — useful for
things like a parts organiser with labelled compartments, a shelf with numbered
positions, or a tray with rows and columns.

### Configuring a grid

Enable the **"Enable grid layout"** checkbox when creating or editing a
location. The grid editor offers:

- **Cell labels** — choose between **Alphanumeric** (A1, B3, …) and **Numeric**
  (1, 2, …) display.
- **Levels** — one to three levels of hierarchy within the grid. Each level can
  be:
  - **1D strip** — a single row of N cells (count 1–100).
  - **2D grid** — R rows × C columns (each 1–100).
- An optional **cell name** per level (e.g. "Drawer", "Slot") that appears in
  the summary and labels.

A live **preview** on the right of the editor shows what the grid will look
like as you configure it.

Example — a three-drawer organiser where each drawer has a 4×6 grid of
compartments:

```
Level 1: 1D strip, count 3   (Drawer A, B, C)
Level 2: 2D grid, 4 rows × 6 cols   (A1 … F4 within each drawer)
```

### Using the grid in the detail view

The detail view for a grid location shows a **Layout** section with an
interactive visual of the grid. You can:

- **Drill into a level** — click a cell to navigate down to that sub-level.
- **Select individual cells** — click cells in the deepest level to highlight
  them. Drag to select a rectangle. Click a selected cell to deselect it.
  "Clear" removes the whole selection.
- **Get a QR code for a specific cell** — select exactly one cell, then click
  **QR for Cell**. The QR encodes both the location ID and the cell coordinate
  so a scanner can jump directly to it.

### Storage coordinates

A coordinate is stored as a flat array of integers on the inventory entry — one
integer per dimension per grid level traversed. A partial prefix covering a
whole number of levels is valid (e.g., you can record "Drawer B" without
specifying a compartment). The coordinate is re-interpreted against the
location's current grid config whenever it is displayed, so re-configuring the
grid dimensions changes how existing coordinates render.

## One preferred location per inventory entry

Each inventory entry points to exactly **one** preferred storage location (and
optionally a coordinate within a grid location). This is intentional: it keeps
"where is this part?" unambiguous for receiving, scanning, and kitting.

### Setting the storage location

You can set or change a part's preferred storage location from:

- **The inventory entry on the part page** — open the part, find the Inventory
  section, and edit the storage location field. The picker shows a searchable
  tree; for grid locations it also shows the grid so you can pick a specific
  cell.
- **The storage assignment scanner** — the mobile scanner's "Storage
  Assignment" mode lets you scan a location QR first, then scan part bags one
  by one to assign them to that location. Useful when physically organising
  parts into a new area. See [Scanning](scanning.md) for details.

To clear a preferred location, select "None" in the storage location picker.
The inventory entry remains; it just has no preferred location until you set
one again.

### What "one location" means in practice

If a part is split across two physical bins, choose the primary location (the
one you'd normally pull from). You can note the secondary location in the part's
description or notes field. The constraint is intentional: the system does not
model split stock — that decision stays with the people managing the
organisation.

## QR codes for locations

Every location (and every grid cell) can have a QR code printed and stuck to
the physical container. Click **QR Code** in the detail view action bar (or the
QR icon in the tree row) to open a dialog with the downloadable PNG.

QR codes for locations use a structured IPC-2D payload that encodes the
location ID and, for grid cells, the coordinate. The mobile scanner and the
storage assignment workflow both understand this format — scanning a location QR
navigates automatically to that location (and cell) without any manual
navigation.

## How storage interacts with the rest of partrack

### Inventory entries

A part's storage location is part of its inventory entry. Setting or changing
it updates the inventory entry immediately. The location is shown on the part
page and in the inventory list, and is used by kitting sessions to tell
assemblers where to find each part. See [Parts & inventory](parts-inventory.md).

### Receiving orders

When you receive an order, stock flows into inventory entries. The parts land in
whichever storage location is already set on each inventory entry — receiving
does not change the preferred location, it just records the adjustment. If a
part has no preferred location set, the received stock is counted but locationless
until you assign one.

### Scanning

The **Storage Assignment** mode in the mobile scanner is designed to assign
parts to storage locations quickly. Scan a location QR first to set the target
(including the specific cell if it's a grid location), then hold-to-scan part
bags; each scanned part is immediately assigned to that target. Auto-advance
moves the cursor to the next cell after each successful scan if you enable it.
See [Scanning](scanning.md) for the full scanning workflow.

### Kitting (build assembly)

When assembling a build, the kitting session shows the storage location and
coordinate for each part so assemblers know exactly where to find components.

### Global search

Typing in the global search bar includes storage locations in the results.

## Role requirements

| Action | Viewer | Member | Purchaser | Admin |
|---|:---:|:---:|:---:|:---:|
| View locations, browse tree, see parts | ✓ | ✓ | ✓ | ✓ |
| Create, rename, or edit a location | | ✓ | ✓ | ✓ |
| Delete a location | | ✓ | ✓ | ✓ |

See [Roles & permissions](roles-and-permissions.md) for a full breakdown.

## Tips for structuring your hierarchy

- **Match your physical search path.** Model the containers you actually open in
  sequence: room → cabinet → drawer → organiser, not a part-category taxonomy.
  When you need a part, you open the cabinet, then the drawer — make the
  hierarchy reflect that.
- **Use grid layouts for dense storage.** Parts organisers, SMD books, and
  reel racks benefit from grid layouts because you can print a cell-specific QR
  for each slot and scan directly to it during assignment.
- **Root nodes for areas or furniture.** Keep root nodes coarse: "East Wall",
  "Lab Bench", "Storage Room". Finer grain belongs lower in the tree.
- **Print QR codes before filling.** Label every shelf, drawer, and organiser
  slot with its QR before putting parts in. It is much faster to scan a label
  than to type or tap a location during a bulk assignment.
- **One canonical home.** Resist the temptation to list multiple bins. Pick the
  primary one and keep parts there. If overflow is genuinely needed, create a
  second inventory entry (not a second storage field).
- **Delete empty nodes freely.** Locations that hold no inventory entries can be
  deleted without consequence. If your physical layout changes, reorganise the
  tree to match — renaming is free, and deletion is blocked only when parts
  point to the location.
