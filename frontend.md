# partrack — Frontend Design

## 1. Overview

The frontend is a pure **single-page application** (SPA). It is compiled by Vite to a static `dist/` directory and embedded in the Go binary via `//go:embed`. The Go server serves `index.html` as the fallback for all non-API routes, enabling HTML5 history-mode routing.

There is **no server-side rendering** and no SvelteKit. The architecture explicitly rules it out:
> *"SvelteKit is designed for SSR/SSG. Using it as a pure SPA would require `adapter-static` and adds unnecessary complexity."*

---

## 2. Technology Stack

| Concern | Choice | Rationale |
|---|---|---|
| Framework | **Svelte 5 + TypeScript** | Architecture decision; compiles to plain JS, no runtime, runes model is simpler than React hooks |
| Build | **Vite** | Architecture decision; fast dev server, TypeScript built-in |
| CSS | **Tailwind CSS v4** | Utility-first, zero-runtime, co-located with markup, pairs well with component library |
| Component library | **shadcn-svelte** | Composable headless + styled components (built on bits-ui), Tailwind-based, no lock-in — components are copied into the codebase, not installed as opaque packages |
| Routing | **Custom minimal router** | No mature Svelte 5 routing library outside SvelteKit; a ~80-line history-mode router using runes and the History API is simpler than any available third-party option |
| Server state | **@tanstack/svelte-query** | Handles caching, background refetching, optimistic updates, and loading/error states without boilerplate; essential for a data-dense app with many list/detail views |
| HTTP client | **`fetch` + thin wrapper** | No library needed; a typed wrapper provides the base URL, cookie forwarding, and structured error parsing |
| SSE | **Native `EventSource` API** | Architecture decision; no library required |
| Forms | **Manual validation** | Most forms are small (1–4 fields); add a form library if complexity warrants it later |

### Why not `svelte-spa-router`?
It is hash-based and was not updated for Svelte 5. The Go server already handles the history-mode fallback, so there is no advantage.

### Why shadcn-svelte over a custom design system?
partrack is a data-dense, action-heavy tool. Correct, accessible component behaviour (dialogs, dropdowns, command palettes, tooltips) requires significant effort to implement well. shadcn-svelte provides this for free and lets us focus on application logic.

---

## 3. Project Structure

The Svelte project lives in `web/src/`. Vite output goes to `web/dist/` (git-ignored; `.gitkeep` already committed for the Go embed).

```
web/
  dist/                     ← Vite build output (git-ignored; Go-embedded)
  src/
    lib/
      api/                  ← API client layer
        client.ts           ← Base fetch wrapper: base URL, error parsing, cookie auth
        auth.ts             ← /auth/login, /auth/logout, /auth/me, OIDC redirect
        teams.ts            ← Teams, memberships
        distributors.ts     ← Distributor list, team configs
        parts.ts            ← Parts catalog, parameters, listings, enrichment, listing refresh
        storage.ts          ← Storage locations
        inventory.ts        ← Inventory entries, adjustments, replenish preview
        projects.ts         ← Projects
        revisions.ts        ← BOM revisions, line items, substitutes
        stockcheck.ts       ← Stock check (stateless calculation endpoint)
        cart.ts             ← Cart read/write, add-shortfall, add-low-stock, convert
        orders.ts           ← Orders, line items, receipt
        builds.ts           ← Builds
        checkouts.ts        ← General checkouts
        preferences.ts      ← User preferences
        sessions.ts         ← Session list + revoke
        tokens.ts           ← API token list, create, revoke
        sse.ts              ← EventSource lifecycle management
      components/
        ui/                 ← shadcn-svelte primitives (copied in, not installed)
          Button.svelte
          Badge.svelte
          Dialog.svelte
          DropdownMenu.svelte
          Input.svelte
          Select.svelte
          Table.svelte
          Tabs.svelte
          Toast.svelte
          Tooltip.svelte
          Separator.svelte
          Skeleton.svelte
          ...
        layout/             ← App chrome
          AppShell.svelte       ← Root layout: sidebar + main area
          Sidebar.svelte        ← Navigation tree + team switcher
          Topbar.svelte         ← Page title, breadcrumbs, slot for page-level actions
          TeamSwitcher.svelte   ← Dropdown listing user's teams; navigates on select
        common/             ← Reusable domain-agnostic components
          Pagination.svelte     ← Offset-based pagination control
          SearchInput.svelte    ← Debounced text input with clear button
          EmptyState.svelte     ← "No items yet" illustration + CTA
          ConfirmDialog.svelte  ← Generic "Are you sure?" confirmation dialog
          JobProgress.svelte    ← Inline status banner for async jobs (SSE-driven)
          PriceDisplay.svelte   ← Renders a "10.4500" decimal string as formatted currency
          DateDisplay.svelte    ← Formats RFC3339 timestamp to locale string
          CopyButton.svelte     ← Click-to-copy with feedback
        parts/              ← Parts-domain components
          PartSearch.svelte     ← Combobox to search and select a Part by MPN/description
          PartBadge.svelte      ← Compact MPN + description chip
          LifecycleBadge.svelte ← Active / NRND / Obsolete badge
          ParameterTable.svelte ← Grouped parameters display (user / distributor sourced)
          ListingTable.svelte   ← Distributor listings for a part
          ListingPicker.svelte  ← Select a listing from a part's listings
        inventory/
          StockBadge.svelte     ← In Stock / Partial / Out of Stock coloured chip
          AdjustmentHistory.svelte ← Paginated list of stock_adjustments for an entry
        bom/
          BOMLineItemRow.svelte ← Single BOM line item with resolve status + substitute editor
          SubstituteManager.svelte ← Add/remove approved substitutes (PartSearch + list)
          StockCheckRow.svelte  ← Required / on-hand / shortfall row in stock check view
        cart/
          CartLineItem.svelte   ← Editable cart row: qty field + ListingPicker + source badge
          CartSummary.svelte    ← Per-distributor cost breakdown
        orders/
          OrderStatusBadge.svelte ← Draft / Placed / Received etc.
          ReceiveLineItem.svelte  ← Qty received input for order receipt
      stores/               ← Global reactive state
        auth.ts             ← currentUser (User + memberships[]); set on login/me response
        activeTeam.ts       ← teamId + role derived from URL; guards route access
        preferences.ts      ← Writable preferences blob; synced to /api/v1/preferences
        toasts.ts           ← Toast notification queue
      types/                ← TypeScript interfaces mirroring API response shapes
        auth.ts
        teams.ts
        parts.ts
        inventory.ts
        orders.ts
        ...
      router/
        index.ts            ← Route definitions, navigate(), Link component
        guards.ts           ← requireAuth(), requireRole() guard wrappers
      utils/
        price.ts            ← Format "10.4500" string to locale currency string
        date.ts             ← Format RFC3339 to relative/absolute display
        uuid.ts             ← Validate UUID strings
        roles.ts            ← Role ordering helpers (hasMinRole, etc.)
    routes/                 ← Page components (one file per route)
      Login.svelte
      TeamSelect.svelte           ← "/" — lists user's teams; redirects if only one
      team/
        Dashboard.svelte          ← "/teams/:teamId"
        Inventory.svelte          ← "/teams/:teamId/inventory"
        InventoryEntry.svelte     ← "/teams/:teamId/inventory/:entryId"
        Parts.svelte              ← "/teams/:teamId/parts"
        PartDetail.svelte         ← "/teams/:teamId/parts/:partId"
        Storage.svelte            ← "/teams/:teamId/storage"
        Projects.svelte           ← "/teams/:teamId/projects"
        ProjectDetail.svelte      ← "/teams/:teamId/projects/:projectId"
        RevisionDetail.svelte     ← "/teams/:teamId/projects/:projectId/revisions/:revId"
        StockCheck.svelte         ← "/teams/:teamId/projects/:projectId/revisions/:revId/stock"
        Cart.svelte               ← "/teams/:teamId/cart"
        Orders.svelte             ← "/teams/:teamId/orders"
        OrderDetail.svelte        ← "/teams/:teamId/orders/:orderId"
        Builds.svelte             ← "/teams/:teamId/builds"
        BuildDetail.svelte        ← "/teams/:teamId/builds/:buildId"
        Checkouts.svelte          ← "/teams/:teamId/checkouts"
        settings/
          Distributors.svelte     ← "/teams/:teamId/settings/distributors"
          Members.svelte          ← "/teams/:teamId/settings/members"
          Team.svelte             ← "/teams/:teamId/settings"
    settings/               ← User-level settings (not team-scoped)
      Profile.svelte              ← "/settings/profile"
      Sessions.svelte             ← "/settings/sessions"
      Tokens.svelte               ← "/settings/tokens"
    App.svelte              ← Root component: router outlet + AppShell
    main.ts                 ← Vite entry; mounts App.svelte
  index.html                ← SPA shell; single <script type="module" src="/src/main.ts">
  vite.config.ts
  tsconfig.json
  tailwind.config.ts        ← (Tailwind v4 uses CSS config; this may be minimal)
  package.json
  .gitignore                ← node_modules, dist
```

---

## 4. Routing

### Router Design

A single file (`src/lib/router/index.ts`) implements an ~80-line history-mode router:

- A Svelte 5 rune (`$state`) holds the current `{ path, params }` object.
- A `navigate(path)` function calls `history.pushState` and updates the state.
- A `<Link href="...">` component renders an `<a>` that intercepts clicks and calls `navigate()`.
- A `<Route>` component (or a route-matching function) resolves the current path to a page component.
- `popstate` listener handles browser back/forward.

No third-party router dependency.

### Route Table

| Path | Component | Min Auth | Notes |
|---|---|---|---|
| `/login` | `Login.svelte` | none | Redirects to `/` if already logged in |
| `/` | `TeamSelect.svelte` | authenticated | Redirects to `/teams/:teamId` if user has exactly one team |
| `/teams/:teamId` | `Dashboard.svelte` | V | Team overview |
| `/teams/:teamId/inventory` | `Inventory.svelte` | V | Paginated, filterable inventory list |
| `/teams/:teamId/inventory/:entryId` | `InventoryEntry.svelte` | V | Full detail: part info, adjustments, listings |
| `/teams/:teamId/parts` | `Parts.svelte` | V | Parts catalog browser |
| `/teams/:teamId/parts/:partId` | `PartDetail.svelte` | V | Part detail: parameters, listings, enrichment trigger |
| `/teams/:teamId/storage` | `Storage.svelte` | V | Storage location CRUD (admin creates/edits) |
| `/teams/:teamId/projects` | `Projects.svelte` | V | Project list + create (M+) |
| `/teams/:teamId/projects/:projectId` | `ProjectDetail.svelte` | V | Revision list, set active revision |
| `/teams/:teamId/projects/:projectId/revisions/:revId` | `RevisionDetail.svelte` | V | Line item list, resolve line items, manage substitutes |
| `/teams/:teamId/projects/:projectId/revisions/:revId/stock` | `StockCheck.svelte` | V | Stock check with build-qty input; add to cart |
| `/teams/:teamId/cart` | `Cart.svelte` | P | Cart management, convert to orders |
| `/teams/:teamId/orders` | `Orders.svelte` | V | Order list |
| `/teams/:teamId/orders/:orderId` | `OrderDetail.svelte` | V | Order detail, receive items (P+) |
| `/teams/:teamId/builds` | `Builds.svelte` | V | Build event history |
| `/teams/:teamId/builds/:buildId` | `BuildDetail.svelte` | V | Build detail, stock deductions |
| `/teams/:teamId/checkouts` | `Checkouts.svelte` | V | General checkout history |
| `/teams/:teamId/settings` | `Team.svelte` | A | Team name/description |
| `/teams/:teamId/settings/members` | `Members.svelte` | A | Member list, add/remove, change role |
| `/teams/:teamId/settings/distributors` | `Distributors.svelte` | A | Enable/configure distributors |
| `/settings/profile` | `Profile.svelte` | authenticated | Edit display name, email |
| `/settings/sessions` | `Sessions.svelte` | authenticated | List and revoke sessions |
| `/settings/tokens` | `Tokens.svelte` | authenticated | Create/revoke API tokens |

### Route Guards

Two guard functions wrap page components:

- `requireAuth(component)` — redirects to `/login` if no session.
- `requireRole(teamId, minRole, component)` — redirects to `/teams/:teamId` if the user's role in the team is below `minRole`. Role ordering: `viewer < member < purchaser < admin`.

The `activeTeam` store is set when any `/teams/:teamId/...` route is resolved; it exposes `{ teamId, role }` to all descendant components.

---

## 5. State Management

### Auth Store (`stores/auth.ts`)

```ts
// Populated on app load (GET /auth/me) and on login.
interface AuthState {
  user: User | null;
  memberships: TeamMembership[];
  loading: boolean;
}
```

On app start, `App.svelte` calls `GET /api/v1/auth/me`. If it returns 401, the user is redirected to `/login`. On successful logout, the store is cleared.

### Active Team Store (`stores/activeTeam.ts`)

Derived from the current route. When the path is `/teams/:teamId/...`, the router writes `{ teamId, role }` into this store. All team-scoped components read from it rather than re-parsing the URL. Null when on a non-team route.

### Preferences Store (`stores/preferences.ts`)

A writable store. Loaded on auth, persisted by calling `PUT /api/v1/preferences` when mutated. Used for: theme, locale, per-table column visibility, default BOM import column names.

### Server State — @tanstack/svelte-query

All API data (inventory list, part detail, cart, orders, etc.) is managed via TanStack Query:

- **Queries** (`useQuery`) — data fetching with automatic background refetching, loading/error states.
- **Mutations** (`useMutation`) — data writes with cache invalidation. E.g. after receiving order line items, invalidate the order query, the inventory query for affected parts, and the adjustment history query.
- **Query keys** are scoped: `['inventory', teamId, filters]`, `['parts', teamId, partId]`, etc.

This eliminates boilerplate loading/error/refetch state management, which would be substantial given the number of list views.

### Toast Store (`stores/toasts.ts`)

A queue of `{ id, type: 'success' | 'error' | 'info', message, detail? }` objects. Components push to it; the global `<Toaster>` (rendered in `App.svelte`) drains it. Used for mutation feedback ("Part enrichment queued", "Order received successfully", "Error: ...").

---

## 6. API Client Layer

### Base Client (`api/client.ts`)

```ts
// Returns parsed JSON on 2xx; throws ApiError on non-2xx.
// Credentials: 'include' ensures the session cookie is sent automatically.
async function request<T>(method: string, path: string, body?: unknown): Promise<T>
```

`ApiError` carries `{ status, code, message }` from the backend's standard error envelope.

Authentication is entirely cookie-based for the browser client. No Authorization header management is needed — the session cookie is set by the server and sent by `fetch` with `credentials: 'include'`.

### Module Example (`api/inventory.ts`)

```ts
export function listInventory(teamId: string, params: ListInventoryParams) {
  return request<PaginatedResponse<InventoryEntry>>('GET', `/api/v1/teams/${teamId}/inventory?${qs(params)}`)
}

export function createAdjustment(teamId: string, entryId: string, body: CreateAdjustmentBody) {
  return request<StockAdjustment>('POST', `/api/v1/teams/${teamId}/inventory/${entryId}/adjustments`, body)
}
```

Every module exports typed functions. TypeScript interfaces in `types/` are written once, matching the documented API response shapes.

### Error Handling Convention

TanStack Query surfaces errors from mutations/queries automatically. Page components render an error state when a query fails. The `useMutation` `onError` callback pushes to the toast store for user feedback.

---

## 7. Components

### Layout

| Component | Description |
|---|---|
| `AppShell` | Outer wrapper: fixed sidebar on the left, scrollable main content on the right |
| `Sidebar` | Navigation links (grouped: Parts, Inventory, Projects, Orders, Builds, Settings); collapses on narrow screens; team switcher at the top |
| `Topbar` | Sticky page header: page title, breadcrumb trail, right-aligned action slot (e.g. "New Project" button) |
| `TeamSwitcher` | Dropdown listing the user's teams by name; clicking navigates to the team's dashboard |

### UI Primitives (shadcn-svelte)

Button, Badge, Input, Select, Textarea, Dialog (modal), DropdownMenu, Popover, Tooltip, Tabs, Table, Separator, Skeleton (loading placeholder), ScrollArea, Command (command palette / combobox). All sourced from shadcn-svelte — components live in `src/lib/components/ui/` as first-class source files.

### Common Domain-Agnostic Components

| Component | Description |
|---|---|
| `Pagination` | Previous/Next + page info for offset-based pagination |
| `SearchInput` | Debounced input (300ms), clear button, forwards `search` event |
| `EmptyState` | Centered illustration + heading + optional CTA button |
| `ConfirmDialog` | Modal with title, description, confirm/cancel; returns Promise\<boolean\> |
| `JobProgress` | Inline banner showing async job state (pending → running → complete/error); subscribes to SSE for job ID |
| `PriceDisplay` | Renders `"10.4500"` as `$10.45` (or locale-appropriate format) |
| `DateDisplay` | Renders RFC3339 timestamp as "3 hours ago" / tooltip with full date |
| `CopyButton` | Icon button; copies text to clipboard; shows tick on success |

### Domain Components

**Parts**

| Component | Description |
|---|---|
| `PartSearch` | Full combobox (`GET /api/v1/teams/:id/parts?q=...`) to pick a Part; shows MPN + description |
| `PartBadge` | Compact chip: MPN (bold) + truncated description; links to part detail |
| `LifecycleBadge` | Coloured chip: Active (green), NRND (amber), Obsolete (red), etc. |
| `ParameterTable` | Two-tab display: "User Specified" / "Distributor Sourced"; shows name, value, unit, source |
| `ListingTable` | Table of distributor listings: distributor, SKU, packaging, price, stock, MOQ; includes "Refresh" action |
| `ListingPicker` | Select component populated from a part's listings; used in cart and inventory entry edit |

**Inventory**

| Component | Description |
|---|---|
| `StockBadge` | "In Stock", "Partial" (amber), "Out of Stock" (red), "On Order" (blue) |
| `AdjustmentHistory` | Paginated table of stock adjustments with source badge (Manual / Order / Build / Checkout) |

**BOM**

| Component | Description |
|---|---|
| `BOMLineItemRow` | Row in revision detail: ref designator, qty, raw description, resolve status chip, linked part (or PartSearch to resolve), substitutes count |
| `SubstituteManager` | Expandable panel: lists approved substitutes + `PartSearch` to add new ones; remove button per entry |
| `StockCheckRow` | Stock check result row: line item, required qty, on-hand qty (across primary + substitutes), shortfall chip, "Add to cart" button (shown when shortfall > 0) |
| `BOMImportProgress` | Specialised `JobProgress` for `JobBOMImport`; shows per-item resolution events as they arrive over SSE |

**Cart**

| Component | Description |
|---|---|
| `CartLineItem` | Row with: part name, qty input, `ListingPicker`, source badge (BOM Shortfall / Low Stock / Manual), unit price × qty subtotal, remove button |
| `CartSummary` | Collapsible per-distributor totals table at the bottom of the cart; "Convert to Orders" button |

**Orders**

| Component | Description |
|---|---|
| `OrderStatusBadge` | Draft (grey) / Placed (blue) / Exported (amber) / Partially Received (cyan) / Received (green) |
| `ReceiveLineItem` | Inline form: shows ordered qty, input for received qty; validates received ≤ ordered |

---

## 8. Page Descriptions

### Login (`/login`)

Username + password form. On submit, calls `POST /api/v1/auth/login`, stores user in auth store, navigates to `/`. Also shows OIDC login button(s) if the backend reports providers are configured (detected via a `GET /api/v1/auth/providers` endpoint or hardcoded from config).

### Team Select (`/`)

Grid of the user's teams (from `memberships[]` in auth store). Each card shows team name, role badge, and quick-link to inventory/projects. "New Team" button opens a dialog. If the user has exactly one team, auto-redirects to `/teams/:teamId`.

### Dashboard (`/teams/:teamId`)

Summary tiles:
- Inventory count, items below minimum stock
- Active projects count
- Open orders count
- Cart item count (with link to cart)
- Recent builds / checkouts

### Inventory (`/teams/:teamId/inventory`)

Filterable, paginated table of inventory entries. Columns: MPN, description, quantity on hand, storage location, min stock (with warning icon if below min). Filter bar: text search, storage location dropdown, "Below minimum" toggle. "Add Part to Inventory" action opens a dialog with `PartSearch`.

### Inventory Entry (`/teams/:teamId/inventory/:entryId`)

Left column: part detail (MPN, manufacturer, description, lifecycle badge, parameters). Right column: quantity on hand, storage location, min stock settings, preferred listing. Bottom: `AdjustmentHistory`. Actions: Manual Adjustment (dialog with delta + reason), Edit Entry (storage, min stock, reorder qty).

### Parts Catalog (`/teams/:teamId/parts`)

Paginated, filterable parts list. Columns: MPN, manufacturer, description, category, package, lifecycle. Filters: scope (global/team), text search, category, lifecycle. "New Part" button (member+). Each row links to part detail.

### Part Detail (`/teams/:teamId/parts/:partId`)

Tabs: **Overview** (MPN, manufacturer, description, datasheet link, category, package, lifecycle badge), **Parameters** (`ParameterTable`), **Listings** (`ListingTable` with per-listing "Refresh" button), **Inventory** (how much this team has on hand + link to entry). Header actions: "Enrich Part" (triggers `JobPartEnrich` — shows `JobProgress` inline).

### Storage Locations (`/teams/:teamId/storage`)

Simple table: label, description, entry count. Admin can create/edit/delete locations (delete blocked if entries exist — 422 from backend shown as toast).

### Projects (`/teams/:teamId/projects`)

List of projects with name, active revision label, revision count. "New Project" opens a name/description dialog. Each row links to project detail.

### Project Detail (`/teams/:teamId/projects/:projectId`)

Revision list (label, imported date, item count, resolved/unresolved counts, active badge). "Import BOM" button opens a side panel with: label field, notes field, file upload (CSV) + column mapping (or manual paste), set-as-active toggle. Import calls `POST /api/v1/teams/:id/projects/:id/revisions` and shows `BOMImportProgress`. Each revision row links to revision detail.

### Revision Detail (`/teams/:teamId/projects/:projectId/revisions/:revId`)

Header: revision label, imported-by, imported-at, notes. "Set Active" button (member+). Action: "Check Stock" button (navigates to stock check). Body: table of `BOMLineItemRow` components, with counts of resolved/unresolved at the top. Unresolved items are highlighted. "Resolve All" — triggers enrichment for all unresolved items.

### Stock Check (`/teams/:teamId/projects/:projectId/revisions/:revId/stock`)

Build quantity input at the top (ephemeral, not saved). Table of `StockCheckRow` components. Summary row: boards fully buildable count. Actions: "Add All Shortfalls to Cart" button — calls `POST /api/v1/teams/:id/cart/add-shortfall`, then navigates to cart.

### Cart (`/teams/:teamId/cart`)

List of `CartLineItem` components. Cart is empty state or items. Below the list: `CartSummary` with per-distributor totals. Header actions: "Add Low Stock" (calls add-low-stock, refreshes), "Clear Cart" (confirm dialog + DELETE /items loop or batch endpoint). "Convert to Orders" button (purchaser+) — confirm dialog, calls `POST /api/v1/teams/:id/cart/convert`, navigates to orders.

### Orders (`/teams/:teamId/orders`)

Tabbed list: All / Draft / Placed / Received. Each row: distributor, order reference (if placed), line item count, total price, status badge, last updated. Link to order detail.

### Order Detail (`/teams/:teamId/orders/:orderId`)

Header: order status, distributor, total price, "Distributor Order Reference" field (editable for placed). Line items table with `ReceiveLineItem` for items not yet fully received (purchaser+). Actions by status:
- Draft: "Export CSV", "Submit via API" (if distributor has API integration)
- Placed: "Receive Items" (opens receive flow), "Mark as Received" (all at once)
- Exported: "Enter Order Reference" → marks as Placed

### Builds (`/teams/:teamId/builds`)

Table of build events: project name, revision label, quantity built, built-at, notes. "Record Build" button (member+) opens a multi-step dialog:
1. Pick project + revision
2. Enter quantity
3. Preview consumption table (from `POST /api/v1/teams/:id/builds/preview`)
4. Confirm → submits build

### Build Detail (`/teams/:teamId/builds/:buildId`)

Build metadata header. Table of consumed parts with quantity deducted, source inventory entry, link to part. Provides audit trail.

### General Checkouts (`/teams/:teamId/checkouts`)

Table of checkout events. "New Checkout" opens a dialog: add one or more parts with qty + optional reason.

### Distributor Settings (`/teams/:teamId/settings/distributors`)

Table of all distributors. Toggle (enabled/disabled), preferred toggle. "Configure" button opens a dialog for `cache_duration_seconds` and `api_credentials` JSON field. Credentials shown as a masked field with "Edit" to reveal/replace — never returned from the backend, so the field is always empty on load.

### Members Settings (`/teams/:teamId/settings/members`)

Table of members with role badge and "Change Role" / "Remove" actions. "Add Member" button: user search by email + role select.

### Profile Settings (`/settings/profile`)

Edit display name. Change email. If the account has a local password, "Change Password" section. Shows linked OIDC providers if any.

### Sessions (`/settings/sessions`)

Table of active sessions with IP, user agent, created/last-seen, current session highlighted. "Revoke" button per row (except current session which shows "Current").

### API Tokens (`/settings/tokens`)

Table of tokens: name, created, last used, expires. "New Token" opens a dialog: name + optional expiry. On creation, shows a one-time display of the raw token (`ptk_...`) with a `CopyButton`. "Revoke" per row.

---

## 9. SSE Integration

### Connection Management (`api/sse.ts`)

A single `EventSource` is opened at `/api/v1/events?team_id=...` when a team context is active. The module:

- Opens the connection lazily on first subscription.
- Maintains a subscriber map: `Map<string /* jobId */, Set<(event: SseEvent) => void>>`.
- Dispatches `message` events to subscribers by matching `event.job_id`.
- Reconnects automatically on connection drop (native `EventSource` behaviour).
- Closes when navigating away from team context or on logout.

### Component Integration

Components that trigger async jobs (BOM import, part enrichment, listing refresh) receive a `job_id` from the `202` response. They pass it to `<JobProgress jobId={...} />`, which subscribes to the SSE module and renders progress.

**`JobProgress` states:**
- `pending` — spinner, "Job queued…"
- `running` — progress text from SSE event payload
- `done` — success check, dismissible
- `error` — error message from SSE event, dismissible

`BOMImportProgress` extends this with a live-updating item-resolution counter ("Resolved 12/48 line items").

On job completion, `JobProgress` calls an optional `onComplete` callback so the parent page can invalidate its TanStack Query cache (e.g. refresh the revision's line item list after BOM import).

---

## 10. Key Design Decisions

### Price formatting

Prices from the API are `"10.4500"` decimal strings. A `price.ts` utility formats them using `Intl.NumberFormat` with the user's locale from preferences. Never parsed to `float` — kept as strings through the UI to avoid IEEE 754 representation issues on display, and sent back as strings in cart/order mutations.

### Role-gated UI

Rather than hiding UI elements entirely based on role (surprising to viewers), the pattern is:
- Show the action.
- Disable it (greyed button + tooltip: "Requires purchaser role") if the user lacks the required role.
- Exception: Admin-only settings pages are not shown in the sidebar for non-admins.

### Pagination strategy

All list pages use **offset pagination** matching the backend API. Page size is stored in preferences (default 50). Pagination controls are rendered by `<Pagination>`. No infinite scroll — the data is ordered and bounded; accurate total counts enable "page 3 of 7" style navigation.

### BOM import flow

BOM import is a notable UX challenge because it involves a job with potentially slow SSE feedback. The import is initiated in the project detail page via a side panel (not a full-page modal). After submitting, the side panel closes and a `BOMImportProgress` banner appears in the revision detail page. The user can navigate away and back — the job ID is preserved in the query cache until resolved.

### Monetary value display

All prices displayed using the `<PriceDisplay>` component so locale formatting is consistent. The component accepts the raw string and renders formatted output. This is the single point of price formatting logic in the frontend.

---

## 11. Build Integration

```bash
# Development (Vite dev server proxies /api to the Go server)
cd web && npm run dev

# Production
cd web && npm run build   # outputs to web/dist/
go build ./...            # embeds web/dist/ into the binary via //go:embed
```

Vite dev server is configured with a proxy: requests to `/api` are forwarded to `http://localhost:8080` (the Go server). The frontend `fetch` calls use relative paths (`/api/v1/...`), so they work without modification in both dev and production.

The Vite config also sets `build.outDir = '../web/dist'` (relative to the `web/` directory, or absolute path) so the Go embed path `web/dist` is always correct.
