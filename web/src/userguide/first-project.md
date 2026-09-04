# Your first project

Welcome! This chapter is a guided walkthrough that takes you from a brand-new
account all the way through your first complete cycle: importing a design,
finding out what you're short, buying it, receiving it, and assembling boards
while partrack keeps your stock accurate. It ties together everything in the
rest of the guide — each step links to the chapter that goes deeper.

You don't have to do every step in order. If you're starting purely from a BOM
and don't yet track any physical stock, you can skip the storage and inventory
setup and jump straight to creating a project. The "you can skip this if…"
notes throughout will help you take the shortest path that fits you.

> **Tip:** Keep this guide open as you go. The **?** icon in the top bar opens
> the help drawer to the chapter for whatever page you're on, so you can dip in
> and out without losing your place.

## Step 1 — Get access and find your way around

Everything in partrack belongs to a **team**, so before anything else you need
to be a member of one. A **team admin** adds you from **Settings → Members** and
gives you a **role**. Which role you need depends on how far through this
tutorial you want to go:

| You want to… | Minimum role |
|---|---|
| Browse parts, projects and stock | Viewer |
| Adjust stock, scan, import BOMs, create and check out builds | Member |
| Manage the cart and place orders | Purchaser |
| Manage members or configure distributors | Admin |

For the full first-project loop you'll want at least **member**, and you'll need
a **purchaser** for the buying steps (7–8). If you can't take an action below,
your role is almost certainly the reason — see
[Roles & permissions](roles-and-permissions.md).

Once you're in, take a moment to orient yourself:

- **Sidebar** (left) — your main navigation: Dashboard, Parts, Storage, Bulk
  Scan Review, Projects, Cart, Orders, Builds, Checkouts.
- **Team switcher** (top of the sidebar) — if you belong to more than one team,
  this is how you change which one you're working in. Your role can differ per
  team, so check you're in the right one.
- **Dashboard** — your team's landing page; it summarises stock alerts, work in
  progress and recent activity.
- **Global search** — press `⌘K` (or `Ctrl-K`), or click the search bar, to jump
  to any part, project, order, or storage location from anywhere.

> **You can skip ahead if** you already know your way around — head to Step 4 to
> start a project.

## Step 2 — Set up your storage layout (recommended)

Storage is your map of where parts physically live. Setting it up early pays off
later: received parts land in a known place, scanning can drop parts straight
into a labelled bin, and assemblers get told exactly where to find each
component during a build.

- Open **Storage** from the sidebar. Click the **+** in the "Locations" panel to
  add a root location, then nest children to mirror how you actually search:
  room → cabinet → drawer → organiser.
- For dense storage (parts organisers, SMD books, reel racks), enable a **grid
  layout** so each slot is individually addressable — and printable as its own
  QR code.

Creating and editing locations needs the **member** role or higher; viewers can
browse but not change the tree. See [Storage](storage.md) for the full picture,
including grid layouts and QR codes.

> **You can skip this if** you're only checking buildability from a BOM and
> aren't tracking physical bins yet. You can always build the tree later and
> assign locations then.

## Step 3 — Seed your initial inventory (optional)

A **part** is a catalog entry; **inventory** is how much of it you actually
hold. The two are deliberately separate. If you already have components on the
shelf, get them into partrack now so your first stock check reflects reality.
You have two ways to do this:

- **Scan it in (fastest).** Pair your phone as a scanner and use the **Bulk
  scan** workflow to check in a whole order or stocktake a drawer, then review
  the batch and commit it. partrack reads distributor barcodes and DataMatrix
  labels, so it identifies the part and quantity for you. A good pattern is to
  pick the storage location first, then scan everything going into it. See
  [Scanning](scanning.md).
- **Enter it by hand.** Add or find parts on the **Parts** page, then adjust
  stock on the part's inventory entry. Quantity on hand is never edited
  directly — you add an immutable **ledger** adjustment (here, a *manual
  correction*) and the running total updates. See
  [Parts & inventory](parts-inventory.md).

Both paths need the **member** role.

> **You can skip this entirely if** you're starting purely from a BOM with no
> stock on hand yet. Your first stock check will simply show everything as a
> shortfall — which is fine, that's what the cart is for.

## Step 4 — Create a project

A **project** represents one PCB design. It holds your imported BOMs, their
revisions over time, and is the starting point for stock checks and builds.

- Open **Projects** from the sidebar and click **New Project**.
- Give it a name, then **Create Project**.

This needs the **member** role. Read more in
[Projects & BOMs](projects-bom.md).

## Step 5 — Import your BOM and work the review gate

This is the heart of partrack. Open your new project and click **Import BOM**,
then select the BOM file exported from your PCB tool.

partrack reads each line and tries to **resolve** it to a catalog part, in
order: a direct **MPN** match, a **distributor SKU** match, a **distributor API
lookup** for parts it doesn't know yet, and **generic / parametric
interpretation** for lines that describe a part by specification (e.g.
"0603 10 nF 50 V") rather than naming a manufacturer part.

Here's what to expect:

- **Auto-resolved lines** — anything partrack is confident about resolves
  silently. You don't have to touch these.
- **The review gate** — lines that need a human decision pause here. You confirm
  how each was read: which value and footprint were interpreted, which candidate
  part to use, and whether to import a **specific** part or create a **generic**
  one. Work through them one at a time.
- **AI assist** — if your deployment has it configured, partrack can suggest
  interpretations for ambiguous lines to speed up the review. It's a
  suggestion; you still confirm.

Importing needs the **member** role. For the full detail on resolution,
generic vs specific parts, and the review gate, see
[Projects & BOMs](projects-bom.md).

> **Tip:** Once you tell partrack how to read a particular line, it remembers.
> This **interpretation memory** is scoped to the project, so the same or
> similar lines resolve the same way next time — including on your next
> revision. You won't redo this work.

## Step 6 — Run a stock check

Now find out whether you can build the boards. From the project's revision,
start a **stock check**, enter a **build quantity** (how many boards you intend
to make — this is asked at check time, not stored on the project), and review
the **Stock Check Results**.

For each BOM line you'll see required quantity, quantity on hand, and a status:

| Status | What it means |
|---|---|
| **In stock** | You have enough to cover this line for the build quantity. |
| **Partial** | You have some, but not all — there's a shortfall. |
| **Out of stock** | You have none for this line. |

The **Shortfall** column tells you how many more you need, and the check also
flags any line that's dropped below the part's minimum stock level.

- **Substitutes count too.** If a BOM line has approved **substitutes**, the
  check counts those toward coverage — so an in-stock alternate can keep a board
  buildable even when the primary part is short.
- The summary at the top tells you whether the revision is **fully buildable**
  and how many items still need attention.

Stock checking needs the **member** role. More on statuses and substitutes in
[Projects & BOMs](projects-bom.md).

## Step 7 — Get shortfalls into the cart

In partrack, purchasing demand is **build-driven**. From the stock check
results, click **Create Build Plan** to turn this stock check into a build
(more on builds in Step 9). When you submit that build, its shortfalls flow
automatically into your team's **shopping cart**.

There is one cart per team, and it's persistent. The cart gathers demand from a
few sources and **merges** duplicates, so a part needed by two builds appears
once. Before you buy:

- Open **Cart** from the sidebar.
- Adjust quantities, **pick which distributor listing** to buy each part from,
  and remove anything you don't want yet. Items are grouped **by distributor**,
  because that's how the orders will come out.

> **Heads up:** Changing the cart requires the **purchaser** role. As a member
> you can open the cart and see the shortfalls your build created, but hand off
> to a purchaser to adjust quantities and convert. See [Shopping cart](cart.md).

## Step 8 — Convert to orders, then receive them

When the cart looks right, **Convert** it. This creates one **order per
distributor**. At conversion partrack also links the new orders to the builds
that drove the demand and creates soft **allocations** reserving the incoming
stock for those builds — so two builds don't quietly compete for the same parts.

Then it's over to the real world:

1. Place each order with the distributor (DigiKey, Mouser, LCSC, or whatever
   your team has configured).
2. When the parts arrive, open the order on the **Orders** page and **receive**
   its line items, recording the quantities that came in.

Receiving is **allocation-aware**: quantities reserved for a build are applied
to those builds first, and anything left over goes into general inventory. All
of it lands through the immutable stock ledger, so your counts stay auditable.
This is what lets a build that was waiting on parts move toward **ready**
without any manual reconciliation.

Converting, ordering, and receiving all need the **purchaser** role. See
[Shopping cart](cart.md) and [Orders](orders.md).

## Step 9 — Build the boards

A **build** records an actual assembly event — this project, this BOM revision,
this many boards — and it's what ultimately deducts parts from stock. You likely
already created one in Step 7; here's how it progresses:

| State | What's happening |
|---|---|
| **draft** | Created, nothing reserved — adjust quantity or notes freely. |
| **pending** | Submitted; its shortfalls now feed the cart. |
| **ordered** | Parts to cover it have been ordered; allocations reserve incoming stock. |
| **ready** | Everything required is on hand or allocated — ready to assemble. |
| **assembled** | Boards built; stock deducted through the ledger. |

- **Submit** moves a draft to **pending** and recomputes the cart (this is the
  Step 7 hand-off). A build already fully covered by stock can jump straight to
  **ready**.
- As you order and **receive** parts (Step 8), the build's allocations fill and
  it advances toward **ready** on its own. The **kitting** view shows how
  complete each build's kit is and what it's still waiting on.
- When you actually assemble the boards, **check out** the build. This deducts
  the parts it consumed (recording Build-sourced ledger entries), moves it to
  **assembled**, and lets you record the quantity actually built and resolve any
  substitute choices.

Creating and checking out builds need the **member** role. A **purchaser** can
also toggle whether a pending build contributes to cart demand — handy for a
build that's on hold. See [Builds](builds.md).

## Step 10 — Close the loop

That's one full cycle. Here's what keeps it smooth next time:

- **Re-import revised BOMs.** When your design changes, import the new BOM as a
  new **revision** of the same project. Unchanged lines reuse prior resolutions,
  so you only review what actually changed.
- **Lean on interpretation memory.** Generic and ambiguous lines you've already
  confirmed resolve the same way automatically. You can inspect or delete
  remembered interpretations from the project if a past decision needs revising.
- **Keep stock honest.** Receive orders promptly, check out builds when you
  assemble them, and use **Checkouts** for ad-hoc withdrawals (repairs,
  prototypes, samples) that aren't tied to a build. Because every change is a
  ledger entry, your quantities stay trustworthy — correct counts by adding an
  adjustment, never by overwriting.
- **Watch the dashboard.** It surfaces low-stock alerts and work in progress so
  you know when to reorder or assemble next.

Need help on any step? The **?** icon in the top bar opens this guide to the
right chapter for the page you're on, and you can browse the rest from the list
on the left. From here, the per-topic chapters —
[Parts & inventory](parts-inventory.md), [Storage](storage.md),
[Projects & BOMs](projects-bom.md), [Shopping cart](cart.md),
[Orders](orders.md), [Builds](builds.md), and [Scanning](scanning.md) — go
deeper than this walkthrough. Happy building!
