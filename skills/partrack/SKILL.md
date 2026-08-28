---
name: partrack
description: Research the user's own electronics parts inventory (partrack) from the command line — what parts are on hand, where they are stored, their specs, datasheets, distributor listings and notes, parametric search of the catalog, plus the workflow around it: projects and their BOM revisions, stock checks for a build, the cart, orders, builds and checkouts. Use when asked what is in stock, whether a design can be built from inventory, what is on order, what a build is waiting for, where a part lives, or for the specs of a part the user already owns.
---

# Parts inventory research (`pt`)

`pt` is a CLI over the user's partrack server — their *own* catalog and stock
room, not a distributor's. Use it whenever the question is about parts they
already have. For sourcing parts they do **not** have, that is a distributor
catalog question — the DigiKey research skill (`dks`) covers it, and the two
are complements.

`pt` reads; it does not write. It cannot check parts out, adjust stock, add to
the cart or import a BOM. It *can* read a BOM file locally and tell you what the
import will make of it — see **Cross-checking a BOM** below — and it can read
everything the server already holds: the catalog and stock room, projects and
their imported BOM revisions, the cart, orders, builds and checkouts.

## Credentials

A human seeds them once with `pt auth login`. **Do not attempt to log in
yourself or handle the password** — just call `pt` and let it read the stored
token. Confirm with `pt auth status` (add `--format json` to parse it). Exit
code 3 means the token is missing or rejected: report that and stop, rather
than trying to work around it.

## Conventions for non-interactive use

- Pass `--format json` for anything you parse; the plain layout is for humans
  and is not a stable interface.
- stdout is data only; warnings and notices go to stderr. **Read stderr** — it
  is where `pt` tells you a filter did not mean what you probably wanted.
- Exit codes: `0` ok · `1` error · `2` usage · `3` auth · `4` not found ·
  `5` ambiguous · `6` network.
- Exit 5 means the reference matched several parts, and they are listed. Pick
  one deliberately — never retry with a guess.
- `--team` selects the team when the user belongs to more than one.
- `--history` and `--params` take their value **attached** (`--history=20`,
  `--params=tolerance,power`), because the value is optional. Detached,
  `--history 20` leaves `20` as a stray argument; bare `--history` is fine.
- Reference data (categories, the parameter registry) is cached for 30 days;
  `--refresh` re-fetches it. Stock, price and location are **never** cached, so
  a stock figure is always live.

## The research loop

Parametric search takes three steps. Skipping to step 3 with a guessed slug is
the most common way to get a confidently empty result.

```sh
pt categories resistor                      # 1. find the category
pt filters chip-resistor-surface-mount      # 2. see the filterable slugs AND
                                            #    the values your catalog holds
pt search -c chip-resistor-surface-mount --param resistance=10k --param tolerance=1%
```

`pt filters` is the important one: it prints each parameter's slug, unit and
value type, plus **what your catalog actually contains** — enum vocabularies
verbatim and numeric spans. Enum matching is exact string equality server-side,
so copy those strings character for character.

The free-text query is a plain substring match over MPN, description,
manufacturer and aliases. It is **not** value-aware: `pt search "10k 0402"`
finds nothing useful. Values belong in `--param`, package in `--package`.

## Addressing an object

Every part-taking command accepts, in this order: `@3` (row 3 of the last
listing), a full id, an id prefix, an exact MPN, or a substring. `@N` is the
natural way to chain — list, then act on a row.

Projects, orders, builds and checkouts address the same way. The text form
matches the project name, an order's external reference or distributor, or a
build's project name or revision label; a checkout has no name, so use `@N` or
an id prefix.

**`@N` belongs to the kind of the last listing.** After `pt orders list`, `@1`
is an order and `pt show @1` fails with exit 2 rather than fetching some
unrelated part. Two commands deliberately leave `@N` on parts, because their
rows *are* parts: `pt cart show` and `pt bom show`.

```sh
pt search -c Resistors --param resistance=10k
pt show @2
pt stock @2 --history
```

## The `--param` grammar

Repeatable. Values use the same SI grammar as the BOM importer, so RKM (`4k7`,
`0R22`), SI prefixes, fractions (`1/8W`) and ppm all parse.

| Form | Meaning |
|---|---|
| `resistance=10k` | ±1% band around 10 000 (`--param-tolerance 0` for exact) |
| `resistance=9k..11k` | inclusive range |
| `resistance='>=10k'` | open lower bound (quote it — the shell eats `>`) |
| `mounting_type=SMD,THT` | enum/text, matches any listed value |

A bare value gets a tolerance band because a stored `0.1 µF` and `100 nF`
differ in the last floating-point bit. The search footer always prints what
your value actually became — check it.

## Three things that will mislead you

These are properties of the server's data model, not bugs, and each one can
produce a wrong answer that looks perfectly reasonable.

**1. Unmapped parameters cannot be filtered.** Parameters arrive from
distributor enrichment and an admin maps them onto the canonical registry.
Until that happens they have no `parameter_id`, and filtering keys on
`parameter_id` — so the parameter is stored and displayed but unmatchable.
`pt show` marks these `(unmapped)`. If a spec you can see in a dossier yields
nothing as a filter, this is why; say so rather than concluding the user has no
such parts.

```sh
pt --format json show @1 | jq -r '.part.parameters.distributor_sourced[]
  | select(.parameter_id == null) | .name'          # not filterable
```

**2. A category's profile lives on the class, not the leaf.** `Resistors`
carries `value_param: resistance`; its child `Chip Resistor - Surface Mount` —
where the parts actually sit — carries nothing. `pt filters` resolves the
inherited profile and reports `Profile from`, so trust its output over the raw
API.

**3. Range parameters compare only the lower bound.** A `range` parameter
(`temperature_range`, `voltage_supply_range`, …) stores the span's low end in
`numeric_value` and the high end in `numeric_value_max`, and the server's
filter looks only at the low end. So `--param temperature_range=85` finds parts
whose **minimum** rating is 85 °C — almost never the question. `pt` warns on
stderr when you filter one. To find parts rated *up to* 85 °C, filter for a
maximum at or above it, or inspect `numeric_value_max` in a dossier. Range
parameters are also absent from the `pt filters` table (the catalog endpoint
excludes them); the command names them at the bottom instead.

## Answering the common questions

```sh
# What do we have in stock, and where?
pt search STM32 --stock in
pt where STM32F103C8T6
pt where --location "Drawer A"          # contents, including sub-locations
pt where                                # the whole storage tree

# How many can I actually use?  (on-hand minus build reservations)
pt --format json stock @1 | jq '.quantity_free'
pt stock @1 --history=20               # the ledger behind the number

# What is running low?
pt search --stock low --fields mpn,stock,min,reorder,loc --sort stock

# Everything about one part
pt show @1                              # or --only params,listings
pt --format json show @1 | jq -r '.part.datasheet_url'
pt --format json show @1 | jq -c '.part.listings[]
  | {distributor_name, sku, stock_available, unit_price, moq}'
```

Choose columns with `--fields` rather than post-filtering in jq: available are
`id, uuid, mpn, mfr, desc, category, pkg, status, scope, stock, min, reorder,
loc, tracked`. `--params` adds parameter columns (requires `--category`).

In JSON, values are native — quantities are numbers and anything absent is
`null`, never the `—` used for display. **An empty `stock` means the part has
no inventory entry at all**, which is a different claim from holding zero. Do
not report the two alike:

```sh
pt --format json search --fields mpn,stock | jq -r '.items[]
  | select(.stock == null) | .mpn'      # not tracked — not "out of stock"
```

## Answering questions about the workflow

The chain is: a project owns BOM revisions → a stock check says what building N
boards would take → shortfalls go to the cart → the cart becomes orders →
receiving an order feeds a build. Every step is readable.

```sh
# What projects are there, and is the active BOM ready to build?
pt projects list
pt projects show "Power Supply v2"        # incl. the active revision's resolution counts

# Which BOM lines still need a human?
pt bom show "Power Supply v2" --unresolved

# Can we build 10 of these from stock?
pt bom stock "Power Supply v2" --quantity 10 --short
pt --format json bom stock @1 --quantity 10 | jq '.fully_buildable'

# What is on order, and what is it for?
pt orders list --status open
pt orders show @1                         # line items + the builds it was raised for

# What are we assembling, and what is reserved?
pt builds pipeline                        # active builds by stage
pt builds show @1                         # allocations, orders, substitute choices
pt builds show @1 --preview               # what it will actually consume, line by line

# What is staged to buy, and why?
pt cart show --demands
pt --format json cart show | jq '[.items[].line] | add'   # cart total

# Who took what off the shelf?
pt checkouts list
pt checkouts show @1
```

Four things worth knowing before you report an answer:

**Build quantity is ephemeral.** It is a parameter of the stock-check question,
never a property of the project, which is why `--quantity` is required and has
no default. A stock check is a snapshot against current stock, not a commitment.

**`fully_buildable` and per-line shortfall answer different questions.**
`fully_buildable` is how many complete boards current stock covers; a line can
be short while others have plenty. Report the one that was asked for.

**`quantity_free` is on-hand minus other builds' reservations.** A part can show
healthy stock and still be unusable because a pending build has it reserved.
`pt bom stock` breaks this out per line as `on hand` / `reserved` / `free`.

**A cart listing marked `auto` was chosen by the sourcing policy, not a person.**
`pt cart show --fields mpn,qty,dist,unit,line,chosen,reason` shows which rule
applied. An item with no listing at all cannot be ordered, and `pt` says so
under the table — do not silently treat it as free.

Two order endpoints are not exposed because they are not reads: exporting an
order mutates it, and `pt orders show --distributor-status` calls the
distributor and caches the result. Do not reach for the latter unless the user
asked for live distributor state.

## Cross-checking a BOM before it is imported

BOM import is the friction point in partrack, and most of what goes wrong is
decided before the server sees anything: how the file parses, which column means
what, and how each line's value reads. `pt bom check <file>` rehearses exactly
that stage **locally** — no server, no login, nothing imported — and prints what
partrack will make of the file. Add `--resolve` and the server also forecasts
what each line would resolve to.

When someone asks for help with a BOM, run it first and read the output back to
them. It is the cheapest possible review, and it is the only way to see the
things the import wizard does silently.

```sh
pt bom check board.csv
pt bom check board.csv --problems                  # only rows needing attention
pt --format json bom check board.csv | jq '.findings[]'
```

**Work the findings in severity order.** `error` means the import will not do
what they intend; `warning` means it proceeds but something is likely misread.

| What you'll see | What it means | What to tell them |
|---|---|---|
| `row_dropped` | The row never reaches partrack at all. The wizard discards any row with no reference designators or a non-positive quantity, **silently**. | Give the line a designator and a quantity ≥ 1, or confirm they meant to omit it. This is the single most common "partrack lost my parts" cause. |
| `quantity_coerced` | Quantity is parsed with JavaScript's `parseInt`, so `1,000` imports as **1** and `3 pcs` as `3`. | Strip the separators and units from the quantity column. |
| `sku_without_distributor` | The import resolves a SKU only against a known distributor. With no distributor column, **no SKU row resolves by SKU**. | Add a distributor column, or pick one distributor in the wizard's distributor step. Model it with `--distributor <name>`. |
| `duplicate_role` / `missing_role` | Two columns claim one role (only the first is used), or designators/quantity are unmapped (everything drops). | Fix with `--map "<header>=<role>"` to confirm, then set the same mapping in the wizard. |
| `designator_count` | `R1,R2` with quantity 5. | A BOM error the importer carries through faithfully. Theirs to fix. |
| `unmapped_column` / a `—` role | A column partrack ignores. Altium's value column is called **Comment** and does not auto-map. | `--map "Comment=value"` — without it, every passive loses its value and cannot match parametrically. |

**Then read the interpretation table.** This is the actual cross-check: the raw
cells beside what partrack understood. Look for:

- **`route: unknown`** — nothing reliable extracted; the line will need manual
  review at import. Common causes: a value that is a symbol name
  (`Conn_01x04_Pin`, `LED`), or an empty value column.
- **`!`** marks rows the review gate will surface first; **`~`** marks rows whose
  value did not parse cleanly. `~` rows carry `residue:<tokens>` — the tokens the
  parser could not attribute, which is usually the clue. `9k10%` leaves
  `residue:9k10%` because the missing space defeats the split.
- **`class_footprint_conflict`** — the reference designator and the footprint
  disagree about what the part is (an `FB` in an `L_` footprint). One of them is
  wrong, and the designator wins.
- **The package column.** Verify the metric trap resolved the way they expect:
  `R_0603_1608Metric` must read as **0603** (imperial), not 1608. A bare `0402`
  with no structural cue is genuinely ambiguous and defaults to imperial.
- **The params column.** `10k` → `resistance=10 kΩ`, `100nF X7R 16V` →
  `capacitance=100 nF dielectric=X7R voltage_rating=16 V`. A value that produced
  no params will not match anything parametrically.

**Then forecast what will resolve.** `--resolve` asks the server to walk the real
resolution tier ladder against their catalog and report the outcome per line. It
writes nothing — no revision, no line items, no distributor lookups.

```sh
pt bom check board.csv --resolve --project "Rev C Mainboard"
pt --format json bom check board.csv --resolve | jq '.forecast.summary'
```

**Always pass `--project` when one exists.** The prior-revision tier and the
project's remembered interpretations are project-scoped; without it a re-imported
BOM reports lines as unresolved that the real import would carry forward, and you
will hand back a needlessly alarming answer.

| Outcome | What to tell them |
|---|---|
| `auto_sku` / `auto_mpn` / `auto_generic` / `auto_prior_revision` | Lands by itself. Nothing to do. |
| `auto_mpn_fuzzy` | Lands, but the MPN matched as a substring (BOM `AZ431A` → catalog `AZ431AN-ATRG1`). It is surfaced for a one-click confirm at review — worth eyeballing that it picked the right variant. |
| `ambiguous` | Several catalog parts match; someone has to pick. The candidates come back with their stock, so recommend on what they actually hold. |
| `importing` | Not in their library. The distributor pipeline will try to fetch it — which needs that distributor configured and enabled for the team. |
| `unresolved` (empty string) | Nothing matched and there is nothing to look up. Usually an unmapped value column, a parametric spec with no matching part, or a row that needs a generic part creating. |

**Two limits to state, not paper over.** An `importing` line's real outcome
depends on a distributor API call the preview does not make. And the forecast is
against the catalog as it stands: parts created during the import can change what
later lines match. So report "N of M lines resolve on this forecast", never "your
BOM will import cleanly".

The import itself is still done in the web UI; `pt` cannot post a revision yet.

## Limits worth stating rather than working around

- **No writes.** Checkouts, deposits, cart operations and BOM import are not
  implemented. Direct the user to the web UI for those. (`pt bom check` is a
  local read of a file, not a write to partrack.)
- **No "where used".** Nothing answers "which projects consume this part"; the
  server has no such query. You can get close from the other side: `pt stock
  <part>` lists the builds reserving it, and `pt bom show <project>` lists what
  one project consumes.
- **Distributor stock and price are not in search results.** They cost one
  call per part, so they appear in `pt show`, not `pt search`.
- **Substitutes, distributor configs, the dashboard and the activity log have
  no commands yet.** If the question needs one of those, say so rather than
  approximating it from what is available.
- **Reading the cart needs the purchaser role.** Exit 3 on `pt cart show` when
  everything else works means the user is a viewer, not that auth is broken.
