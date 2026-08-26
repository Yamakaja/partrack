# `pt` — the partrack command-line client

`pt` queries your partrack catalog and inventory from a terminal: parametric part
search, stock levels and locations, distributor listings, notes and datasheets.
It is built for non-interactive use — plain column-aligned text by default,
`--format json` for anything that gets parsed, data on stdout and diagnostics on
stderr.

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

```sh
pt config show          # resolved settings, with where the token came from
pt config set team Lab
pt config path
pt config clear-cache
```

Settings live in `config.json`; the token lives in a separate `credentials.json`
at mode `0600`, so the config file never holds a secret and is safe to share.

## Addressing a part

Every command that takes a part accepts, tried in this order:

| Form | Meaning |
|---|---|
| `@3` | row 3 of your last `pt search` |
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

## Commands

| Command | Purpose |
|---|---|
| `pt search [query]` | search the catalog and your inventory |
| `pt filters <category>` | the parameters carried by parts in a category, with the values in your catalog |
| `pt categories [query]` | browse the category tree with your part counts |
| `pt show <part>` | full dossier: identity, inventory, parameters, listings, note, reservations |
| `pt stock <part>` | on-hand vs reserved vs free, thresholds, location, ledger |
| `pt where [part]` | locate a part, list a location's contents, or print the storage tree |
| `pt teams` | the teams you belong to |
| `pt auth` / `pt config` | credentials and settings |

Two flags take an **optional** value and so require it attached:
`--history=20` (`pt stock`) and `--params=tolerance,power` (`pt search`).
Bare `--history` / `--params` use their defaults; detached, the value is left
behind as a stray argument and the command exits 2 saying so.

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

### Columns

`--fields` picks and orders columns for both plain and JSON output. The default
is `mpn,mfr,desc,pkg,stock,loc,status`. Available: `id, uuid, mpn, mfr, desc,
category, pkg, status, scope, stock, min, reorder, loc, tracked`.

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

- **No "where used".** Nothing answers "which projects consume this part", so
  `pt` cannot either. That needs a server-side query first.
- **Distributor stock and price are not in the search response.** Your own
  inventory rides along free with `GET /parts`; distributor availability costs
  one call per part, so it appears in `pt show`, not in `pt search`.
- **Writes are not implemented yet.** `pt` is read-only; checkouts, deposits and
  cart operations are the next increment.
