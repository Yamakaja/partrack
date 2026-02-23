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
        client.ts           ← Base fetch wrapper: credentials, error parsing
        auth.ts             ← /auth/login, /auth/logout, /auth/me, OIDC redirect,
                               sessions (list/revoke), API tokens (list/create/revoke),
                               user preferences, user search
        admin.ts            ← Instance admin: user CRUD, set-password, reset links;
                               also getResetPasswordInfo / completePasswordReset
        teams.ts            ← Teams, memberships
        distributors.ts     ← Distributor list, team configs
        parts.ts            ← Parts catalog, parameters, listings, enrichment, listing refresh
        storage.ts          ← Storage locations
        inventory.ts        ← Inventory entries, adjustments, replenish preview
        projects.ts         ← Projects
        revisions.ts        ← BOM revisions, line items, substitutes, stock check
        cart.ts             ← Cart read/write, add-shortfall, add-low-stock, convert
        orders.ts           ← Orders, line items, receipt
        builds.ts           ← Builds
        checkouts.ts        ← General checkouts
        sse.ts              ← EventSource lifecycle management
      components/
        ui/                 ← shadcn-svelte primitives (copied in, not installed)
          badge/            ← Badge
          button/           ← Button
          dialog/           ← Dialog (via bits-ui Dialog.Root/Content/Portal/Overlay/Title)
          dropdown-menu/    ← DropdownMenu
          input/            ← Input
          label/            ← Label
          select/           ← Select
          separator/        ← Separator
          skeleton/         ← Skeleton
          table/            ← Table, TableHeader, TableBody, TableRow, TableHead, TableCell
          tabs/             ← Tabs, TabsList, TabsTrigger, TabsContent
          textarea/         ← Textarea
          tooltip/          ← Tooltip
        layout/             ← App chrome
          AppShell.svelte       ← Root layout: sidebar + main area
          Sidebar.svelte        ← Navigation tree + team switcher + admin section
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
          Toaster.svelte        ← Global toast notification renderer
        parts/              ← Parts-domain components
          PartSearch.svelte     ← Combobox to search and select a Part by MPN/description
          LifecycleBadge.svelte ← Active / NRND / Obsolete badge
          ParameterTable.svelte ← Grouped parameters display (user / distributor sourced)
          ListingTable.svelte   ← Distributor listings for a part
        inventory/
          StockBadge.svelte     ← In Stock / Partial / Out of Stock coloured chip
          AdjustmentHistory.svelte ← Paginated list of stock_adjustments for an entry
        bom/
          BOMLineItemRow.svelte    ← Single BOM line item with resolve status + substitute editor
          BOMImportProgress.svelte ← Specialised JobProgress for BOM import jobs
      stores/               ← Global reactive state (Svelte 5 runes; `.svelte.ts` suffix)
        auth.svelte.ts      ← currentUser (User + memberships[]); set on login/me response
        activeTeam.svelte.ts ← teamId + role derived from URL; guards route access
        preferences.svelte.ts ← Writable preferences blob; synced to /api/v1/preferences
        toasts.svelte.ts    ← Toast notification queue
      types/                ← TypeScript interfaces mirroring API response shapes
        auth.ts             ← User (incl. is_admin), TeamMembership, Team, Session, ApiToken
        common.ts
        inventory.ts
        orders.ts
        parts.ts
        projects.ts
      router/
        index.ts            ← currentRoute(), navigate(), matchRoute(), linkAction
        guards.ts           ← Guard helpers
      utils/
        cn.ts               ← Tailwind class merging (clsx + tailwind-merge)
        date.ts             ← Format RFC3339 to relative/absolute display
        price.ts            ← Format "10.4500" string to locale currency string
        roles.ts            ← Role ordering helpers (hasMinRole, etc.)
    routes/                 ← Page components (one file per route)
      Login.svelte
      TeamSelect.svelte           ← "/" — lists user's teams; redirects if only one
      admin/
        Users.svelte              ← "/admin/users" — instance admin: user management
        ResetPassword.svelte      ← "/reset-password" — public password-reset flow
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
  package.json
  .gitignore                ← node_modules, dist
```

---

## 4. Routing

### Router Design

A single file (`src/lib/router/index.ts`) implements an ~80-line history-mode router:

- A Svelte 5 rune (`$state`) holds the current `{ path, params }` object.
- A `navigate(path)` function calls `history.pushState` and updates the state.
- `linkAction` is a Svelte action applied to `<a>` elements that intercepts clicks and calls `navigate()`.
- `matchRoute(pattern, path)` resolves a path pattern (with `:param` segments) to a params map.
- `popstate` listener handles browser back/forward.

No third-party router dependency.

### Route Table

| Path | Component | Auth required | Notes |
|---|---|---|---|
| `/login` | `Login.svelte` | no | Redirects to `/` if already logged in |
| `/reset-password` | `ResetPassword.svelte` | no | Token from `?token=` query param; signs user in on success |
| `/` | `TeamSelect.svelte` | yes | Redirects to `/teams/:teamId` if user has exactly one team |
| `/admin` | `Users.svelte` | admin | Alias for `/admin/users` |
| `/admin/users` | `Users.svelte` | admin | Instance admin user management |
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
| `/settings/profile` | `Profile.svelte` | yes | Edit display name, email |
| `/settings/sessions` | `Sessions.svelte` | yes | List and revoke sessions |
| `/settings/tokens` | `Tokens.svelte` | yes | Create/revoke API tokens |

Auth column: `V` = viewer+, `M` = member+, `P` = purchaser+, `A` = team admin+, `admin` = instance admin flag.

### Route Guards

The `$effect` in `App.svelte` redirects to `/login` when the user is not authenticated, with `/login` and `/reset-password` as the only public exemptions. The `activeTeam` store is set when any `/teams/:teamId/...` route is resolved; it exposes `{ teamId, role }` to all descendant components.

---

## 5. State Management

### Auth Store (`stores/auth.svelte.ts`)

```ts
// Populated on app load (GET /auth/me) and on login.
interface AuthState {
  user: User | null;   // includes is_admin
  memberships: TeamMembership[];
  loading: boolean;
}
```

On app start, `App.svelte` calls `GET /api/v1/auth/me`. If it returns 401, the user is redirected to `/login`. On successful logout, the store is cleared. `setAuth(user, memberships)` is called after login or password reset to update the store without a round-trip.

### Active Team Store (`stores/activeTeam.svelte.ts`)

Derived from the current route. When the path is `/teams/:teamId/...`, the router writes `{ teamId, role }` into this store. All team-scoped components read from it rather than re-parsing the URL. Null when on a non-team route.

### Preferences Store (`stores/preferences.svelte.ts`)

A writable store. Loaded on auth, persisted by calling `PUT /api/v1/preferences` when mutated. Used for: theme, locale, per-table column visibility, default BOM import column names.

### Server State — @tanstack/svelte-query

All API data (inventory list, part detail, cart, orders, etc.) is managed via TanStack Query:

- **Queries** (`createQuery`) — data fetching with automatic background refetching, loading/error states.
- **Mutations** (`createMutation`) — data writes with cache invalidation. E.g. after receiving order line items, invalidate the order query, the inventory query for affected parts, and the adjustment history query.
- **Query keys** are scoped: `['inventory', teamId, filters]`, `['parts', teamId, partId]`, etc.

This eliminates boilerplate loading/error/refetch state management, which would be substantial given the number of list views.

### Toast Store (`stores/toasts.svelte.ts`)

A queue of `{ id, type: 'success' | 'error' | 'info', message, detail? }` objects. Components push to it via `pushToast()`; the global `<Toaster>` (rendered in `App.svelte`) drains it. Used for mutation feedback ("Part enrichment queued", "Order received successfully", "Error: ...").

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

### Module Overview

| Module | Key exports |
|---|---|
| `auth.ts` | `login`, `logout`, `getMe`, `listSessions`, `revokeSession`, `listTokens`, `createToken`, `revokeToken`, `getPreferences`, `putPreferences`, `searchUsers`, `OIDCInitiate` |
| `admin.ts` | `listUsers`, `createUser`, `updateUser`, `setPassword`, `generateResetLink`, `getResetPasswordInfo`, `completePasswordReset` |
| `teams.ts` | `listTeams`, `createTeam`, `getTeam`, `updateTeam`, `listMembers`, `addMember`, `updateMemberRole`, `removeMember`, `deactivateUser`, `searchUsers` |
| `distributors.ts` | `listDistributors`, `listDistributorConfigs`, `upsertDistributorConfig`, `deleteDistributorConfig` |
| `parts.ts` | `listParts`, `createPart`, `getPart`, `updatePart`, `enrichPart`, `listParameters`, `createParameter`, `deleteParameter`, `listListings`, `refreshListing` |
| `storage.ts` | `listStorageLocations`, `createStorageLocation`, `updateStorageLocation`, `deleteStorageLocation` |
| `inventory.ts` | `listInventory`, `getInventoryEntry`, `createInventoryEntry`, `updateInventoryEntry`, `listAdjustments`, `createAdjustment`, `replenishPreview` |
| `projects.ts` | `listProjects`, `createProject`, `getProject`, `updateProject`, `stockCheck` |
| `revisions.ts` | `listRevisions`, `importRevision`, `getRevision`, `updateRevision`, `updateBOMItem`, `addSubstitute`, `removeSubstitute`, `listSubstitutes` |
| `cart.ts` | `getCart`, `clearCart`, `addCartItem`, `updateCartItem`, `removeCartItem`, `addShortfalls`, `addLowStock`, `convertCart` |
| `orders.ts` | `listOrders`, `getOrder`, `updateOrder`, `cancelOrder`, `exportOrder`, `submitOrder`, `linkOrderReference`, `receiveOrderItem` |
| `builds.ts` | `listBuilds`, `getBuild`, `previewBuild`, `createBuild` |
| `checkouts.ts` | `listCheckouts`, `getCheckout`, `createCheckout` |
| `sse.ts` | EventSource lifecycle management |

### Error Handling Convention

TanStack Query surfaces errors from mutations/queries automatically. Page components render an error state when a query fails. The `onError` callback or `catch` in handlers pushes to the toast store for user feedback.

---

## 7. Components

### Layout

| Component | Description |
|---|---|
| `AppShell` | Outer wrapper: fixed sidebar on the left, scrollable main content on the right |
| `Sidebar` | Navigation links (grouped: Parts, Inventory, Projects, Orders, Builds, Settings); team switcher at the top; "Administration → Users" section visible only to `is_admin` users |
| `Topbar` | Sticky page header: page title, right-aligned action slot (e.g. "New Project" button) |
| `TeamSwitcher` | Dropdown listing the user's teams by name; clicking navigates to the team's dashboard |

### UI Primitives (shadcn-svelte)

Button, Badge, Input, Label, Select, Textarea, Dialog (via bits-ui), DropdownMenu, Tooltip, Tabs, Table, Separator, Skeleton (loading placeholder). All sourced from shadcn-svelte — components live in `src/lib/components/ui/` as first-class source files.

### Common Domain-Agnostic Components

| Component | Description |
|---|---|
| `Pagination` | Previous/Next + page info for offset-based pagination |
| `SearchInput` | Debounced input (300ms), clear button |
| `EmptyState` | Centered heading + optional description |
| `ConfirmDialog` | Modal with title, description, confirm/cancel, optional destructive styling |
| `JobProgress` | Inline banner showing async job state (pending → running → complete/error); subscribes to SSE for job ID |
| `PriceDisplay` | Renders `"10.4500"` as `$10.45` (or locale-appropriate format) |
| `DateDisplay` | Renders RFC3339 timestamp as locale string |
| `CopyButton` | Icon button; copies text to clipboard; shows tick on success |
| `Toaster` | Global toast notification renderer (mounted in `App.svelte`) |

### Domain Components

**Parts**

| Component | Description |
|---|---|
| `PartSearch` | Full combobox (`GET /api/v1/teams/:id/parts?q=...`) to pick a Part; shows MPN + description |
| `LifecycleBadge` | Coloured chip: Active (green), NRND (amber), Obsolete (red), etc. |
| `ParameterTable` | Two-tab display: "User Specified" / "Distributor Sourced"; shows name, value, unit, source |
| `ListingTable` | Table of distributor listings: distributor, SKU, packaging, price, stock, MOQ; includes "Refresh" action |

**Inventory**

| Component | Description |
|---|---|
| `StockBadge` | "In Stock", "Partial" (amber), "Out of Stock" (red), "On Order" (blue) |
| `AdjustmentHistory` | Paginated table of stock adjustments with source badge (Manual / Order / Build / Checkout) |

**BOM**

| Component | Description |
|---|---|
| `BOMLineItemRow` | Row in revision detail: ref designator, qty, raw description, resolve status chip, linked part (or PartSearch to resolve), substitutes count |
| `BOMImportProgress` | Specialised `JobProgress` for `JobBOMImport`; shows per-item resolution events as they arrive over SSE |

---

## 8. Page Descriptions

### Login (`/login`)

Username + password form. On submit, calls `POST /api/v1/auth/login`, stores user in auth store, navigates to `/`. Also shows OIDC login button(s) if the backend reports providers are configured.

### Reset Password (`/reset-password`)

Public page — no auth required. Reads `token` from `?token=` query param. Calls `GET /api/v1/auth/reset-password?token=...` on mount to validate the token and retrieve the target user's name. If valid, shows a password + confirm form. On submit, calls `POST /api/v1/auth/reset-password`; on success, calls `setAuth()` and navigates to `/`. Shows an error state if the token is invalid or expired.

### Team Select (`/`)

Grid of the user's teams (from `memberships[]` in auth store). Each card shows team name, role badge, and quick-link to inventory/projects. "New Team" button opens a dialog. If the user has exactly one team, auto-redirects to `/teams/:teamId`.

### Admin Users (`/admin/users`)

Instance admin only. Paginated table of all user accounts. Columns: Display Name, Email, Status (Active/Inactive chip), Admin (badge), Has Password, Created. Row actions:
- **Edit** — dialog to update display name, email, and instance-admin flag.
- **Set Password** — dialog with a password field; calls `POST /admin/users/{id}/set-password`.
- **Reset Link** — generates a 24-hour one-time password-reset link via `POST /admin/users/{id}/password-reset-link`; shows a copyable URL in a modal.
- **Deactivate / Reactivate** — toggles `is_active`; deactivation shows a confirm dialog and immediately revokes all sessions.

"Add User" button opens a dialog: display name (required), email (required), password (optional — if blank, user must use a reset link or OIDC to log in first).

### Dashboard (`/teams/:teamId`)

Summary tiles: inventory count, items below minimum stock, active projects count, open orders count, cart item count, recent builds/checkouts.

### Inventory (`/teams/:teamId/inventory`)

Filterable, paginated table of inventory entries. Columns: MPN, description, quantity on hand, storage location, min stock (with warning icon if below min). Filter bar: text search, storage location dropdown, "Below minimum" toggle.

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

Header: revision label, imported-by, imported-at, notes. "Set Active" button (member+). Action: "Check Stock" button (navigates to stock check). Body: table of `BOMLineItemRow` components, with counts of resolved/unresolved at the top. Unresolved items are highlighted.

### Stock Check (`/teams/:teamId/projects/:projectId/revisions/:revId/stock`)

Build quantity input at the top (ephemeral, not saved). Table of stock check rows. Summary row: boards fully buildable count. Actions: "Add All Shortfalls to Cart" button — calls `POST /api/v1/teams/:id/cart/add-shortfall`, then navigates to cart.

### Cart (`/teams/:teamId/cart`)

List of cart items. Below the list: per-distributor cost breakdown. Header actions: "Add Low Stock" (calls add-low-stock, refreshes), "Clear Cart" (confirm dialog). "Convert to Orders" button (purchaser+) — confirm dialog, calls `POST /api/v1/teams/:id/cart/convert`, navigates to orders.

### Orders (`/teams/:teamId/orders`)

Tabbed list: All / Draft / Placed / Received. Each row: distributor, order reference (if placed), line item count, total price, status badge, last updated. Link to order detail.

### Order Detail (`/teams/:teamId/orders/:orderId`)

Header: order status, distributor, total price. Line items table with receive-qty inputs for items not yet fully received (purchaser+). Actions by status: Draft → Export CSV / Submit via API; Placed → Receive Items; Exported → Enter Order Reference.

### Builds (`/teams/:teamId/builds`)

Table of build events: project name, revision label, quantity built, built-at, notes. "Record Build" button (member+) opens a multi-step dialog: pick project + revision → enter quantity → preview consumption table → confirm.

### Build Detail (`/teams/:teamId/builds/:buildId`)

Build metadata header. Table of consumed parts with quantity deducted, source inventory entry, link to part.

### General Checkouts (`/teams/:teamId/checkouts`)

Table of checkout events. "New Checkout" opens a dialog: add one or more parts with qty + optional reason.

### Distributor Settings (`/teams/:teamId/settings/distributors`)

Table of all distributors. Toggle (enabled/disabled), preferred toggle. "Configure" button opens a dialog for `cache_duration_seconds` and API credentials.

### Members Settings (`/teams/:teamId/settings/members`)

Table of members with role select and "Remove" action. "Add Member" button: user search by name/email + role select.

### Profile Settings (`/settings/profile`)

Edit display name and email. Shows linked OIDC providers if any.

### Sessions (`/settings/sessions`)

Table of active sessions with IP, user agent, created/last-seen, current session highlighted. "Revoke" button per row (except current session which shows "Current").

### API Tokens (`/settings/tokens`)

Table of tokens: name, created, last used, expires. "New Token" opens a dialog: name + optional expiry. On creation, shows a one-time display of the raw token with a `CopyButton`. "Revoke" per row.

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

On job completion, `JobProgress` calls an optional `onComplete` callback so the parent page can invalidate its TanStack Query cache.

---

## 10. Key Design Decisions

### Price formatting

Prices from the API are `"10.4500"` decimal strings. A `price.ts` utility formats them using `Intl.NumberFormat` with the user's locale from preferences. Never parsed to `float` — kept as strings through the UI to avoid IEEE 754 representation issues on display, and sent back as strings in cart/order mutations.

### Role-gated UI

Rather than hiding UI elements entirely based on role (surprising to viewers), the pattern is:
- Show the action.
- Disable it (greyed button + tooltip: "Requires purchaser role") if the user lacks the required role.
- Exception: Admin-only settings pages are not shown in the sidebar for non-admins.

The instance-level admin flag (`user.is_admin`) is separate from team roles. The "Administration" sidebar section is only rendered when `authStore.user?.is_admin` is true.

### Pagination strategy

All list pages use **offset pagination** matching the backend API. Page size is stored in preferences (default 50). Pagination controls are rendered by `<Pagination>`. No infinite scroll — the data is ordered and bounded; accurate total counts enable "page 3 of 7" style navigation.

### BOM import flow

BOM import is a notable UX challenge because it involves a job with potentially slow SSE feedback. The import is initiated in the project detail page via a side panel (not a full-page modal). After submitting, the side panel closes and a `BOMImportProgress` banner appears in the revision detail page. The user can navigate away and back — the job ID is preserved in the query cache until resolved.

### Admin bootstrapping

No instance admin exists initially. The first admin must be set via direct SQL:
```sql
UPDATE users SET is_admin = TRUE WHERE email = 'your@email.com';
```

Thereafter, admins can grant the flag to others via the Users admin panel.

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

The Vite config also sets `build.outDir = '../web/dist'` so the Go embed path `web/dist` is always correct.
