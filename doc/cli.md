# `pt` — the partrack command-line client

`pt` queries your partrack instance from a terminal: parametric part search,
stock levels and locations, distributor listings, notes and datasheets, and the
workflow around them — projects and their BOM revisions, the cart, orders,
builds and checkouts. It is built for non-interactive use — plain column-aligned
text by default, `--format json` for anything that gets parsed, data on stdout
and diagnostics on stderr.

It is a sibling to [`dks`](https://github.com/…/dks) (DigiKey catalog research):
same conventions, same output discipline, pointed at your own library instead of
a distributor's.

```sh
go build -o pt ./cmd/pt
# or
go install github.com/Yamakaja/partrack/cmd/pt@latest
```

## Authentication

```sh
pt --url https://partrack.example.com auth login   # prompts for email + password
pt auth status
pt auth logout                                     # revokes the token, then removes it
```

`auth login` signs in, mints an API token named `pt@<hostname>`, closes the
session it used, and stores the token with `0600` permissions. Your password is
never written to disk.

If you sign in through **OIDC or LDAP** and have no local password, create a
token in the web UI and hand it over instead:

```sh
pt auth login --token ptk_…
```

For CI, set `PARTRACK_TOKEN` and skip login entirely.

## Configuration

Precedence: built-in defaults < config file < environment < flags.

| What | Flag | Env | Default |
|---|---|---|---|
| Server URL | `--url` | `PARTRACK_URL` | — (required) |
| Active team | `--team` | `PARTRACK_TEAM` | the only team you belong to |
| API token | — | `PARTRACK_TOKEN` | stored by `auth login` |
| Output format | `--format` | — | `plain` |
| Config dir | `--config-dir` | `PT_CONFIG_DIR` | `$XDG_CONFIG_HOME/partrack` |
| Cache dir | `--cache-dir` | `PT_CACHE_DIR` | `$XDG_CACHE_HOME/partrack` |

`-v` / `--debug` logs each HTTP request and its status to stderr, which is the
quickest way to see the query a command actually sent.

The config and cache directories fall back to `~/.config/partrack` and
`~/.cache/partrack` when `XDG_*` is unset. State found in the old
`os.UserConfigDir` location is migrated on first run, with a note on stderr.

```sh
pt config show          # resolved settings, with where the token came from
pt config get team
pt config set team Lab
pt config path
pt config clear-cache
```

Settings live in `config.json`; the token lives in a separate `credentials.json`
at mode `0600`, so the config file never holds a secret and is safe to share.

## Addressing an object

Every command that takes a part accepts, tried in this order:

| Form | Meaning |
|---|---|
| `@3` | row 3 of your last listing |
| `3f2a1111-…` | a full part id |
| `3f2a` | an id prefix, matched against parts you have recently seen |
| `RC0402FR-0710KL` | an exact MPN |
| `stm32f103` | a substring of MPN, description, manufacturer or alias |

A reference that matches several parts prints the candidates and exits **5**
rather than picking one, so a script never acts on a part you did not name. A
substring match that resolves to exactly one part reports which one it chose, on
stderr.

```sh
pt search -c Resistors --param resistance=10k
pt show @2
pt stock RC0402FR-0710KL
```

Projects, orders, builds and checkouts use the same ladder, with the text form
matched against whatever identifies that kind:

| Kind | Text form matches |
|---|---|
| project | the project name |
| order | the external reference, then the distributor name |
| build | the project name or the BOM revision label |
| checkout | nothing — a checkout has no name, so use `@N` or an id prefix |

**`@N` is scoped to the kind of the last listing.** After `pt orders list`, `@1`
is an order; asking `pt show @1` for a part then fails with a usage error naming
the command that would refresh the set, rather than fetching a part by an
order's id.

```sh
pt orders list
pt orders show @1          # the first order
pt show @1                 # exits 2: the last listing was orders
```

`pt cart show` and `pt bom show` are the exceptions that prove the rule: their
rows *are* parts, so they leave `@N` addressing parts — `pt show @2` on a cart
row is the useful next step.

## Commands

**The parts library**

| Command | Purpose |
|---|---|
| `pt search [query]` | search the catalog and your inventory |
| `pt filters <category>` | the parameters carried by parts in a category, with the values in your catalog |
| `pt categories [query]` | browse the category tree with your part counts |
| `pt show <part>` | full dossier: identity, inventory, parameters, listings, note, reservations |
| `pt stock <part>` | on-hand vs reserved vs free, thresholds, location, ledger |
| `pt where [part]` | locate a part, list a location's contents, or print the storage tree |

**The workflow: BOM → shortfall → cart → order → build**

| Command | Purpose |
|---|---|
| `pt projects list` / `show <project>` | projects, their active revision and its resolution summary |
| `pt bom check <file>` | how partrack will read a BOM file, before importing it (local, no server) |
| `pt bom show <project>` | an imported revision's lines and how each one resolved |
| `pt bom stock <project> --quantity N` | what building N boards would take, line by line |
| `pt cart show` | the team's cart, with the listing each item will be ordered from |
| `pt orders list` / `show <order>` | purchase orders and their line items |
| `pt builds list` / `pipeline` / `show <build>` | assembly runs and the stock they reserve |
| `pt checkouts list` / `show <checkout>` | ad-hoc withdrawals and the ledger entries they made |

**Everything else**

| Command | Purpose |
|---|---|
| `pt teams` | the teams you belong to |
| `pt auth` / `pt config` | credentials and settings |

Most list commands share `--fields`, `--limit`, `--offset` and `--format json`;
`pt <noun> list --help` names the columns that noun offers.

Two flags take an **optional** value and so require it attached:
`--history=20` (`pt stock`) and `--params=tolerance,power` (`pt search`).
Bare `--history` / `--params` use their defaults; detached, the value is left
behind as a stray argument and the command exits 2 saying so.

Reading the cart needs the **purchaser** role; everything else here needs
**viewer**. A role you do not hold comes back as exit **3**.

### Searching

The free-text query is a **substring match** over MPN, description, manufacturer
and aliases — it is *not* value-aware. `pt search "10k 0402"` will not work;
that is a parametric query:

```sh
pt search -c Resistors --param resistance=10k --param tolerance=1% --package 0402
pt search -c Capacitors --param capacitance=100n..1u --package 0603
pt search STM32 --stock in
pt search --stock low --fields mpn,stock,min,loc --sort stock
```

Other filters: `--mpn` (exact), `--scope global|team`, `--lifecycle`,
`--location` (includes descendant locations), `--limit` / `--offset` / `--all`.

`--all` walks every page in 200-row steps, up to **2000 rows**. Past that it
stops and says so on stderr rather than silently returning a partial answer.

### The `--param` grammar

`--param` is repeatable. Slugs come from `pt filters <category>`; values go
through the same SI grammar the BOM importer uses, so RKM (`4k7`, `0R22`), SI
prefixes, fractions (`1/8W`) and ppm all work.

| Form | Meaning |
|---|---|
| `resistance=10k` | a ±1% band around 10 000 (`--param-tolerance` to change, `0` for exact) |
| `resistance=9k..11k` | an inclusive range |
| `resistance='>=10k'` | an open lower bound (`>` is accepted; bounds are always inclusive) |
| `resistance='<=1M'` | an open upper bound |
| `mounting_type=SMD,THT` | enum / text — matches any listed value |

A bare value gets a band rather than an exact match because a stored `0.1 µF`
and `100 nF` differ in the last floating-point bit. The band is tight enough to
separate adjacent E96 values, and the search footer always prints what your value
actually became:

```
3 parts · team Lab · Resistors · resistance 9.9 kΩ..10.1 kΩ · tolerance 0.99 %..1.01 %
```

Enum matching is **exact string equality** server-side, which is why
`pt filters` prints the vocabulary actually present in your catalog — copy those
strings verbatim.

### What is filterable, and why

Parameters are curated in three admin-managed layers (see
[datamodel.md](datamodel.md) for the model). All three affect what `--param` can
do:

1. **The parameter registry** — canonical definitions (slug, unit, `value_type`).
2. **Distributor parameter mappings** — each distributor's own parameter ids are
   mapped onto registry definitions. Enrichment auto-registers newly-seen ones
   *unmapped*, awaiting admin review.
3. **The category class profile** — `value_param` (which slug a bare value
   encodes) and `primary_params`, set per category.

The consequences worth knowing:

**Unmapped parameters cannot be filtered.** Filtering keys on the canonical
`parameter_id`, so a parameter still awaiting admin mapping is stored and
displayed but unmatchable. `pt show` prints each parameter's slug and marks these
`(unmapped)`, so their absence from `pt filters` is explained rather than
mysterious.

**Class profiles live on the class, not the leaf.** `Resistors` carries
`value_param: resistance`; its child `Chip Resistor - Surface Mount` — where the
parts actually sit — carries nothing, and the server's schema endpoint reads only
the category you ask for. `pt` walks the cached tree and reports the inherited
profile, naming where it came from:

```
Category      Resistors / Chip Resistor - Surface Mount
Value param   resistance
Profile from  Resistors

SLUG            NAME           UNIT  TYPE     IN YOUR CATALOG
power_rating +  Power Rating   W     numeric  63 mW .. 400 mW
resistance *    Resistance     Ω     numeric  1 Ω .. 1.47 MΩ
tolerance +     Tolerance      %     numeric  0.1 % .. 1 %
```

`*` marks the value parameter, `+` the curated primary set. The values column is
catalog-derived — what your parts actually hold — because that is what a filter
can match.

**Range parameters are a trap.** A `range` parameter (`temperature_range`,
`voltage_supply_range`, …) stores the span's lower bound in `numeric_value` and
the upper in `numeric_value_max`, and the server's filter compares **only the
lower bound**. So `--param temperature_range=85` finds parts whose *minimum*
rating is 85 °C, not parts rated up to 85 °C. `pt` warns on stderr whenever you
filter one. They are also omitted from `pt filters` entirely (the catalog
endpoint excludes them), so the command names them explicitly instead.

### Narrowing a dossier

`pt show <part>` prints six sections; `--only` picks among them:

```sh
pt show @1 --only params,listings
pt show @1 --only stock            # alias for `inventory`
```

Sections: `identity, inventory, params, listings, note, allocations`.
`specs` / `parameters` and `stock` are accepted as aliases for `params` and
`inventory`.

Two other narrowing flags are easy to miss: `pt categories --non-empty` hides
categories your team holds nothing in (and `cats` is an alias for the command),
`pt filters <category> --max-values N` caps how many observed values each
parameter lists, and `pt where --location X --limit N` bounds a location listing.

### Checking a BOM before importing

BOM import is where most friction lives, and most of it is decided before the
server is involved — in how the file parses and which column means what.
`pt bom check` runs that stage locally and prints what partrack will make of it:

```sh
pt bom check board.csv
pt bom check board.csv --problems              # only the rows needing attention
pt bom check board.csv --map "Comment=value"   # Altium calls the value column Comment
pt bom check board.csv --delimiter ';' --skip 3 --comment '#'
```

It needs no server, no team and no login, and it imports nothing. What it shows:

- **The column mapping** it inferred, so a `Comment` column silently ignored is
  visible before it costs you a review cycle. Override with `--map`, by header
  name or by 1-based position.
- **Every row the import would drop.** The wizard discards any row with no
  reference designators or a non-positive quantity, silently — a blank quantity
  cell removes the line from your BOM with nothing said. Each one is reported
  with its line number and the reason.
- **Quantities the importer coerces.** The wizard parses quantity with
  JavaScript's `parseInt`, so `1,000` imports as **1**. `pt` reproduces that
  (predicting the import, not improving on it) and flags it.
- **How each line is interpreted** — route, category, package and the parameters
  extracted from the value, next to the raw cells they came from. This is the
  cross-check: what you wrote beside what partrack understood.

Rows are ordered attention-first, the way the review gate orders them. `!` marks
a row the gate will surface; `~` marks one whose value did not parse cleanly, and
those carry their unattributed tokens as `residue:…`, which is usually the clue
to what was misread.

Two structural warnings are worth acting on before you import:

- **SKUs with no distributor.** The import resolves a SKU only against a known
  distributor, so a BOM with a SKU column and no distributor column resolves
  none of them — they fall through to the MPN and parametric tiers.
  `--distributor <name>` models choosing one.
- **Designator count vs quantity.** `R1,R2` with quantity 5 is a BOM error the
  importer will faithfully carry through.

#### Forecasting what will resolve

Everything above is the parse. `--resolve` adds the other half: what each line
would actually resolve to, against your catalog.

```sh
pt bom check board.csv --resolve
pt bom check board.csv --resolve --project "Rev C Mainboard"
```

This part needs a login and a team. The forecast is not computed locally — the
server walks the **real** resolution tier ladder (`internal/bomresolve`, the same
code the import job runs) and reports the outcome per line. It writes nothing:
no revision, no line items, no distributor lookups, no inventory entries.

```
OUTCOME         LINES  MEANING
auto_mpn        3      resolves — exact MPN in your catalog
auto_generic    3      resolves — one catalog part satisfies the parametric spec
auto_mpn_fuzzy  2      resolves, but flagged — MPN matched as a substring
ambiguous       3      needs you to pick — several parts match
importing       3      not in your library — will be looked up at the distributor
unresolved      2      unresolved — nothing matched and there is nothing to look up

8 of 16 lines resolve without you.
```

Lines that need attention are then listed individually, with the candidates an
ambiguous line was choosing between (and their stock, so a pick can be made on
what you actually have).

**Pass `--project` when you have one.** The prior-revision tier and the project's
remembered interpretations are both project-scoped, so without it a re-imported
BOM reports lines as unresolved that the real import would carry forward.

Two limits are inherent rather than fixable: a line marked `importing` depends on
a distributor API call the preview does not make, and the forecast is against the
catalog as it stands — parts created during an import can change what later lines
match. See [bom-import.md](bom-import.md) for the full flow.

### Reading an imported BOM

`pt bom show <project>` prints the line items of a revision — the designators,
what each line resolved to, and the state that decided it. Without `--revision`
it reads the project's **active** revision, which is the one stock checks and
builds run against.

`--unresolved` narrows to the lines that still need a person. That includes
`auto_mpn_fuzzy`, which does resolve: a substring MPN match is a weaker claim
than an exact one, so the review queue surfaces it and so does this.

```sh
pt bom show "Power Supply v2"
pt bom show @1 --unresolved
pt bom show @1 --revision rev-2 --fields refdes,qty,value,mpn,state
```

Columns: `id, refdes, qty, value, footprint, bom_mpn, mpn, mfr, pkg, stock,
state, subs, desc`. `bom_mpn` is what the file said; `mpn` is what it resolved
to, and comparing the two is usually the point.

### Checking stock for a build

```sh
pt bom stock "Power Supply v2" --quantity 10
pt bom stock @1 --quantity 25 --short
```

`--quantity` is required. Build quantity is **ephemeral** — a parameter of the
question, not a property of the project — so there is no default to fall back
on. `--short` keeps only the lines with a shortfall, which is the question you
were actually asking.

The header reports `fully buildable`: how many complete boards current stock
covers, which is a different number from "is line N short". Per line you get
need, on hand, reserved (by other builds), free, on order and short. A line
fitted with an approved substitute names the substitute, not the primary part.

### Orders, builds, the cart

```sh
pt orders list --status open           # every status that is not yet terminal
pt orders show @1
pt builds list --project "Power Supply v2" --status pending
pt builds pipeline                     # active builds, grouped into their four stages
pt builds show @1 --preview            # what it will consume, line by line
pt cart show --demands                 # …and which pending builds asked for it
pt checkouts list
```

`--status open` on `pt orders list` is not a server-side value: it expands to
`draft,exported,submitted,confirmed,shipped`, one request each, merged and
re-sorted newest-first.

`pt cart show` marks each listing `auto` or `by hand`. An `auto` listing was
picked by the team's sourcing policy, and `--fields …,reason` says which rule
applied — plus `(stale stock)` when a preference was honoured against stock data
old enough that the server flagged it. An item with **no** listing cannot be
ordered at all, and is called out under the table rather than silently costing
nothing.

`pt builds show --preview` is opt-in because the endpoint behind it costs the
server a query per BOM line.

Two order endpoints are deliberately **not** wired up. `GET …/orders/{id}/export`
transitions the order to `exported` — that is a write, and belongs with the write
commands. `GET …/orders/{id}/distributor-status` calls the distributor and caches
the result onto the order, so it sits behind an explicit
`pt orders show --distributor-status`.

### Columns

`--fields` picks and orders columns for both plain and JSON output. Every list
command has one; `pt <noun> list --help` names its columns, and an unknown field
exits 2 listing the ones that exist.

For `pt search` the default is `mpn,mfr,desc,pkg,stock,loc,status`, and the
available set is `id, uuid, mpn, mfr, desc, category, pkg, status, scope, stock,
min, reorder, loc, tracked`.

`--wide` (`pt search`) stops the description column being truncated.

`--params` adds parameter columns — bare for every parameter in the result set,
or `--params=tolerance,power` for a subset. It **requires `--category`**, because
the server only returns parameter values when a category filter is present.

`--sort <field>[:desc]` sorts the rows you fetched. The parts endpoint always
orders by MPN, so any other order happens client-side; when the result set is
larger than what was fetched, `pt` says so rather than implying a global sort.

An empty `stock` column means the part has **no inventory entry** — a different
statement from holding zero, and the two never render alike. In JSON that is
`null` versus `0`.

### JSON output

Values are JSON-native: quantities are numbers, `tracked` is a boolean, and
anything absent is `null` — never the `—` placeholder used for display.

```sh
pt --format json search --fields mpn,stock,loc | jq '.items[] | select(.stock == null) | .mpn'
pt --format json show @1 | jq '.part.listings[] | {distributor_name, stock_available, unit_price}'
pt --format json stock @1 | jq '.quantity_free'
pt --format json bom stock @1 --quantity 10 | jq '.items[] | select(.shortfall > 0) | .resolved_part_mpn'
pt --format json cart show | jq '[.items[].line] | add'
pt --format json orders list --status open | jq '.items[] | {ref, status, total}'
pt --format json builds show @1 | jq '.build.allocations[] | {part_mpn, quantity_reserved}'
```

## Caching

`pt` caches **only reference data** — the category tree and the parameter
registry — for 30 days, plus the last result set for `@N` addressing. Stock,
price, and location are **never** cached: your server is on your network with no
API quota, and a cached stock figure would defeat the point of asking. Use
`--refresh` to re-fetch reference data, or `pt config clear-cache` to drop it.

This is the deliberate opposite of `dks`, which caches aggressively because
DigiKey rations requests.

## Driving `pt` from an agent

`skills/partrack/SKILL.md` is a Claude skill covering the command surface, the
conventions above, and the three data-model facts that otherwise produce
confident wrong answers (unmapped parameters, inherited class profiles, and
range parameters comparing only the lower bound). Symlink it into
`~/.claude/skills/partrack` to install it.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | error |
| 2 | usage — bad flag, bad `--param`, unknown command |
| 3 | authentication — no token, expired token, forbidden |
| 4 | not found — no such part, category, location, or `@N` out of range |
| 5 | ambiguous — the reference matched several objects; candidates are listed |
| 6 | network — the server could not be reached |

## Known limits

- **Writes are not implemented yet.** Checkouts, deposits, cart operations and
  `pt bom import` are the next increment. This is a matter of what has been
  built, not a design constraint: the client speaks every verb, and
  `pt bom check` already produces the exact payload an import would post.
- **No "where used".** Nothing answers "which projects consume this part", so
  `pt` cannot either. That needs a server-side query first. `pt stock <part>`
  gets close from the other direction: it lists the builds reserving it.
- **Distributor stock and price are not in the search response.** Your own
  inventory rides along free with `GET /parts`; distributor availability costs
  one call per part, so it appears in `pt show`, not in `pt search`.
- **Substitutes, distributor configs, the dashboard and the activity log have no
  commands.** They are read-only surfaces `pt` simply has not reached yet.
- **`pt orders list --status` costs one request per status.** The endpoint takes
  a single status, so `--status open` — five statuses — is five requests, and its
  reported total is their sum rather than a de-duplicated count.
- **BOM revision listings are not paginated server-side.** `pt bom show` fetches
  every line and filters locally; `--limit` clamps what is *printed*, not what is
  fetched.
