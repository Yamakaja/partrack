# Projects & BOMs

This is the heart of partrack. A **project** is one PCB design; its **bill of
materials (BOM)** lists every component the board needs. This chapter walks you
through creating a project, importing a BOM, getting every line **resolved** to a
catalog part (including the smart parts of that process — generic/parametric
matching, the review gate, and the optional AI assistant), checking whether you
have the stock to build, and managing approved substitutes.

If you are brand new, read [Getting Started](getting-started.md) and
[Your First Project](first-project.md) first — they give the quick tour. This
chapter is the deep reference.

## Projects

A project groups everything about one board design:

- its **BOM revisions** (the design changes over time — Rev A, Rev B, …),
- the parts those BOMs resolve to,
- and the starting point for **stock checks** and **builds**.

### Creating a project

From your team's **Projects** list, create a project. You can set:

- **Name** (required).
- **Description** — free text.
- **Preferred distributor** — a hint used later when ordering.
- **Source URL** — a link back to the design (e.g. a repository).
- **Project manager** — a team member who owns the project. Their name shows on
  the project page; it is informational and does not change anyone's permissions.

A project starts empty — no BOM yet. The next step is importing one.

> **Who can do what.** Viewing projects, revisions, stock checks, and the
> interpretation-memory page needs the **viewer** role. Creating a project,
> importing a BOM, resolving line items, and editing substitutes need the
> **member** role. See [Roles & Permissions](roles-and-permissions.md).

## BOM revisions

Every time you import a BOM you create a **revision** — a labelled snapshot of
the parts list at that point in the design. Revisions are never overwritten:
re-importing a tweaked BOM adds a new revision and keeps the old one, so you can
always stock-check or build against the exact version you are assembling.

One revision is the project's **active** revision (the import wizard marks each
new import active by default). The active revision is what the project page
surfaces first, but every revision remains available from the project's revision
list.

Each line in a revision is a **BOM line item**: a set of reference designators
(e.g. `R1, R2, R5`), a quantity (placements per board), and whatever identifying
or describing fields your BOM carried (MPN, manufacturer, SKU, distributor,
value, footprint, description).

## Importing a BOM

Open a project and choose **Import BOM**, then pick a CSV file exported from your
PCB/EDA tool. The importer is a **five-step wizard**:

1. **Parse** — read the file into rows.
2. **Map Columns** — tell partrack what each column means.
3. **Map Distributors** — only when needed (see below).
4. **Interpret** — review how partrack read each row.
5. **Details** — name the revision and confirm.

partrack remembers the parse settings and column mapping you used last time (per
project), so repeat imports of the same export format need almost no setup.

### Step 1 — Parse

partrack accepts **CSV** files (`.csv`). On this step you adjust how the file is
split into rows and columns:

- **Delimiter** — comma, semicolon, tab, etc. (auto-detected by default).
- **Quote character** and **escape character**.
- **Has header row** — whether the first row is column names.
- **Comment marker** — lines starting with this are skipped (e.g. KiCad's `#`).
- **Skip first N lines** — drop leading metadata/banner rows some tools emit.

A live preview of the first rows updates as you change options. Get the preview
looking like a clean table, then continue.

### Step 2 — Map Columns

Assign each column a **role** from a dropdown. The roles are:

| Role | What it is | Used for |
|---|---|---|
| **Designators** \* | Reference designators (`R1, R2`) | Required. Implies the component class and the per-line placement count. |
| **Quantity** \* | Placements per board | Required. |
| **MPN** | Manufacturer part number | Identity resolution. |
| **Manufacturer** | Manufacturer name | Disambiguates an MPN shared across makers. |
| **SKU** | Distributor SKU | Identity resolution (with a distributor). |
| **Distributor** | Distributor name | Pairs with the SKU column. |
| **Value** | EDA value field (`10k`, `100nF X7R`, `4k7`) | Generic/parametric interpretation. |
| **Footprint** | EDA footprint/package (`R_0805_2012Metric`, `0603`) | Generic/parametric interpretation. |
| **Name** | A part name | Metadata. |
| **Description** | Free-text description | Kept verbatim for traceability. |
| **— Ignore —** | Skip this column | — |

\* **Designators** and **Quantity** must each be mapped exactly **once** — the
wizard will not let you continue otherwise. Common header names are
auto-detected (e.g. `Ref`, `Qty`, `MPN`, `Value`, `Footprint`, `Package`), so
you usually only fine-tune.

**Two worlds of BOM lines.** A line can name the part it needs in two ways, and
the roles you map declare which:

- **By identity** — an MPN (and manufacturer) or a distributor SKU. This names
  *one specific catalog part*. The production/manufacturing workflow.
- **Parametrically** — a value and a footprint (`0603` + `10nF`). This names a
  *class* of acceptable parts ("any 0603 10nF cap"). The prototyping workflow.
  Map **Value** and **Footprint** for this; you do not need an MPN.

A line can carry both — an MPN *and* a value. Identity wins (the MPN resolves the
line) and the value is parsed alongside as a cross-check.

> **Value vs. Description.** They are different. **Value** is the terse,
> class-aware spec (`10k`, `100nF X7R`) that drives interpretation. **Description**
> is verbatim free text kept only for reference. Map both if your BOM has both.

### Step 3 — Map Distributors

This step appears only when distributor/SKU information is present:

- If your BOM has a **Distributor** column, map each distinct distributor *name*
  found in the file to one of your team's known distributors.
- If you mapped a **SKU** column but no distributor column, you are asked to pick
  **one** distributor that all the SKUs belong to.
- If there is nothing to map, the wizard skips straight to **Interpret**.

### Step 4 — Interpret (the review gate)

This is the key idea that makes BOM import reliable. Before partrack touches your
parts catalog, it shows you **how it read every row** and lets you fix anything
wrong. Misreads are caught here, where they are cheap, instead of after a wrong
part has been linked.

Each row is read into a small **interpretation**:

- a **route** — shown as a badge:
  - **Specific** — an identity line (MPN/SKU), resolved against the catalog;
  - **Generic** — a parametric line (value + footprint), matched or turned into a
    generic part;
  - **Ignored** — a do-not-populate / virtual / mechanical line (e.g. `DNP`,
    fiducials, mounting holes, test points);
  - **Review** — partrack could not read it confidently; it needs your eyes.
- a **category** (e.g. Resistors), inferred from the reference designator;
- a **package** and **mounting** (SMD/THT), normalised from the footprint;
- the extracted **parameters** (e.g. `resistance = 10 kΩ`, `tolerance = 1%`),
  shown in human units;
- **flags** for anything notable (e.g. a class/footprint mismatch).

The list is **dense and sorted attention-first**: problematic, low-confidence,
and AI-read rows float to the top; confident rows sit below to scroll through and
spot-check. Nothing is hidden — even a confident read can be wrong, so a quick
scan is worth it.

You can:

- **Edit** any field — flip the route, fix the category/package, correct a value,
  add or remove a parameter. Editing a row's source Value/Footprint re-parses it
  on the spot.
- Leave it alone — most rows need no changes.

When you edit a row, your correction is marked **edited** and is persisted as a
deliberate decision (and remembered — see [Interpretation memory](#interpretation-memory)).

> Step 4 only **reviews how rows were read.** It does not yet create or link
> parts — that happens after the import, during resolution. You will get a second,
> narrower review (the **BOM Review** dialog) for any line that needs a catalog
> decision.

### Step 5 — Details

Give the revision a **label** (required, e.g. "Rev B") and optional **notes**,
then confirm. partrack creates the revision, saves your parse + column mapping to
the project for next time, marks the revision active, and kicks off **resolution**
in the background. You land on the revision page and watch progress live.

## How lines get resolved (resolution priority)

After import, a background job tries to link each line to a catalog **part**. It
walks a fixed priority order and stops at the first tier that produces a confident
single match. Tiers that find *several* candidates mark the line **ambiguous** for
you to pick; tiers that find nothing fall through to the next.

| # | Tier | Condition | Result on a single match |
|---|---|---|---|
| 1 | **Distributor SKU** | Line has a SKU **and** a resolved distributor | `Auto (SKU)` |
| 2 | **MPN + Manufacturer** | Both present; exact catalog match | `Auto (MPN)` |
| 3 | **MPN only** | MPN present; exact catalog match | `Auto (MPN)` |
| 4 | **Prior revision** | A previous revision of *this project* resolved the same line (by MPN, by MPN+mfr, by SKU, or by an identical designators+value+footprint signature) | `Auto (Prior)` |
| 5 | **Generic / parametric match** | The line was read as a generic spec; a single existing catalog part satisfies it (category + package + value) | `Auto (Generic)` |
| 6 | **Fuzzy local MPN** | A single local part's MPN/alias *contains* the BOM MPN (a packaged variant) | `Auto (MPN ?)` — resolved but flagged for a one-click confirm |
| 7 | **Distributor API import** | Line has usable MPN/SKU but no local match; a background lookup fans out across your enabled distributors | Auto-imported if unambiguous; otherwise surfaced to import |

Notes and gotchas:

- **The order matters.** Identity (SKU, then MPN) is tried before the
  prior-revision shortcut, which is tried before generic matching, which is tried
  before the fuzzy and distributor-import tiers. An exact identity match always
  wins over a looser one.
- **Multiple matches at tiers 2, 3, 5, or 6 → `Needs review`** (`ambiguous`). The
  line stays unresolved and you choose in the BOM Review dialog.
- **Generic matching never creates a part.** Tier 5 only *links* to a part that
  already exists. If nothing matches, the line stays unresolved with an explicit
  *Create generic part* action — you decide whether to add it.
- **`Auto (MPN ?)`** (fuzzy) is resolved but deliberately surfaced for a confirm,
  because a substring match is less certain than an exact one.
- **A resolved line gets an inventory entry.** When a line resolves, partrack
  ensures your team has an inventory entry (at quantity 0) for that part, so it is
  ready to track stock. See [Parts & Inventory](parts-inventory.md).
- **Substitutes carry forward.** On import, any approved substitutes from the
  immediately prior revision are copied onto matching new-revision lines.

## Generic / parametric matching

When a line is read as a **Generic** spec (a value + footprint, no specific MPN),
partrack matches it against the catalog by *meaning*, not by part number. This is
what lets "0603 10nF" find whatever 0603 10nF cap is already in your library.

It works on a **canonical package** plus parameters:

- **Package** is normalised so messy EDA strings and clean codes compare equally.
  `R_0805_2012Metric`, `0805`, and the distributor form `"0805 (2012 Metric)"`
  all canonicalise to imperial **0805**. (The importer correctly ignores the
  *metric twin* embedded in EDA strings — `R_0201_0603Metric` is imperial **0201**,
  not 0603.)
- The **class** (from the reference designator prefix — `R`, `C`, `L`, `D`, `LED`,
  `FB`, `Y`, `U`, `Q`, `J`, `F`, …) supplies the unit, so `5k` on an `R` means
  5 kΩ. **RKM** notation is understood (`4k7` = 4.7 kΩ, `4n7` = 4.7 nF).
- **Compound values** are split: `10nF X7R 16V 10%` →
  `{capacitance: 10 nF, dielectric: X7R, voltage_rating: 16 V, tolerance: 10%}`.

Matching then picks one of three modes from the line's shape:

- **Value-band** — a package plus a primary magnitude (R/C/L): exact package,
  value within a tolerance band around the nominal (±1% by default; a
  tolerance-bearing line like `1k 10%` widens the band proportionally).
- **Package-only** — a package but no magnitude (e.g. a generic LED or ferrite
  bead): category subtree + package.
- **Signature** — no package but discriminating numbers (e.g. a pin header's
  positions/rows/pitch): every provided parameter must match within a tight window.

Matching is **closed-world and exact about what you ask for**: a generic part with
*no* tolerance recorded matches a bare `10k` line but is **excluded** by a stricter
`10k 1%` line. You never silently satisfy a 1% requirement with a part of unknown
tolerance. A line that lists a tolerance demands a part that records one.

If exactly one catalog part satisfies the spec, the line auto-resolves
(`Auto (Generic)`). If several do, it is `Needs review` and you pick. If none do,
it stays unresolved with a *Create generic part* action.

## The AI assistant (optional LLM)

partrack can use a large language model (LLM) as a **fallback** for rows the
deterministic parser cannot read cleanly. This is entirely optional and **only
available if an administrator has configured an LLM** for the instance. With no
LLM configured, everything above still works — you just do not get AI assistance.

### What it does

The AI is a backstop, **not** the primary path. Deterministic parsing and the
priority tiers always run first; the LLM is only asked about rows that would
otherwise be left for review — for example:

- an oddly formatted value (`9k10%`, `5V/10uF`) the parser left with leftover
  tokens;
- a row with no recognisable class or value;
- a **specific part whose MPN lives in a description column** you did not map as
  MPN — the AI can read the identity out of the full row.

For each escalated row it does up to two things:

1. **Classify** the row as *generic*, *specific*, *ignore*, or *unknown*. For a
   *specific* part it reads the identity (manufacturer + MPN and/or distributor +
   SKU) — keeping **only** values that literally appear in the row (an
   anti-hallucination guard), so it can never invent a part number.
2. For a *generic* part, **extract parameters** into the chosen category, grounded
   with your catalog's real value ranges, enum values, and packages in use.

A discovered identity is overlaid onto the line's empty columns, so it then flows
through the normal SKU/MPN resolution tiers.

### How it shows up

- The AI runs **asynchronously** during the Interpret step. Phase-1 (deterministic)
  readings appear instantly; AI-read rows stream in live with a spinner and a
  *Reading with AI…* overlay, then resolve in place.
- AI-read rows get an **AI indicator** and an **AI-read** badge, and sort to the
  top so you verify them. You can open the indicator to see the model's one-line
  decision (and reasoning, when the model provides it).
- If an AI read fails, the row simply keeps its deterministic reading — the AI
  **never blocks** import.

### Important caveats

- **Privacy:** when the LLM is enabled, the contents of escalated BOM rows are
  sent to the configured model provider. Only escalated rows leave the instance,
  and only when the feature is on.
- **Limits:** an administrator caps how many rows a single import may send to the
  AI, so one pathological file cannot fan out endlessly. Beyond the cap, extra
  rows keep their deterministic reading.
- **Verify AI reads.** They are a convenience, not gospel. The review gate badges
  and sorts them precisely so you give them a glance.

## Interpretation memory

Prototyping iterates BOMs constantly, and re-reviewing the same 40 passives every
revision would be miserable. So once you confirm (or the AI derives) how a line
should be read, partrack **remembers** it.

- Memory is keyed by **component class + normalised value + normalised footprint**,
  so `10k`, `10K`, and `10 k` all hit the same entry.
- It is **scoped to the project** and shared with your team — a correction in one
  project never changes how a *different* project's BOM is read (this deliberately
  avoids leaking project-specific identity decisions across designs).
- It is reused on every later import of the same project, and across MPN-only and
  generic lines alike. A remembered row shows a **remembered** badge in the review
  gate and sinks to the bottom (it needs the least attention).
- It also feeds the live preview, so the Interpret step shows exactly what the
  import will persist — not a fresh parse the import would then override.

### Inspecting and pruning memory

Open the project's **Interpretations** page (the interpretation-memory link on the
project) to see every remembered interpretation: its key, who created it, and what
it resolves to. **Delete** an entry to make future imports re-read that line
fresh — useful when a past decision was wrong or the design moved on. Viewing
needs the viewer role; deleting needs member.

Across revisions, partrack also carries forward the immediately prior revision's
reading for an unchanged line, preserving any edit you made, so reviewed lines do
not re-flag.

## Reviewing and resolving lines (the BOM Review dialog)

After import, the revision page shows a resolution bar and a per-line table. Lines
that still need a human decision (ambiguous, unresolved, fuzzy-confirm, or
importing) are counted as **ready for review**. Open **Review** (or click a row)
to step through them one at a time.

Each card shows the line's details, the status badge, and the parser's
interpretation. Depending on the line, you get the relevant actions:

- **Matching parts in your library** — for an unresolved generic line, the exact
  catalog parts that satisfy its spec, each showing **stock on hand** (and whether
  it is a global part). Pick one to resolve.
- **Create generic part** — for a generic line with no specific manufacturer part,
  create (or reuse) a generic catalog part from the interpreted spec and resolve
  the line to it. (A generic part is an ordinary team part with no MPN, carrying a
  category, package, and value parameters — it works in inventory, stock checks,
  and builds like any other part.) You can also **Create all** generics in one
  click from the revision page.
- **Import new part** — when the distributor lookup found several matching
  products, pick the primary listing (and any others to import across all enabled
  distributors), then confirm.
- **Choose from local library** — search your catalog and link any part directly.
- **Import from distributor** — look up by MPN or SKU; one lookup fans out across
  every enabled distributor and lets you pick the listing(s) to import.
- **Confirm match** — accept a fuzzy `Auto (MPN ?)` match so it persists as a
  confirmed resolution and leaves the queue.
- **Ignore / Un-ignore** — exclude a line from stock checks and ordering (or bring
  it back). Use this for DNPs and anything you do not stock.
- **Clear resolution** — unlink a part and re-decide.

A revision is **ready for stock checking** when every line is either resolved to a
part or ignored.

## Stock checking

A **stock check** answers: *can I build N boards of this revision, and what am I
short?* Open **Stock Check** from the project or a revision, enter a **build
quantity**, and run it.

> The build quantity is **ephemeral** — you type it at check time and it is not
> stored on the project. (It is only recorded when you actually create a
> [build](builds.md).)

For each line the check reports:

| Field | Meaning |
|---|---|
| **Quantity per board** | Placements per board (the line's quantity). |
| **Required** | Quantity per board × build quantity. |
| **On hand** | Current stock of the resolved part. |
| **Reserved / Free** | Stock already soft-allocated to other builds, and what is left free. |
| **On order** | Quantity inbound on open orders. |
| **Shortfall** | How many you are short for this build. |
| **Status** | `in_stock`, `partial`, or `out_of_stock` (see below). |
| **Below restock** | Whether building would drop you under the part's minimum stock level. |

The **status** per line:

| Status | Meaning |
|---|---|
| **in_stock** | Free stock (of the part that will fulfil the line) covers the requirement. |
| **partial** | Some stock exists, but not enough — you are short. |
| **out_of_stock** | No usable stock, or the line is unresolved. |

Two things to understand:

- **Substitutes count.** A line's check considers its primary part **and** its
  approved substitutes together. If the primary is short but an approved
  substitute has enough **free** stock to fully cover the requirement, the line is
  shown as covered by that substitute. (A single line is never split across two
  parts.) The substitute chosen here is exactly the one a build will record.
- **Unresolved lines** are treated as fully short (`out_of_stock`) — resolve or
  ignore them first for an accurate picture.

The result also reports **fully buildable** — the largest whole number of boards
you can build right now from free stock, limited by the tightest line.

### What to do next

From the stock-check result you can:

- **Create a build plan** — turn the check into a [build](builds.md). Submitting a
  build with shortfalls automatically **adds those shortfalls to your team's
  [shopping cart](cart.md)** to be ordered. (A full-stock build can go straight to
  ready.)
- Proceed to **checkout** a build to record assembly and deduct stock.

See [Cart & Ordering](cart.md) for turning shortfalls into orders, and
[Builds](builds.md) for the full assembly lifecycle.

> **Generic parts and ordering.** A generic part has no distributor listing of its
> own, so when it is short you choose a concrete listing to actually order — the
> cart handles this binding when you order against a generic part.

## Substitutes

A BOM line can have **approved substitutes** — alternative parts acceptable in
that position. Manage them from the line (the **Manage Substitutes** dialog):

- **Add** a substitute by searching your catalog.
- **Order them by priority** — move substitutes up and down. Priority order
  decides which substitute a build reaches for first.
- **Remove** a substitute.
- **Import** the substitute lists used on a previous revision of the project, so
  you do not rebuild them each revision.

How substitutes behave:

- **Stock checks** count an approved substitute's free stock toward a short line,
  walking the priority order and taking the first substitute that *fully* covers
  the requirement.
- **Builds** record and draw from the same substitute the stock check showed, so
  what you saw is what gets consumed.
- An in-stock substitute keeps a board buildable even when the primary is out of
  stock — which is exactly the point.

## Quick do's and don'ts

- **Do** map the **Value** and **Footprint** columns for prototyping BOMs — that
  is what unlocks generic matching and the "find what's in the drawer" workflow.
- **Do** spot-check the Interpret step even when it looks clean; a confident
  misread can still slip through.
- **Do** ignore DNP / fiducial / mounting-hole lines so they do not skew stock
  checks. (Many are auto-detected as **Ignored**.)
- **Don't** expect generic matching to invent parts — it only links to parts that
  already exist; use *Create generic part* (or pre-populate your catalog) to add
  them.
- **Don't** assume an `Auto (MPN ?)` (fuzzy) line is correct — open it and
  **Confirm match** or pick the right part.
- **Don't** rely on AI reads without a glance — verify the AI-badged rows.
- **Don't** stock-check a revision that still has unresolved lines and expect
  accurate numbers — resolve or ignore everything first.
