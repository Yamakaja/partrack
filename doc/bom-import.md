# BOM Import — Interpretation, Satisfaction & Review Design

This document describes the end-to-end BOM import flow in partrack, with a focus
on how BOM **line items become resolved to Parts** — including the common
prototyping/research case where parts are specified *generically* (a footprint
and a value, e.g. `0603` `10nF`) rather than by a specific MPN or distributor
SKU.

It is a **planning document**. It defines behaviour, states, and
responsibilities for the frontend, backend handlers, background worker, and data
model. It does not prescribe concrete code, but it is meant to be precise enough
to implement against.

Relevant background:

- [architecture.md](architecture.md) — overall system architecture
- [datamodel.md](datamodel.md) — conceptual entities (Projects, BOM Revisions,
  BOM Line Items, Parts, Distributor Listings, Inventory Entries, Parameter
  Definitions, Categories, …)
- [userstory.md](userstory.md) — **Epic 1: BOM Import** and US-1.1/US-1.3
- [work/genspec-parser-examples.yaml](work/genspec-parser-examples.yaml) — the
  parser test corpus (refdes/footprint/value cases with expected interpretations)
- `internal/handler/revisions.go` — `ImportRevision`
- `internal/worker/queue.go` — `JobBOMImport`
- `internal/paramparse/` — SI-aware value parser (the *bottom* of the parse stack)
- `web/src/lib/components/bom/BOMImportDialog.svelte` — CSV import UI

---

## 1. Two Worlds: Identity and Parametric

A BOM line item can describe the part it needs in one of two fundamentally
different ways:

- **By identity** — a Manufacturer Part Number (with manufacturer) or a
  distributor SKU. This names *one specific, catalogable component*. This is the
  manufacturing/production workflow and is fully supported today.
- **Parametrically** — a category (implied by the reference designator), a
  footprint/package, and a value (`10k`, `100nF X7R`, `4k7`). This names a
  *class* of components that many MPNs satisfy. This is the prototyping/research
  workflow: the user grabs whatever 0603 10nF cap is in the drawer.

partrack supports both. The parametric world is mapped onto the **same Part
catalog** as the identity world — a "generic part" is simply a plain
team-scoped `parts` row with no MPN, carrying a category, a package, and a small
set of value parameters (see §7). Nothing in inventory, the stock ledger,
builds, checkout, or stock-checking needs to special-case it.

This document's central job is to describe how a line item from *either* world
is turned into a Part link, with the parametric world handled by a new
**interpretation** phase that precedes resolution.

---

## 2. The Core Model: Interpret → Satisfy

Resolution is split into two phases with a **review gate** between them:

1. **Interpret.** Turn the raw line item (reference designators, footprint,
   value, plus any mpn/sku/manufacturer) into a structured **interpretation**:
   *what does this row mean?* — is it an identity reference or a parametric
   spec, what category, what package, and what constraints (parameters) were
   extracted. This is the output of the parser (§5). It is fuzzy, best-effort,
   and **cheap to review** because it is small and structured.

2. **Review the interpretation (gate).** The user is shown the parser's reading
   of every line and confirms or corrects it (§6). Ideally they change nothing,
   but they can flip the assumed route, fix a mis-extracted value, or correct a
   package. **Nothing touches the Part catalog until the interpretation is
   accepted.**

3. **Satisfy.** Take the *confirmed* interpretation and fulfil it against the
   catalog (§7): for identity rows, match/import the specific Part; for
   parametric rows, find an existing Part meeting the constraints or create a
   generic one. Because the fuzzy step (parsing) has already been validated by a
   human, satisfaction can run largely automatically.

The key principle: **the human checks the cheap structured thing
(interpretation) before any expensive or hard-to-undo thing (creating/linking
parts) happens.** Misparses are caught at the point they are cheapest to fix.

A BOM line item is, after interpretation, effectively a **constraint set**.
Satisfaction is the act of finding or creating a Part that meets the
constraints. Identity rows have one tight constraint (the MPN/SKU); parametric
rows have a small set (`category=Resistor, package=0603, resistance=10k`).

---

## 3. High-Level Flow

Three stages, mapping onto frontend / backend / review responsibilities:

```
 ┌─────────────────────────┐   ┌──────────────────────────┐   ┌─────────────────────────┐
 │ Stage 1: Parse & Map    │   │ Stage 2: Interpret       │   │ Stage 3: Satisfy        │
 │ (frontend, synchronous) │ → │ (backend job) + REVIEW   │ → │ (backend) + REVIEW      │
 │                         │   │ GATE (user confirms      │   │ (auto where possible,   │
 │ CSV → columns → roles   │   │ interpretation)          │   │ review only true gaps)  │
 └─────────────────────────┘   └──────────────────────────┘   └─────────────────────────┘
```

- **Stage 1 — Parse & Map** (`BOMImportDialog.svelte`): the user parses the CSV
  and maps columns to roles. Unchanged except for two new roles (`value`,
  `footprint`) — see §4.
- **Stage 2 — Interpret + Review**: the import job runs the parser over every
  line, producing and persisting a per-line interpretation. The user reviews
  interpretations and accepts them. This is the new primary human gate.
- **Stage 3 — Satisfy + Review**: confirmed interpretations are fulfilled. Most
  rows resolve with no further input; only genuine catalog ambiguity (an MPN
  matching several manufacturers, a distributor import needing a pick) surfaces
  for a second, narrow review.

Stages 2 and 3 may be presented as one continuous review experience in the UI,
but they are conceptually and in the data model distinct.

---

## 4. Stage 1 — Parse & Column Mapping

Responsible component: `web/src/lib/components/bom/BOMImportDialog.svelte`.

The user parses the CSV (delimiter, quoting, comments, header row, skip-N) and
maps each column to a **role**. Roles act as **resolution-strategy selectors**:
the columns the user maps implicitly declare how each row should be interpreted.

```ts
type ColumnRole =
  | 'ignore'
  | 'designators'   // reference designators  → component class + quantity
  | 'quantity'
  // identity columns → identity resolution
  | 'mpn'
  | 'manufacturer'
  | 'sku'
  | 'distributor'
  // parametric columns → parametric/generic resolution   (NEW)
  | 'value'         // EDA value field: "10k", "100nF X7R", "4k7"
  | 'footprint'     // EDA footprint/package: "R_0805_2012Metric...", "0603"
  // metadata
  | 'description'   // verbatim free text, kept for traceability
  | 'name';
```

Notes:

- **`value` is distinct from `description`.** `description` is verbatim free
  text kept for traceability (`raw_description`); `value` is the terse,
  class-aware parametric input that drives the parser. Both may be present.
- **`footprint` is a single role** covering both messy EDA footprint strings and
  clean package codes; the parser normalises either to a canonical imperial
  package (§5.2).
- **Roles are not mutually exclusive per row.** A BOM may carry both an MPN and a
  Value. Identity wins (the MPN resolves the row), but the parsed value still
  **enriches and validates** (we know it should be 10k; a mismatch can be
  flagged). A pure prototyping BOM maps only `designators`, `quantity`, `value`,
  `footprint` and has no MPN at all — the headline parametric case.
- The distributor-name mapping sub-step (textual distributor → `distributors.id`)
  only appears when a `distributor`/`sku` column is mapped. Unchanged.

On confirm, the frontend POSTs to
`POST /teams/{teamId}/projects/{projectId}/revisions` with `label`,
`set_active`, `bom_import_config` (parse + column mapping, persisted on the
Project), and `items[]`.

---

## 5. Stage 2 — Interpretation (the Parser)

The parser turns each raw line into a structured interpretation. It lives
backend-side (a new package, working name `genspec`, layered above
`paramparse`) so it is reusable by import, manual entry, and scanning. It is a
**best-effort token scanner, not a strict grammar**: it extracts whatever it can
and leaves the rest null for review.

### 5.1 Classification — class supplies the unit

The decisive first step. **`5k` is 5 kΩ only because the reference designator is
`R`.** The same text on a `C` is meaningless. So the class must be resolved
before the value field can be interpreted.

- **Reference designator prefix** → component class (primary signal):
  `R`→Resistor, `C`→Capacitor, `L`→Inductor, `D`→Diode, `LED`→LED, `FB`→Ferrite
  Bead, `Y`/`X`→Crystal, `U`→IC, `Q`→Transistor, `J`/`P`→Connector, `F`→Fuse,
  `TP`/`FID`/`MH`/`H`→virtual. Two-letter prefixes (`FB`, `JP`) must be tested
  before single-letter ones.
- **Footprint class token** corroborates (`R_…`, `C_…`, `LED_…`). Agreement →
  high confidence; conflict (refdes `R` vs footprint `C_…`) → flagged for review.
- The class resolves to a **partrack category** (§8) and, via that category's
  **class profile** (§8.2), to a *primary parameter* (the magnitude the value
  field encodes) and a set of *recognised secondary parameters*.

### 5.2 Footprint → package — structured scan, not naive substring

Extract a **canonical package** (§5.2.1) plus mounting type. Chip-size packages
normalise to an **imperial** size code; other families keep a cleaned standard
name. A naive substring search is wrong because of the metric twin:

```
R_0201_0603Metric   →  imperial 0201   (the 0603 is the METRIC twin!)
R_0805_2012Metric   →  imperial 0805
R_Shunt_..._WSK2512_6332Metric  →  imperial 2512
```

Rules:

- The imperial code is the size token immediately **before** `_<NNNN>Metric`.
- A standalone `<NNNN>Metric` (or an IPC name like `RESC1608…`) is metric → map
  to imperial via the pairing table below.
- **Imperial codes are variable length** — not always four digits. Modern parts
  go down to `01005` (5 digits) and `008004` (6 digits), so the scanner must not
  assume a fixed width.
- **A bare code is genuinely ambiguous** between imperial and metric, and the
  ambiguity now *cascades*: `0402` is imperial (1005 metric) **and** the metric
  twin of imperial `01005`; `0603` is imperial **and** the metric twin of `0201`.
  With no `_Metric` structural cue, default to the **imperial** reading when the
  code is in the common imperial set (unambiguous 5/6-digit codes like `01005`
  need no guess).
- Metric↔imperial pairing (extend as needed), metric → imperial:
  `0201→008004, 0402→01005, 0603→0201, 1005→0402, 1608→0603, 2012→0805,
   3216→1206, 3225→1210, 4532→1812, 5025→2010, 6332→2512`. Note `0402`/`0603`
   appear on *both* sides — hence the structural-cue rule above.
- Through-hole / array / active-device footprints (`R_Axial_DIN0207…`,
  `R_Array_…`, `SOT-23`, `SOIC-8`) have no chip size → canonical package is the
  cleaned family name, mounting set (`tht`/`smd`), no invented size.
- Always retain the raw footprint string verbatim for traceability.

If no `footprint` column was mapped, the parser may also scan the `value` and
`description` text for a package token (lower confidence).

#### 5.2.1 Canonical package

The chip-size set is **not** the whole story — partrack catalogs every package
family (chip, SOT/SOIC/QFN/BGA, axial/radial, modules, …). "Canonical package"
means a **normalised package identifier**, of which imperial chip codes are one
(important) sub-case; other families normalise to a standard token (strip
library prefixes and `HandSolder`/pad variants → `SOT-23`, etc.).

This normaliser must run on **both sides** of any package comparison. Today
`parts.package` is free text, exact-matched (`package = $1`), and enrichment
stores distributor-formatted strings like `"0603 (1608 Metric)"` — so a generic
part with `package = "0603"` would **never** match. The fix is a stored
`canonical_package` (or normalise-at-query), populated by the *same* normaliser
on distributor enrichment **and** on generic-part creation, and used by the
satisfaction search. **Required, not optional** (§14).

### 5.3 Value → parameters — class-aware, multi-parameter

Given the class's primary parameter and unit, parse the value field into one or
more parameters:

- **RKM notation** (the prefix replaces the decimal point): `4k7`→4700,
  `4R7`→4.7, `0R22`→0.22, `1M5`→1.5e6, `4u7`→4.7e-6, `4n7`→4.7e-9. *(New;
  belongs in `paramparse` so every caller benefits — see §5.5.)* Note `1M5` is
  mega (ohms) for a resistor, while `m` elsewhere is milli — the class/position
  disambiguates.
- **Plain SI**: `5k`, `9 µF`, `2mOhm`, `9Meg`, `100n` — once the unit is supplied
  by the class.
- **Compound specs**: tokenise and recognise secondaries — `\d+%`→`tolerance`,
  `X7R|C0G|NP0|Y5V`→`dielectric`, a bare `\d+V`→`voltage_rating`,
  `1/16W`→`power_rating`. `10 nF X7R` → `{capacitance: 10n, dielectric: X7R}`;
  `9k 1%` → `{resistance: 9k, tolerance: 1}`. A package token leaking into the
  value field (`10k 0402 1/16W`) is extracted as package.

### 5.4 The MPN escape hatch

If the value field looks like an identifier rather than a magnitude
(`1N4148`, `LM358`, `STM32F103C8T6`, `WS2812B`, a full passive MPN like
`GRM188R71C104KA01D`), the interpretation route is set to **identity**, not
parametric, and the value is handed to MPN resolution. This judgement is part of
the interpretation and is **user-overridable** at the review gate.

### 5.5 Relationship to `paramparse`

`paramparse.Parse(text, valueType)` is a *value* parser: given a kind it extracts
an SI number (handling SI prefixes, fractions, ppm/ppb, ranges, dual-unit,
conditional `@` specs). It does **not** classify, does **not** handle RKM, and
does **not** split compound strings. The `genspec` layer sits above it: it
classifies, splits the value into `(slug, raw)` pairs, and calls
`paramparse.Parse` on each. RKM support is added *into* `paramparse`.

### 5.6 Interpretation output

For each line item the parser produces:

```jsonc
{
  "route": "parametric",         // identity | parametric | ignore | unknown
  "category_id": 30,             // resolved partrack category (Resistors)
  "class_confidence": 0.95,
  "package": "0603",             // canonical imperial, or descriptive, or null
  "mounting": "smd",             // smd | tht | null
  "params": [                    // extracted constraints
    { "slug": "resistance", "raw": "10k", "value_si": 10000 },
    { "slug": "tolerance",  "raw": "1%",  "value_si": 1 }
  ],
  "flags": ["class_footprint_conflict"],   // anything needing attention
  "source": "auto"               // auto | user (set to user once edited)
}
```

`route = unknown` means nothing reliable was extractable (e.g. a value with no
class to give it a unit) — the row needs human attention but the parser declines
to guess.

---

## 6. The Interpretation Review Gate

A new review step (the primary human gate). The user is shown the parser's
reading of every line and accepts or corrects it **before any catalog
satisfaction runs**.

Per line, the interpretation card shows:

- **Route / assumption** — *Specific (identity)* · *Parametric/Generic* ·
  *Ignore* — **togglable**. The user can flip a row the parser guessed wrong
  (e.g. "that `1N4148` in the value column *is* the part — treat as identity",
  or "this isn't generic, I want a specific part here").
- **Category, package, mounting** — editable; category from a picker (§8).
- **The constraint list** — each parameter with value + unit
  (`resistance = 10 kΩ`, `tolerance = 1%`), editable: add, remove, correct.
- **Confidence & flags** — low-confidence parses and conflicts (class↔footprint
  mismatch, metric-trap ambiguity) surfaced for attention.
- **Raw inputs** alongside, for reference.

UX requirements:

- **One dense list, sorted attention-first.** Show *every* line (a "wall" is
  fine), ordered by confidence/flags ascending: problematic and low-confidence
  rows at the top to deal with first, the confident rows below to scroll through
  and spot-check (parses can be wrong even when confident — §14.5). Do **not**
  hide the confident rows.
- **Bulk operations** — group identical/similar interpretations ("12 lines →
  generic 0603 parts") and offer *Accept all* / *Create generic parts* across a
  selection. Generic interpretation is high-volume and repetitive; batch
  affordances are essential.
- Editing a line sets its interpretation `source = user`; subsequent re-parses
  must never clobber a user-edited interpretation (§9).

Once the user accepts, the confirmed interpretations are frozen and Stage 3 runs.

---

## 7. Stage 3 — Satisfaction

Fulfil each confirmed interpretation against the catalog. Because interpretation
is already validated, this is mostly deterministic.

### 7.1 Identity route

Existing behaviour, conservative and automatic where clear-cut:

1. **By SKU** — `(distributor, sku)` lookup against `distributor_listings`;
   import the listing via the distributor API if absent and the team has an
   enabled config. Exactly-one → resolve; otherwise ambiguous → review.
2. **By MPN (+ manufacturer)** — exact match in `parts` (team + global scope);
   single candidate → resolve; multiple → ambiguous; none → optional distributor
   `EnrichPart`/search to import. (See §10 for the per-tier state.)

### 7.2 Parametric route — match-only (never auto-create)

The confirmed interpretation is a constraint set `{category, package, params}`.

1. **Search the catalog** for Parts satisfying the constraints, using the
   existing parametric search (`FindPartIDsByParameterRange` etc.) with the
   value/tolerance policy of §7.5 and canonical-package comparison (§5.2.1).
   Generic and specific Parts are both candidates.
   - **Exactly one match** → resolve to it (auto, `auto_generic`).
   - **Multiple matches** → present the candidates; the user picks.
   - **No match** → **leave the line unresolved**. The review surfaces it with an
     explicit *Create generic part* action (§7.3).
2. On resolution, ensure an `inventory_entries` row exists for `(team, part)`
   (quantity 0), as for any resolved line.

**Satisfaction never writes new Parts.** Auto-creating Parts from fuzzy parses
would silently pollute the catalog, and a single BOM's many identical passives
would race to create duplicates of each other. Generic-part creation is therefore
always an explicit, user-initiated action (§7.3) — which, as a side benefit,
dissolves the in-batch dedup race entirely (there is nothing to race).

### 7.3 Creating a generic Part (explicit user action)

Generic-part creation happens only when the user asks for it in review — never
automatically (§7.2). It can be done per row or in bulk (select several
unresolved parametric rows → *Create generic parts*). The UI shows any existing
parametric matches first, so the user **links instead of duplicating**.

A generic Part is an ordinary `parts` row — no new entity, no `kind`
discriminator:

- `scope = 'team'`, `team_id` set, `mpn = NULL`, `manufacturer = NULL`.
- `category_id`, `package`, and **`canonical_package`** (§5.2.1) from the
  interpretation.
- A user-sourced `part_parameters` row per extracted constraint, written through
  the **existing** user-param write path so each carries its canonical
  `parameter_id` + parsed `numeric_value` and is immediately searchable — this is
  what later lets the same spec on a future BOM *match* this part.
- `lifecycle_status = 'active'`; an `inventory_entries` row at quantity 0.

Creation is **local and synchronous** — unlike `unknown_part_import` (which needs
distributor enrichment), a generic Part is fully described by the interpretation,
so no background job is required. Because creation is user-initiated and
low-concurrency, a **create-or-get** keyed by the normalised constraint tuple
(category + canonical_package + sorted params) is enough to prevent accidental
duplicates; no DB-level uniqueness constraint is required.

The **kit / E-series generator** (a separate tool) is the bulk way to
*pre-populate* the generic catalog, so import-time satisfaction increasingly
*finds* generics rather than leaving them unresolved.

### 7.4 Matching semantics (why generic "just works")

Matching is the existing parametric intersection, which is **closed-world**: a
filter on a parameter only returns parts that *have* that parameter in range. So:

- A generic `{Resistor, 0603, 10k}` (no tolerance) **matches** a loose query
  `{Resistor, 0603, 10k}` and is **excluded** by a stricter
  `{…, tolerance=1%}` query, because it has no tolerance row to satisfy the
  filter.
- Symmetry falls out for free: *a generic BOM line matches a generic part; a
  fully-specified line demands a specific part.* You never silently satisfy a 1%
  requirement with a part of unknown tolerance.

No new matching code is required.

### 7.5 Value & tolerance matching policy

Matching a parsed magnitude against catalog Parts is **not** exact equality —
values parse with rounding, and real parts carry their own tolerance. Defaults
(tunable):

- **BOM line with no tolerance** (a bare generic spec like `10k`): accept a
  candidate whose value is within **±1%** of the nominal (the default epsilon).
  `10k` → `[9 900, 10 100] Ω`.
- **BOM line with a tolerance `T`** (e.g. `1k 10%`), judged on two axes:
  - **Value proximity** — candidate *nominal* within `0.1 × T` of the BOM
    nominal (for `T = 10%`, ±1%; for `T = 1%`, ±0.1%). So a `1010 Ω` part is
    acceptable for `1k 10%`.
  - **Tolerance similarity** — candidate tolerance must **meet** the spec (no
    looser than `T`) but should be **similar**, not wastefully tighter: don't
    spend `0.1%` parts to satisfy a `10%` generic. Prefer same tolerance class;
    allow modestly tighter.
- **Closed-world still holds** (§7.4): a generic part with *no* tolerance
  parameter is matched only by no-tolerance BOM lines (via the ±1% epsilon); a
  tolerance-bearing line filters it out.

These thresholds share DNA with the deferred order-generation/substitution
selection logic; here they apply only to *matching a BOM line to a Part*, and
live as tunable defaults (candidate epsilon, proximity factor, tolerance band).

---

## 8. Generic Part Classes & Parameter Mapping

This is where the abstract parser meets partrack's concrete reference data. It is
also the area with the most open design choices (§15).

### 8.1 Classes are Categories

A "class" (Resistor, Capacitor, Inductor, …) is a row in the **`categories`**
table (606 seeded, hierarchical via `parent_id`, DigiKey-derived; e.g. Resistors
= 30, Capacitors = 5, Inductors/Coils/Chokes = 15, Crystals/Oscillators = 8).
Classification (§5.1) resolves a reference designator to a category. For generic
parts the **broad top-level category** is normally sufficient (a generic
"0603 10k" is simply category *Resistors*); a finer subcategory is optional.

`distributor_category_mappings` already maps distributor categories → partrack
categories; the refdes→category mapping for generics is a small, separate,
mostly-static table (refdes prefix → category slug).

### 8.2 The class profile: `primary_params` + a new `value_param`

The class profile combines two pieces of category data:

- **`categories.primary_params`** (existing; a `JSONB` ordered array of parameter
  slugs, exposed as `string[]`, **currently empty for all seeded categories**) —
  the parameters that matter for the category. It already serves the parts-search
  UI (primary filter columns); here it doubles as the set of **recognised
  secondary parameters** for compound-value splitting. E.g. Resistors →
  `["resistance", "tolerance", "power_rating", "package"]`, Capacitors →
  `["capacitance", "voltage_rating", "dielectric", "tolerance", "package"]`.
  Existing `string[]` consumers are untouched.
- **`categories.value_param`** (new; a **nullable** parameter slug) — names the
  single **primary parameter** that the bare value field encodes (`resistance`
  for R, `capacitance` for C). `value_param IS NULL` means the category has no
  parametric magnitude, so the value is treated as an identity/text reference and
  not parsed — this doubles as the per-class **parametric-vs-identity signal**
  (§5.4). A row like an IC or connector leaves it null.

Combined with the **`parameter_definitions`** registry (which supplies each
slug's unit and `value_type`), this gives the parser everything it needs:

- the **primary magnitude** — `value_param` plus its unit/value_type;
- the **recognised secondary parameters** — `primary_params`, used to gate the
  global token recognizers (§5.3) by plausibility;
- the **unit and value_type** for each — from `parameter_definitions`.

So the class-profile primitive needs **no new table** — one nullable column
(`value_param`) plus the existing registry and `primary_params`.

### 8.3 Parameter registry alignment

Extracted parameter slugs must exist in `parameter_definitions`. The seed
(ids 1–20) already covers `resistance`, `capacitance`, `inductance`,
`voltage_rating`, `power_rating`, `tolerance`, `frequency`, `dielectric`,
`mounting_type`, `package`, etc. Gaps the parser implies (e.g. LED `color`) must
be added to the registry so the parser's output slugs and the registry do not
drift.

### 8.4 Controlling the mapping

Whether and how the class→parameter mapping is editable is an open question
(§15), but the precedent and surfaces exist:

- `distributor_parameter_mappings` is already **admin-curated** (the
  `/admin/parameters` UI lets admins map distributor params → canonical params).
- `categories.primary_params` is global category data; making it
  admin-editable (a category/parameter admin screen) mirrors that precedent.
- The open question is the **scope of control**: global-only (admin), or
  team-overridable, and whether per-category parsing hints (RKM on/off, which
  secondaries to recognise) belong here too.

---

## 9. Persistence — Remembering How Things Were Parsed

Two layers, mirroring how column mappings are already remembered in
`bom_import_config`.

### 9.1 Per-line interpretation (on `bom_line_items`)

`bom_line_items` gains:

- `value TEXT`, `footprint TEXT` — the raw mapped inputs (traceability,
  re-resolution).
- `interpretation JSONB` — the structured interpretation of §5.6 (route,
  category_id, package, mounting, params, flags).
- `interpretation_source TEXT` (`auto` | `user`) — whether a human edited it.

Rules:

- The interpretation is written by the import job and updated by the review gate.
- Re-running interpretation or satisfaction **must not clobber** a row whose
  `interpretation_source = 'user'`.
- Existing resolution columns (`resolved_part_id`, `resolution_state`,
  `ignored`, `resolve_item_id`) continue to record the *satisfaction* outcome.

### 9.2 Interpretation reuse across revisions (implemented)

The prototyping workflow iterates BOMs constantly (Rev A → B → C); re-reviewing
the same 40 passives every revision would be adoption-killing friction. So
interpretation reuse runs in two layers, both keyed by `(class, normalised
value, normalised footprint)` (`genspec.NormalizeKey` folds case/whitespace; the
class comes from the reference designator):

- **Prior-revision carryover** — on import, seed each new line's interpretation
  from the matching line in the project's previous revision, preserving
  `interpretation_source` so a user edit survives and an already-reviewed line
  does not re-flag.
- **Team-level interpretation memory** — a team-scoped table
  (`bom_interpretation_memory`) mapping the key → a confirmed interpretation,
  consulted across projects. It is written whenever the review gate persists a
  `user` interpretation. Once a user corrects the metric trap on
  `R_0201_0603Metric` to package `0201`, that correction is reused on every
  future BOM and the system gets *less* wrong over time. This is to per-line
  interpretation what `bom_import_config` is to a one-off column map.

Per-line precedence on import (when the client sends no override):
prior-revision **user** edit → team memory → prior-revision **auto** reading →
fresh parse (see `handler.resolveReusedInterpretation`). The shared resolver also
backs `POST /bom/interpret-preview`: passing `team_id` layers team memory over
the fresh parse and flags reused rows, so the **review gate shows exactly what
import will persist** rather than a parse the import would then silently
override. (Preview consults only team memory: every prior-revision *user* edit is
also in team memory, and a prior *auto* reading equals the fresh parse, so team
memory reproduces every *visible* reuse.)

> Note: because the parser is deterministic, prior-revision *auto* carryover
> equals a fresh parse; the value of reuse is almost entirely in replaying *user*
> corrections via team memory. Carryover is retained for the carried `user`-edit
> case and as a structural match to this design.

---

## 10. Resolution State Model

Each `bom_line_items` row carries `resolution_state` and `ignored` (migration
`000004`). The state records the *satisfaction* outcome; the interpretation is
recorded separately (§9.1).

| `resolution_state` | `resolved_part_id` | Meaning |
|---|---|---|
| `auto_sku` | set | Satisfied via exact `(distributor, sku)` listing lookup. |
| `auto_mpn` | set | Satisfied via a single `parts` MPN match. |
| `auto_generic` | set | **(new)** Parametric route auto-resolved to a single **existing** catalog match. Satisfaction never *creates* — unmatched parametric rows stay unresolved (`null`) with a *Create generic* action; a user-created generic resolves as `manual`. |
| `ambiguous` | null | Multiple candidates (MPN or parametric); awaiting user pick. |
| `manual` | set | User explicitly linked/picked the Part in review. |
| `null` | null | Unresolved; needs attention (ignore / link / import / create). |
| `ignored` (flag) | null | Excluded from stock checks and shortfall/cart flows. |

A line is **ready** when `resolved_part_id IS NOT NULL OR ignored = TRUE`.

---

## 11. Completion Criteria

A BOM revision is **ready for stock checking** when, for every `bom_line_items`
row, either `resolved_part_id` is non-null or `ignored = TRUE`. The UI surfaces
this as a status badge and should warn before stock-checking a revision that is
not ready.

Note the two gates: a revision cannot reach "ready" until (a) all interpretations
are accepted and (b) satisfaction has resolved or the user has ignored each line.

---

## 12. Downstream Integration

- **Stock checks / builds / checkout** — operate on resolved line items
  identically regardless of whether the Part is generic or specific. Generic
  parts have inventory entries and stock ledgers like any other Part. Line items
  resolving to the same Part are merged for required-vs-on-hand computation.
- **Shortfall → Cart / Orders** — a generic Part has **no Distributor Listing**,
  so it cannot ride the normal cart→order flow directly. When a generic part is
  short, the cart needs a **binding moment**: the user picks a concrete listing
  to fulfil it (order the real SKU), and on receipt may choose to receive it into
  the generic part's inventory entry (treating it as a fill of the generic bin).
  This keeps purchasing exact while keeping stock fungible. *(Detailed cart
  behaviour is out of scope for this doc; flagged here as a known interface.)*

---

## 13. Implementation & Data Flow

The genspec parser lives **backend-side** (a new package above `paramparse`), not
in the frontend like CSV parsing, for four reasons: it is reused by import, bulk
scanning, and manual entry; its rules depend on backend data (the `categories`
class profiles and the `parameter_definitions` registry); it extends Go
`paramparse` (RKM, compound splitting); and it is table-tested against
[work/genspec-parser-examples.yaml](work/genspec-parser-examples.yaml).

Division of labour:

| Step | Where | Sync? |
|---|---|---|
| CSV → rows | frontend (PapaParse) | sync |
| rows → interpretation **preview** | backend, `POST …/bom/interpret-preview` (not persisted; team memory layered when `team_id` given) | sync (regex-cheap) |
| rows → persisted interpretation | backend, inline in `ImportRevision` (with §9.2 reuse) | sync |
| interpretation → satisfaction | backend `JobBOMImport` (catalog search, distributor calls) | async |

The **preview endpoint** keeps the live-feedback nicety of frontend parsing
without duplicating logic: while still on the mapping screen, the frontend posts
the mapped rows and renders how each was interpreted, so the user can fix the
column mapping *before* committing. The same parser code then runs the persisted
pass, and the same reuse resolver (§9.2) backs both, so the preview reflects the
team's remembered corrections that import will persist. Interpretation is cheap
and stays synchronous; only satisfaction (which may hit distributor APIs) is
async, as today.

---

## 14. Risks, Failure Modes & Required Work

Stress-testing surfaced several issues. The first four are **required work** (the
plan is otherwise wrong or insufficient); the rest are guards.

### 14.1 Package canonicalization (required)
Verified hole: `parts.package` is free text, exact-matched, and enrichment stores
`"0603 (1608 Metric)"`, so a generic `"0603"` never matches. Needs a
`canonical_package` normaliser applied on enrichment **and** generic creation and
in the satisfaction search (§5.2.1), handling variable-length imperial codes
(`01005`, `008004`) and non-chip families.

### 14.2 Batch dedup — dissolved by no-auto-create
A BOM's many identical passives would have raced to create duplicate generics.
Because satisfaction is now **match-only** and creation is an explicit,
low-concurrency user action with create-or-get (§7.2–7.3), the race is gone. The
only residual — a user creating a duplicate of an existing generic — is prevented
by showing existing matches before creating.

### 14.3 Value/tolerance epsilon (required)
Exact SI equality is fragile; satisfaction must use the §7.5 policy (±1% default
epsilon; range + proximity + tolerance-similarity for tolerance-bearing lines).

### 14.4 Interpretation reuse (implemented)
Without cross-revision reuse, every revision re-reviews the same passives.
Implemented per §9.2: prior-revision carryover + a team-scoped
`bom_interpretation_memory`, shared between the import and the preview so the
review gate shows what import will persist.

### 14.5 Silent wrong value — limited defence
A confident misparse that looks plausible can pass a skimmed review, and there is
**no** real defence against an in-range typo (`100` meant as `100k`). The cheap
guard is **per-class plausibility ranges**: flag values outside a sane band
(capacitance ≳ 10 mF, or a signal resistor in the µΩ range, are implausible) as
low-confidence so they sort to the top (§6). Render parsed values in **human
units** (`10 kΩ`, not `10000`) so a wrong unit is visually obvious. Beyond that,
the dense problems-first review is the backstop; we accept some typos will pass.

### 14.6 Two review surfaces
The interpretation gate and the satisfaction-ambiguity review should be **one
continuous review screen** (rows flow needs-confirm → needs-pick → done), not two
separate passes.

---

## 15. Open Design Questions

1. **Class profile population.** *Decided:* the primary magnitude is named by a
   new nullable `categories.value_param`; `primary_params` stays the (display +
   secondary-recognition) slug list, untouched. *Open:* who populates
   `value_param` / `primary_params` for the parametric classes initially (a seed
   migration covering ~6–10 categories), and whether per-category parsing hints
   (RKM toggle, recognised-secondary overrides) are needed beyond the global
   recognizers.
2. **Scope of mapping control.** *Decided:* global, admin-editable to start
   (mirroring the admin-curated distributor parameter mappings); team override
   deferred until a concrete need. *Open:* whether per-category parsing hints
   (recognised secondaries, RKM toggle) live in the same admin surface.
3. **Refdes → category table.** Static built-in vs admin-editable; how to handle
   ambiguous prefixes (`D` = diode or LED; `X` = crystal/oscillator/connector).
4. **Registry gaps.** Add `color` and any other slugs the parser emits to
   `parameter_definitions`.
5. **Parsing profiles (KiCad/Altium).** Recommended: *not* a gate; an optional
   hint persisted in `bom_import_config`, used only when a real ambiguity needs
   it. The fuzzy baseline is tool-independent by design.
6. **Generic identity / dedup.** *Resolved:* satisfaction is match-only (§7.2)
   and creation is an explicit user action with create-or-get (§7.3); closed-world
   matching deliberately treats a 10k 0603 *with* a tolerance as a different part
   from one *without* (confirmed desired). No DB uniqueness constraint needed.
7. **Cart binding for generics** (§12) — the concrete UX for ordering against a
   generic part.

---

## 16. SSE & Progress

`JobBOMImport` continues to publish `bom_import_progress` SSE events. The final
event's breakdown gains a parametric bucket, e.g.:

```jsonc
{
  "status": "complete",
  "interpreted": 42,
  "route_identity": 8,
  "route_parametric": 30,
  "route_ignore": 2,
  "route_unknown": 2,
  "job_id": "…"
}
```

Satisfaction outcomes (auto_sku / auto_mpn / auto_generic / ambiguous /
unresolved) are reported after the interpretation gate is cleared.

---

## 17. Summary

- BOM import is split into **Parse & Map → Interpret (+review gate) → Satisfy
  (+narrow review)**.
- Line items live in two worlds — **identity** (MPN/SKU) and **parametric**
  (footprint + value) — both mapped onto the same Part catalog. A generic part is
  a plain team-scoped `parts` row with no MPN.
- The new **interpretation** phase parses the fuzzy parametric inputs into a
  structured, user-confirmed constraint set *before* any catalog changes happen,
  so misparses are caught cheaply.
- **Satisfaction** is constraint-satisfaction and **match-only**: identity rows
  match/import a specific Part; parametric rows match an existing Part (generic or
  specific) or stay unresolved — generic Parts are created only by an explicit
  user action, never automatically. Matching is the existing closed-world
  parametric search (with a value/tolerance epsilon and canonical-package
  comparison) — no new matching code.
- Classes are **categories**; the per-class parameter profile lives in the
  existing-but-empty **`categories.primary_params`** plus a new nullable
  **`categories.value_param`** and the `parameter_definitions` registry — no new
  table, with control mirroring the admin-curated distributor parameter mappings.
- Interpretations are **persisted** per line (and, as a fast-follow, learned at
  team level), mirroring how `bom_import_config` already remembers the column map.
