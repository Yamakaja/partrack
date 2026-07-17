# Lists

A **list** is your private scratchpad of parts. Where the [cart](cart.md) answers "what does the team need to buy" and a [build](builds.md) answers "what does this board consume", a list has **no meaning of its own** — it's just a bag of parts you're working with right now: the pile on your bench, leftovers from a cancelled prototype, things you want to check out in one go.

Lists are **personal**: only you can see your lists. They live within a team (their parts come from that team's catalog), but teammates never see them. Any team role of **member** or above can use lists.

---

## The list panel

Lists live in a **floating panel** in the bottom-right corner — not a regular page. Collapsed, it's a small pill showing the active list's name and part count. Click the pill to expand it; collapse it again with the chevron. The panel **stays with you as you navigate** — carry your list from the parts catalog to the cart to a build without losing it.

- **Create a list** from the panel header's dropdown (**+ New list**). New lists are named automatically ("Untitled list — Jul 17"); click the pencil to rename.
- **Switch lists** from the same dropdown — one list is active at a time, most recently used first.
- **Add parts** with the search box at the bottom of the panel. It searches your team's part catalog; pick a part, and focus jumps to the quantity field — type a count, or just press Enter to add the part without one. Parts not in the catalog yet must be [resolved and imported](scanning.md) first.
- **Quantities are optional.** A list is a *set of parts* first; each row can carry a count. Rows without one show a **—** and are skipped (and reported) by any operation that needs quantities. Click a quantity to edit it; clear it to remove it.
- **Notes**: hover a row for **Add note** — a free-text annotation ("half reel", "from cancelled proto").
- Adding a part that's already on the list **merges**: an add with a quantity sums onto the row; an add without one leaves the stored quantity alone.
- Hover any part number for the usual part card — description, parameters, **stock on hand and storage location**, so the list doubles as a "where do these live?" view.

## Doing things with a list

The panel's action bar offers verbs, and the set changes with the page you're on:

- **Check out** (always available) — withdraw the listed quantities from stock in one [general checkout](parts-inventory.md). A compact confirmation shows the totals ("14 parts, 1,024 pieces") and takes the checkout reason; rows without quantities are skipped. After checking out, the panel offers to **archive** the list — the physical pile is gone, so the list can sink too.
- **Add to cart** (on the Cart page, purchaser role) — push the list's quantified rows into the team cart in one step. Quantities remain editable in the cart afterwards.
- **Add from cart** (on the Cart page, purchaser role) — pull the cart's current contents *into* your list.

Operations never modify the list itself — it's a reference, not a queue.

## Managing lists

**Manage lists…** in the switcher dropdown opens a page with all your lists, including **archived** ones. Archive is the everyday way to put a finished list away (archived lists keep their contents and can be unarchived); **Delete** removes a list permanently. Neither touches parts or stock.

## Collecting with the phone

**Collect** turns the [phone scanner](scanning.md) into the fastest way to fill a list: point it at a pile of bags and each scan lands on the list — with your desktop panel tallying live as you go.

Start it from either end: the **Collect** tile on the phone menu (pick a list or create one on the spot), or the **Collect on phone** button in the list panel's action bar when your scanner is connected.

- **Quantities are a per-list choice.** The first time you collect into a list, the phone asks whether to capture piece counts; the answer is remembered for that list. With counts on, barcodes that carry a quantity (DigiKey and LCSC bags encode one) use it automatically — repeat scans of the same part add up. A barcode without a count brings up a quick number pad; type a count or **Skip** to record the part without one. partrack never invents a quantity.
- **Unknown bags don't land on the list.** A bag that isn't in your catalog shows a card offering **Send to resolve queue** — it joins the normal [bulk-scan resolution](scanning.md) flow for later review on the desktop. Once imported, scan it again.
- **Collecting never touches stock.** Unlike a regular bag scan, Collect creates no inventory entries and writes no adjustments — the list is the only thing that changes.

## Filtering a pile with the phone

**Filter Parts** is Collect's inverse: instead of scanning bags *into* a set, you scan them *against* one. A delivery of 120 bags arrives and 40 belong to a build — instead of cross-checking a list by hand, hold each bag up to the phone and read the verdict:

- ✅ **In the set** — with the quantity needed, reference designators (build targets), the order line, or your list note.
- **≈ Approved substitute** — the bag isn't the exact part, but it's an approved substitute for one that is (build targets); it counts toward coverage.
- ✗ **Not in the set** — put it on the other pile.
- **? Unknown** — the barcode couldn't be read or the part isn't in the catalog.

Filter against **a list**, **a build** (its full demand: overrides, excluded designators, substitute selections, × build quantity), or one or more **orders** (matched by the exact ordered SKUs first). Start from the phone's **Filter Parts** tile, or push a list to the phone with the **Filter** button in the list panel.

The tally counts *coverage* — how many of the target's parts you've seen — and finishing shows the real product of a session: the **"never scanned"** list, the parts that should have been in the pile but weren't. Filtering is completely read-only: nothing is written to lists, stock, or orders; only a one-line summary lands in your activity feed.

Browsing a list on the phone (the **Lists** tile) also shows each part's stock on hand and storage location — handy when the question is "where do these live?" rather than "what's in this pile?".
