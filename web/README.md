# partrack web frontend

Svelte 5 + Vite + Tailwind CSS v4 single-page application. Communicates with the Go backend via REST and Server-Sent Events.

## Stack

| Library | Version | Role |
|---|---|---|
| Svelte 5 | 5.x | Component framework (runes reactivity) |
| TanStack Query | 6.x | Data fetching, caching, and invalidation |
| bits-ui | 2.x | Headless accessible UI primitives |
| Tailwind CSS | 4.x | Utility-first styling via Vite plugin |
| Vite | 7.x | Dev server + production bundler |
| TypeScript | 5.x | Type checking |
| lucide-svelte | — | SVG icons |
| papaparse | — | CSV parsing (BOM imports) |
| zxing-wasm | — | QR/barcode decoding (scanner) |

## Commands

```bash
npm run dev      # dev server on :5173, proxies /api → localhost:8080
npm run build    # production build → dist/
npm run check    # svelte-check + tsc (no emit)
```

## Directory structure

```
src/
├── main.ts                 # mounts App.svelte
├── App.svelte              # root: routing, auth init, QueryClient, SSE lifecycle
├── app.css                 # Tailwind theme tokens + dark mode overrides
├── routes/                 # one file per page, organised by layout context
│   ├── Login.svelte
│   ├── TeamSelect.svelte
│   ├── Scan.svelte         # mobile scanner entry point
│   ├── admin/
│   ├── settings/
│   └── team/               # authenticated, team-scoped pages
│       ├── Dashboard.svelte
│       ├── Parts.svelte / PartDetail.svelte
│       ├── Storage.svelte
│       ├── Projects.svelte / ProjectDetail.svelte
│       ├── RevisionDetail.svelte
│       ├── Cart.svelte
│       ├── Orders.svelte / OrderDetail.svelte
│       ├── Builds.svelte / BuildDetail.svelte
│       ├── Checkouts.svelte
│       └── settings/
├── lib/
│   ├── api/                # HTTP client + per-domain fetch functions
│   ├── cache/              # SSE-driven TanStack Query invalidation
│   ├── components/         # shared components (see below)
│   ├── router/             # custom SPA router + auth guards
│   ├── stores/             # Svelte 5 $state global stores
│   ├── types/              # TypeScript interfaces matching backend JSON shapes
│   └── utils/              # pure helpers (cn, price, date, roles, qr, …)
```

## Routing

The app uses a hand-rolled client-side router (`src/lib/router/index.svelte.ts`), not a file-based framework. Routes are defined as an ordered pattern table in `App.svelte`; the first match wins.

```
/login  /reset-password  /scan              public
/admin/*                                   global admin
/settings/*                                user settings
/teams/:teamId/parts/:partId               part detail
/teams/:teamId/{parts|storage|projects|…}  team pages
/teams/:teamId/settings/*                  team settings
/                                          TeamSelect
```

Navigation is triggered by `navigate(path, replace?)` or by `<a>` elements with the `use:link` action. `popstate` handles browser back/forward.

Route guards live in `src/lib/router/guards.ts` — `requireAuth()` redirects unauthenticated users to `/login`; `requireRole(teamId, minRole)` enforces team role minimums.

## API layer

### Base client (`src/lib/api/client.ts`)

All requests go through a single `request<T>(method, path, body?)` function:

- Session cookies sent with every request (`credentials: 'include'`).
- Non-2xx responses throw `ApiError(status, code, message, data)`. Check `err.code` for machine-readable error codes from the backend.
- 204 responses return `undefined`.

### Domain modules

Each domain has a thin module (`parts.ts`, `orders.ts`, `builds.ts`, …) that exports plain async functions. These are passed directly to TanStack Query's `queryFn` or called inside mutation handlers. There are no wrapper classes.

```typescript
// src/lib/api/parts.ts
export function getPart(id: string, teamId: string): Promise<Part> {
  return request('GET', `/api/v1/teams/${teamId}/parts/${id}`);
}
```

## Data fetching and caching

[TanStack Query](https://tanstack.com/query) manages all server state. `QueryClient` is created in `App.svelte` with `staleTime: 30_000` and mounted via `QueryClientProvider`.

### Query keys

Keys follow a consistent pattern to scope cache entries:

```
['part', partId, teamId]         single entity
['parts', teamId]                collection
['revisions', teamId, projectId] related collection
```

Team ID is always included so switching teams never shows stale data from another team.

### Typical query

```svelte
<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getPart } from '$lib/api/parts';

  let { partId, teamId } = $props();

  const qc = useQueryClient();
  const query = createQuery(() => ({
    queryKey: ['part', partId, teamId],
    queryFn: () => getPart(partId, teamId),
  }));
</script>

{#if $query.isPending}…{:else if $query.isError}…{:else}
  <div>{$query.data.mpn}</div>
{/if}
```

### Mutations

There is no `createMutation` wrapper — mutations are plain async handlers with local loading state:

```typescript
let saving = $state(false);

async function handleSave() {
  saving = true;
  try {
    await updatePart(partId, payload);
    qc.invalidateQueries({ queryKey: ['part', partId, teamId] });
    qc.invalidateQueries({ queryKey: ['parts', teamId] });
    pushToast('success', 'Saved');
  } catch (err: any) {
    pushToast('error', 'Failed', err.message);
  } finally {
    saving = false;
  }
}
```

## Real-time updates (SSE)

The backend publishes lightweight entity-change events over SSE at `/api/v1/teams/{teamId}/events`.

`src/lib/cache/invalidation.ts` subscribes to these events and calls `queryClient.invalidateQueries` automatically, so any open view refreshes without polling. Event types and the query keys they invalidate:

| Event type | Invalidated keys |
|---|---|
| `part_changed` | `['part', id, teamId]`, `['parts', teamId]` |
| `inventory_changed` | `['inventory', …]`, `['parts', teamId]` |
| `cart_changed` | `['cart', teamId]` |
| `order_changed` | `['orders', teamId]`, `['order', id, teamId]` |
| `build_changed` | `['builds', teamId]`, `['build', id, teamId]` |
| `project_changed` | `['projects', teamId]`, … |
| `storage_changed` | `['storage', teamId]` |
| … | … |

Call `activateTeamInvalidation(teamId)` when a team is selected and `deactivateTeamInvalidation()` on team change — this is done in `App.svelte`.

For watching a specific background job (BOM import, enrichment, …) use the lower-level `subscribeSSE(teamId, jobId, callback)` from `src/lib/api/sse.ts`.

## Authentication and session state

On mount, `App.svelte` calls `initAuth()` which hits `GET /api/v1/me`. The result is stored in `authStore` (`src/lib/stores/auth.svelte.ts`):

```typescript
{
  user: User | null;
  memberships: TeamMembership[];
  loading: boolean;
}
```

The current team is tracked separately in `activeTeamStore` (`src/lib/stores/activeTeam.svelte.ts`) and set by the router when navigating into a `/teams/:teamId/*` route.

Role hierarchy: `viewer < member < purchaser < admin`. Check roles with `hasMinRole(userRole, minRole)` from `src/lib/utils/roles.ts` or the router guard `requireRole(teamId, minRole)`.

## Component library overview

### Layout (`src/lib/components/layout/`)

`AppShell` provides the outer shell (sidebar + topbar + main slot) used by every authenticated page. `Topbar` holds breadcrumbs, team switcher, and action buttons. `Sidebar` holds primary navigation. `CommandPalette` provides keyboard-driven search across all entity types.

### DataTable (`src/lib/components/table/`)

The primary way to display list data. Two data sources:

**`LocalDataSource`** — in-memory; built-in sort, filter, and optional CSV export. Good for detail panels and small related lists.

**`RemoteDataSource`** — delegates sort, filter, and pagination to the server. Constructed with a `fetchFn(params) → Promise<{items, total}>`. Good for top-level list views.

Column definitions use `ColumnSpec<T>`:

```typescript
{
  key: 'mpn',
  header: 'MPN',
  sortable: true,
  filterable: true,
  type: 'text',           // 'text' | 'numeric' | 'currency' | 'enum' | 'date' | 'custom'
  filterValues: ['A','B'], // for enum columns
  filterLabels: { A: 'Active' },
}
```

Custom cell rendering via the `renderers` prop — pass a `Record<string, Snippet<[value, row]>>` to `DataTable`:

```svelte
{#snippet statusCell(value, row)}
  <StockBadge status={value} />
{/snippet}

<DataTable {source} renderers={{ stock_status: statusCell }} />
```

### UI primitives (`src/lib/components/ui/`)

Thin Tailwind-styled wrappers around bits-ui primitives: `Button`, `Input`, `Select`, `Dialog`, `DropdownMenu`, `Tabs`, `Badge`, `Skeleton`, `Tooltip`, and more. Import from `$lib/components/ui/button`, etc.

Dialogs use bits-ui `Dialog.Root` with controlled state (`open` + `onOpenChange`). Popovers similarly use controlled mode — never `bind:open` with an object-keyed value (Svelte throws `props_invalid_value` if the key is initially undefined).

### Common (`src/lib/components/common/`)

`Toaster` — renders the global toast queue. Call `pushToast(type, title, detail?)` from `src/lib/stores/toasts.svelte.ts` anywhere.  
`ConfirmDialog` — reusable destructive-action confirmation.  
`DateDisplay`, `PriceDisplay`, `StockBadge` — consistent formatting for common data types.

## Page layout conventions

Every page component follows this structure:

```svelte
<div class="flex flex-col h-full">
  <Topbar title="Page Title">
    {#snippet actions()}…{/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-6">
    <!-- section header -->
    <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">
      Section Title
    </h3>

    <!-- content -->
  </div>
</div>
```

## Styling

Tailwind CSS 4 with a custom design token theme defined in `src/app.css` (`@theme { … }`). Dark mode is toggled via `mode-watcher` which adds/removes `.dark` on `<html>`.

Combine conditional classes with `cn` from `$lib/utils/cn`:

```typescript
import { cn } from '$lib/utils/cn';
cn('px-4 py-2', isActive && 'bg-primary text-white', className)
```

## Key utilities

| Module | Exports |
|---|---|
| `$lib/utils/cn` | `cn(...classes)` — clsx + tailwind-merge |
| `$lib/utils/price` | `formatPrice(decimal, currency)` |
| `$lib/utils/date` | `formatRelative`, `formatDate`, `formatAbsolute` |
| `$lib/utils/roles` | `hasMinRole(role, min)`, `roleIndex(role)` |
| `$lib/utils/qr` | QR code generation for part labels |
| `$lib/utils/designators` | Parses/formats PCB reference designators (R1, C2, …) |
| `$lib/utils/tracking` | Validates carrier tracking numbers |

## Mobile scanner

The `/scan` route is a self-contained mobile inventory management app that runs entirely on a phone browser. It communicates with its own set of backend endpoints (`/api/v1/scan/…`) using a bearer token rather than session cookies — this lets the phone stay logged in independently of the desktop session.

### Authentication and session lifecycle

A scan session is established from the desktop: the user generates a QR code (or link) containing a short-lived token. On the phone:

1. Token arrives in the URL as `?token=…` and is immediately moved to `localStorage` (the URL is scrubbed via `history.replaceState` so the token never appears in browser history).
2. `Scan.svelte` reads the token from `localStorage` on load and attaches it to every request as `Authorization: Bearer <token>`.
3. A `401` or `410` response from any scan endpoint triggers the "session expired" screen; the user must scan a new QR code from the desktop.
4. The current session is held in `scanSessionStore` (`src/lib/stores/scanSession.svelte.ts`): `{ id, teamId, token, connected, liveConnected }`.

### Screen stack and navigation model

The scanner has no URL-based routing. Instead, `Scan.svelte` maintains a `navStack: NavEntry[]` where each entry is a discriminated union describing the current screen and its parameters. Screen transitions are push/pop/replace operations on this array — the last element is always the active screen.

Screens (union variants):

| Screen | Purpose |
|---|---|
| `menu` | Home — choose a workflow, see recent scans |
| `bag_scan` | Scan a distributor bag barcode to look up or import a part |
| `find_part` | Text search across the parts catalog |
| `part_detail` | Part info, stock levels, distributor listings, parameter table |
| `stock_adjustment` | Adjust a part's quantity on hand (delta or absolute set) |
| `receive_order` | Select an open order, scan bags to receive line items |
| `storage_assignment` | Scan bags and assign them to grid storage cells |
| `location_inventory` | Browse all parts stored at a location |
| `build_kitting` | Step through a kitting session, confirming parts for a build |

### Camera decoding

`src/lib/components/mobile-scanner/scanner.svelte.ts` owns the camera. It uses **zxing-wasm** to decode `DataMatrix` and `QRCode` formats from video frames captured via `OffscreenCanvas`.

Two scan modes:

- **Trigger** — single-frame decode on button tap; shows a brief "no barcode" feedback if nothing is found.
- **Continuous** — rapid decode loop while the button is held; yields to the event loop between frames so a `pointerup` can cancel it.

The WASM module is pre-loaded in parallel with the camera permission request to minimise first-scan latency. Camera selection is persisted to `localStorage`; the rear/environment camera is preferred by default. iOS pinch-to-zoom is suppressed on mount.

### Bag scan and lookup flow

`POST /api/v1/scan/lookup` with the raw barcode string. The backend parses it and returns one of:

| Status | Meaning | Action |
|---|---|---|
| `found` | Part is in the catalog | Navigate to `part_detail` |
| `not_found_in_db` | Barcode parsed (SKU/MPN extracted) but part not yet imported | Offer import — auto-import if `can_auto_import`, otherwise show `ListingPicker` for distributor choice |
| `no_fields` | Barcode parsed but no SKU/MPN extracted | Show raw detail; let user retry |
| `parse_error` | Format unrecognised | Show error (only DigiKey and Mouser barcodes are currently supported) |

Import is via `POST /api/v1/scan/parts/import` (single distributor) or `POST /api/v1/scan/parts/import-multi` (multi-distributor picker).

### Receive order flow

The user selects an open order, then scans bags. Each scan hits `POST /api/v1/scan/scans` with `{ order_id, barcode }`. The response status:

| Status | Meaning |
|---|---|
| `matched` | Barcode found in this order; shows qty confirmation |
| `no_match` | Barcode parsed but not in this order |
| `parse_error` | Format unrecognised |

After confirming quantity, `POST /api/v1/scan/confirm` records the receipt. The user can then optionally assign a storage location via `PATCH /api/v1/scan/inventory-entries/{id}`.

### Storage and grid navigation

Storage locations can be hierarchical grids (e.g. Cabinet → Shelf → Tray → Cell). Coordinates are stored as flat integer arrays; `src/lib/components/mobile-scanner/locationUtils.ts` handles decomposition, assembly, and human-readable labelling (`A1`, `A1/B3`, etc.).

`MobileLocationPicker.svelte` provides a tree browser + visual grid selector + QR scan shortcut for jumping directly to a location. `MobileGridVisualiser.svelte` renders the grid with tap-to-select cells and highlights occupied cells.

In the `storage_assignment` workflow the scanner can operate in auto-advance mode: after assigning a part to a cell it immediately returns to the scan screen for the next bag.

### SSE context and desktop integration

`Scan.svelte` opens an SSE stream at `/api/v1/scan/stream?token=…` and polls `GET /api/v1/scan/session` every 10 s. When the desktop pushes a `scan_context_changed` event the phone auto-navigates:

- type `kitting` → `build_kitting` screen (desktop has assigned a kitting session)
- type `order` → `receive_order` screen pre-loaded with the specified order

The phone can also push focus requests back to the desktop: `POST /api/v1/scan/focus` with `{ entity_type, entity_id }` opens the corresponding entity on the desktop via the desktop's own SSE channel (`scan_focus_requested` event consumed by `invalidation.ts`).

### Key files

| File | Purpose |
|---|---|
| `src/routes/Scan.svelte` | Root; auth, session polling, SSE stream, nav stack |
| `src/lib/components/mobile-scanner/scanner.svelte.ts` | Camera + zxing-wasm decode logic |
| `src/lib/components/mobile-scanner/ScanMenu.svelte` | Home screen |
| `src/lib/components/mobile-scanner/BagScan.svelte` | Bag lookup + import |
| `src/lib/components/mobile-scanner/PartDetail.svelte` | Part info (scanner view) |
| `src/lib/components/mobile-scanner/StockAdjustment.svelte` | Inventory delta/set |
| `src/lib/components/mobile-scanner/ReceiveOrder.svelte` | Order receiving |
| `src/lib/components/mobile-scanner/StorageAssignment.svelte` | Grid assignment |
| `src/lib/components/mobile-scanner/BuildKitting.svelte` | Kitting session |
| `src/lib/components/mobile-scanner/FindPart.svelte` | Catalog text search |
| `src/lib/components/mobile-scanner/locationUtils.ts` | Coordinate maths + labels |
| `src/lib/components/mobile-scanner/MobileLocationPicker.svelte` | Tree + grid location modal |
| `src/lib/api/scanner.ts` | All scan API calls |
| `src/lib/stores/scanSession.svelte.ts` | Session token + connection state |
