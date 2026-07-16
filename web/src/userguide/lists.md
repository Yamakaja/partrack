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

Scanner integration — scanning a pile of bags *into* a list, and filtering a pile *against* one — is planned as the next iteration of lists.
