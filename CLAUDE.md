# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Status

partrack is currently in the **architecture and design phase**. No application code exists yet. The repository contains design documentation that must be consulted before making any implementation decisions.

## Design Documents

Read these in order when onboarding to the project:

| File | Purpose |
|---|---|
| [userstory.md](userstory.md) | User stories organised into 6 epics. Defines what the system must do. |
| [datamodel.md](datamodel.md) | Conceptual data model — entities, relationships, and design decisions. Start here before touching any schema or data layer. |
| [architecture.md](architecture.md) | Living architecture document. Updated as design sessions progress. |
| [api.md](api.md) | Full REST API endpoint reference — methods, paths, roles, request/response shapes. |

## What partrack Does

An electronics parts inventory tracking system for PCB designers. Core workflow:

1. User imports a BOM from their PCB tool into a Project
2. System resolves BOM line items to Parts (via MPN or distributor SKU match, or distributor API lookup)
3. User sees stock status per line item and shortfalls
4. System generates order lists per distributor to cover shortfalls
5. User receives orders; inventory is updated and parts are enriched with distributor API data

## Key Data Model Concepts

The data model (`datamodel.md`) has no open design questions. Critical things to understand before writing code:

- **Part** (global or team-scoped) is a catalog entry — it exists independently of stock. Identified by MPN where possible. Never deleted; use lifecycle status instead.
- **Inventory Entry** is the team's *profile* for a Part — holds preferred storage location, preferred distributor listing, minimum stock level, reorder quantity, and the stock ledger. Created with zero quantity when a Part first becomes relevant to a team. Not the same as "has stock."
- **Stock Adjustment** is an immutable ledger entry; quantity on hand is a running total derived from these. Source of an adjustment is one of: Order Line Item (receipt), Build (PCB assembly), General Checkout (ad-hoc withdrawal), or null (manual correction).
- **Distributor Listing** is one Distributor × Part × Packaging Type combination. A single MPN can have many listings (different distributors, different packaging, different MOQ and fixed costs).
- **Part Parameters** from multiple distributor sources coexist on a single Part. User-specified values always win; a `preferred_parameter_source` on the Part resolves conflicts between distributor sources.
- **BOM Line Item** resolves to a primary Part plus zero or more approved substitute Parts. Stock checking covers all of them.
- **Build quantity is ephemeral** — it is a parameter at stock-check time, not stored on the Project. It is recorded on a **Build** entity when boards are actually assembled.
- **Shopping Cart** is the team's persistent staging area (one per team) for parts to be ordered, populated from BOM shortfall checks, inventory maintenance scans, or manual additions. Cart items are converted to Orders grouped by distributor.
- **Build** records a PCB assembly event (project + BOM revision + quantity built) and drives stock deductions.
- **General Checkout** records ad-hoc parts withdrawal not tied to a project.

## Architectural Constraints Already Decided

- Parts are never deleted (use lifecycle status: Active, NRND, Obsolete, Deprecated, No Longer Available)
- One storage location per Inventory Entry
- Part scope is `global` or `team` — no user-level scope
- Distributor Listing price/availability data is cached; cache duration is configurable per distributor per team in Distributor Config
- Order generation selection logic is intentionally deferred — the data needed for it is captured in Distributor Listing fields, Distributor Config preferred flag, and Inventory Entry preferred listing reference
- One Shopping Cart per Team; items are merged when added from multiple sources
