# partrack — Frontend Work Plan

All phases are sequential. Each phase ends with a working, buildable state. See `frontend.md` for the full design rationale.

---

## Phase 0 — Scaffold

**Goal:** A working Vite dev server that proxies `/api` to the Go backend and produces an embeddable `dist/`.

```bash
# Inside web/
npm create vite@latest . -- --template svelte-ts
npm install
npm install -D tailwindcss @tailwindcss/vite
npm install @tanstack/svelte-query
npx shadcn-svelte@latest init
```

Files to write / configure:

| File | Content |
|---|---|
| `web/vite.config.ts` | `plugins: [tailwindcss(), svelte()]`; `server.proxy: { '/api': 'http://localhost:8080' }`; `build.outDir: 'dist'` |
| `web/src/app.css` | `@import "tailwindcss"` (Tailwind v4 single import) |
| `web/index.html` | SPA shell; `<script type="module" src="/src/main.ts">` |
| `web/src/main.ts` | Mount `App.svelte` |
| `web/src/App.svelte` | Stub — renders "partrack" text |
| `web/.gitignore` | `node_modules/`, (do not ignore `dist/` — the `.gitkeep` handles it) |

**Acceptance:** `npm run dev` serves; `npm run build` populates `web/dist/`; `go build ./...` still compiles.

---

## Phase 1 — Router + App Shell

**Goal:** Navigable multi-page skeleton with sidebar and team context.

### 1a. Router (`src/lib/router/`)

| File | Responsibility |
|---|---|
| `index.ts` | `currentRoute` rune (`{ path, params }`); `navigate(path)`; `<Link>`; `<Router>` outlet; `popstate` listener |
| `guards.ts` | `requireAuth(component)` → redirect `/login`; `requireRole(minRole, component)` → redirect `/teams/:id` |

Route map is declared in `App.svelte` as a plain array of `{ pattern, component, guard? }` objects.

### 1b. Layout (`src/lib/components/layout/`)

| Component | Notes |
|---|---|
| `AppShell.svelte` | Fixed sidebar (240 px) + scrollable main; hides sidebar on login/team-select pages |
| `Sidebar.svelte` | Grouped nav links (see §8 of frontend.md); active link highlight via `currentRoute` |
| `Topbar.svelte` | `title` prop + `actions` slot; rendered at top of main area |
| `TeamSwitcher.svelte` | Dropdown from `auth.memberships`; `navigate` on selection |

### 1c. Stub Pages

Create placeholder `.svelte` files (just a `<h1>`) for every route in the route table so the router can resolve all paths. Fill in content in later phases.

**Acceptance:** Clicking sidebar links changes the URL and renders the matching stub page. Browser back/forward works.

---

## Phase 2 — Auth Store + Login

**Goal:** Real login/logout flow; all routes protected.

### Files

| File | Responsibility |
|---|---|
| `src/lib/types/auth.ts` | `User`, `TeamMembership`, `AuthState` interfaces |
| `src/lib/api/client.ts` | `request<T>(method, path, body?)` → typed JSON or throws `ApiError`; `credentials: 'include'` |
| `src/lib/api/auth.ts` | `login()`, `logout()`, `getMe()` |
| `src/lib/stores/auth.ts` | `authStore` (`AuthState`); `initAuth()` calls `getMe()` on app start |
| `src/lib/stores/activeTeam.ts` | Derived from `currentRoute.params.teamId` + `authStore.memberships`; exposes `{ teamId, role }` |
| `src/lib/stores/toasts.ts` | `push(toast)`, `dismiss(id)`; `Toast[]` readable |
| `src/routes/Login.svelte` | Email + password form; calls `login()`; redirects to `/` on success |
| `src/routes/TeamSelect.svelte` | Grid of team cards from `authStore.memberships`; auto-redirect if one team |

Activate guards in `App.svelte` now that `authStore` exists.

Add a global `<Toaster>` in `App.svelte` that renders the toast queue.

**Acceptance:** Unauthenticated users land on `/login`. Successful login navigates to `/`. Logout clears session and returns to `/login`.

---

## Phase 3 — UI Primitives

**Goal:** Shared component library ready for use in all domain phases.

### shadcn-svelte components to add

```bash
npx shadcn-svelte@latest add button badge dialog dropdown-menu input \
  select table tabs tooltip separator skeleton scroll-area command
```

These land in `src/lib/components/ui/`.

### Hand-rolled common components (`src/lib/components/common/`)

| Component | Key props / behaviour |
|---|---|
| `Pagination.svelte` | `total`, `limit`, `offset`; emits `change(newOffset)` |
| `SearchInput.svelte` | Debounce 300 ms; `value` bindable; clear button |
| `EmptyState.svelte` | `title`, `description`, `action?` slot |
| `ConfirmDialog.svelte` | `open`, `title`, `description`; `confirm` / `cancel` events |
| `PriceDisplay.svelte` | Accepts `"10.4500"` string; formats via `Intl.NumberFormat` |
| `DateDisplay.svelte` | Accepts RFC3339 string; renders relative time + full tooltip |
| `CopyButton.svelte` | Icon button; `navigator.clipboard.writeText`; tick feedback 2 s |

Add `src/lib/utils/price.ts`, `date.ts`, `roles.ts`.

**Acceptance:** Storybook-style demo page (hidden route `/dev/ui`) renders all primitives. Remove before production if desired.

---

## Phase 4 — Parts Catalog

**Goal:** Full parts list + part detail pages working end-to-end against the real API.

### Files

| File | Responsibility |
|---|---|
| `src/lib/types/parts.ts` | `Part`, `Parameter`, `Listing`, `PaginatedResponse<T>` |
| `src/lib/api/parts.ts` | `listParts`, `getPart`, `createPart`, `updatePart`, `enrichPart`, `listParameters`, `createParameter`, `deleteParameter`, `listListings`, `refreshListing` |
| `src/lib/api/distributors.ts` | `listDistributors`, `listDistributorConfigs`, `upsertDistributorConfig`, `deleteDistributorConfig` |
| `src/lib/components/parts/PartSearch.svelte` | Combobox using Command; debounced `listParts?q=...` |
| `src/lib/components/parts/PartBadge.svelte` | MPN + description chip |
| `src/lib/components/parts/LifecycleBadge.svelte` | Colour-coded enum badge |
| `src/lib/components/parts/ParameterTable.svelte` | Two-tab: user / distributor; edit/delete for user params |
| `src/lib/components/parts/ListingTable.svelte` | Distributor listings; "Refresh" per row (triggers `refreshListing` + SSE — use stub `JobProgress` for now) |
| `src/lib/components/parts/ListingPicker.svelte` | Select populated from `listListings(partId)` |
| `src/routes/team/Parts.svelte` | Paginated, filtered parts table |
| `src/routes/team/PartDetail.svelte` | Overview + ParameterTable + ListingTable tabs |

**TanStack Query setup** (`src/lib/api/queryClient.ts`): create and export the `QueryClient`; provide it in `App.svelte`.

**Acceptance:** `/teams/:id/parts` lists parts; `/teams/:id/parts/:partId` shows parameters and listings.

---

## Phase 5 — Inventory + Storage

### Files

| File | Responsibility |
|---|---|
| `src/lib/types/inventory.ts` | `InventoryEntry`, `StockAdjustment`, `StorageLocation` |
| `src/lib/api/inventory.ts` | `listInventory`, `createInventoryEntry`, `getInventoryEntry`, `updateInventoryEntry`, `listAdjustments`, `createAdjustment`, `replenishPreview` |
| `src/lib/api/storage.ts` | `listStorageLocations`, `createStorageLocation`, `updateStorageLocation`, `deleteStorageLocation` |
| `src/lib/components/inventory/StockBadge.svelte` | In Stock / Partial / Out of Stock / On Order chip |
| `src/lib/components/inventory/AdjustmentHistory.svelte` | Paginated table; source badge (Manual / Order / Build / Checkout) |
| `src/routes/team/Inventory.svelte` | Paginated list; text search, storage filter, below-min toggle |
| `src/routes/team/InventoryEntry.svelte` | Part info + qty + adjustments; manual adjustment dialog |
| `src/routes/team/Storage.svelte` | CRUD table for storage locations |

**Acceptance:** Inventory list loads; manual stock adjustment posts and quantity updates; storage location CRUD works.

---

## Phase 6 — SSE Module + JobProgress

This phase must precede projects/BOM because BOM import depends on it.

### Files

| File | Responsibility |
|---|---|
| `src/lib/api/sse.ts` | `subscribe(teamId, jobId, callback)`; `unsubscribe(jobId)`; singleton `EventSource` per team |
| `src/lib/components/common/JobProgress.svelte` | `jobId` prop; subscribes to SSE; renders pending/running/done/error states; emits `complete` event |
| `src/lib/components/bom/BOMImportProgress.svelte` | Extends `JobProgress`; parses per-item resolution events; shows `Resolved N/M` counter |

SSE event schema (from backend `StreamEvents` docstring):
```ts
interface SseEvent {
  type: string;    // e.g. 'part_enrichment_progress', 'listing_refresh_done'
  job_id: string;
  team_id: string;
  data: Record<string, unknown>;
}
```

**Acceptance:** Trigger a part enrichment; `JobProgress` transitions through states and fires `complete`; query cache is invalidated via the `onComplete` callback.

---

## Phase 7 — Projects + BOM

### Files

| File | Responsibility |
|---|---|
| `src/lib/types/projects.ts` | `Project`, `Revision`, `BOMLineItem`, `Substitute` |
| `src/lib/api/projects.ts` | `listProjects`, `createProject`, `getProject`, `updateProject` |
| `src/lib/api/revisions.ts` | `listRevisions`, `importRevision`, `getRevision`, `updateRevision`, `listBOMLineItems`, `updateBOMLineItem`, `addSubstitute`, `removeSubstitute` |
| `src/lib/api/stockcheck.ts` | `getStockCheck(teamId, revisionId, buildQty)` |
| `src/lib/components/bom/BOMLineItemRow.svelte` | Ref designator, qty, raw description, resolve status, PartSearch to resolve, substitute count |
| `src/lib/components/bom/SubstituteManager.svelte` | Expandable panel; PartSearch to add; remove per entry |
| `src/lib/components/bom/StockCheckRow.svelte` | Required / on-hand / shortfall; "Add to cart" per row |
| `src/routes/team/Projects.svelte` | Project list + create dialog |
| `src/routes/team/ProjectDetail.svelte` | Revision list; "Import BOM" side panel with CSV upload and column mapping |
| `src/routes/team/RevisionDetail.svelte` | Line items table; BOMImportProgress banner; "Set Active" |
| `src/routes/team/StockCheck.svelte` | Build qty input; StockCheckRow table; "Add All Shortfalls to Cart" |

BOM import: `importRevision` → 202 `{ job_id, bom_revision_id }` → show `BOMImportProgress` → on `complete`, invalidate revision query.

**Acceptance:** Full BOM import → SSE progress → line item resolution flow works end-to-end. Stock check with build qty correctly shows shortfalls.

---

## Phase 8 — Cart + Orders

### Files

| File | Responsibility |
|---|---|
| `src/lib/types/orders.ts` | `CartItem`, `Order`, `OrderLineItem` |
| `src/lib/api/cart.ts` | `getCart`, `addCartItem`, `updateCartItem`, `removeCartItem`, `addShortfall`, `addLowStock`, `convertCart` |
| `src/lib/api/orders.ts` | `listOrders`, `getOrder`, `updateOrderLineItem`, `removeOrderLineItem`, `submitOrder`, `exportOrder`, `markPlaced`, `receiveLineItem` |
| `src/lib/components/cart/CartLineItem.svelte` | Qty input, ListingPicker, source badge, subtotal, remove |
| `src/lib/components/cart/CartSummary.svelte` | Per-distributor cost breakdown; "Convert to Orders" button |
| `src/lib/components/orders/OrderStatusBadge.svelte` | Draft/Placed/Exported/Partially Received/Received |
| `src/lib/components/orders/ReceiveLineItem.svelte` | Ordered qty display + received qty input |
| `src/routes/team/Cart.svelte` | Cart items + CartSummary; "Add Low Stock" action |
| `src/routes/team/Orders.svelte` | Tabbed order list |
| `src/routes/team/OrderDetail.svelte` | Line items, receive flow, export CSV link, submit via API |

**Acceptance:** Cart → convert → draft order → receive line items → inventory quantity increases.

---

## Phase 9 — Builds + Checkouts

### Files

| File | Responsibility |
|---|---|
| `src/lib/api/builds.ts` | `listBuilds`, `previewBuild`, `createBuild`, `getBuild` |
| `src/lib/api/checkouts.ts` | `listCheckouts`, `createCheckout`, `getCheckout` |
| `src/routes/team/Builds.svelte` | Build history table; "Record Build" multi-step dialog (pick project → qty → preview → confirm) |
| `src/routes/team/BuildDetail.svelte` | Build metadata + consumed parts table |
| `src/routes/team/Checkouts.svelte` | Checkout history table; "New Checkout" dialog |

**Acceptance:** Recording a build deducts inventory; checkouts create adjustment records.

---

## Phase 10 — Settings Pages

### Team settings

| Page | File | Key functionality |
|---|---|---|
| Team | `settings/Team.svelte` | Edit team name/description (admin only) |
| Members | `settings/Members.svelte` | Member table + add member dialog + role change + remove |
| Distributors | `settings/Distributors.svelte` | Per-distributor toggle + config dialog (credentials + cache duration) |

### User settings

| Page | File | Key functionality |
|---|---|---|
| Profile | `settings/Profile.svelte` | Edit display name, change email |
| Sessions | `settings/Sessions.svelte` | List sessions + revoke; current session highlighted |
| Tokens | `settings/Tokens.svelte` | List tokens + create (one-time token display with CopyButton) + revoke |

API modules:
- `src/lib/api/sessions.ts` — `listSessions`, `revokeSession`
- `src/lib/api/tokens.ts` — `listTokens`, `createToken`, `revokeToken`
- `src/lib/api/preferences.ts` — `getPreferences`, `savePreferences`

Wire `preferencesStore` to call `savePreferences` on write.

**Acceptance:** Distributor config persists across page reload; token shows once then is hidden; revoking a session removes it from the list.

---

## Phase 11 — Polish + Build Integration

### Functional completeness checks

- [ ] All 22 routes resolve without console errors
- [ ] Empty states shown on every list page when no data
- [ ] Error toasts shown on all mutation failures
- [ ] Role-gated buttons disabled (not hidden) with tooltips for insufficient role
- [ ] All price strings rendered via `PriceDisplay`, never raw
- [ ] All timestamps rendered via `DateDisplay`
- [ ] `TeamSwitcher` works from any team-scoped route

### Build integration verification

```bash
cd web && npm run build          # populates web/dist/
cd ..  && go build ./...         # dist/ is embedded; binary size increases
./partrack                       # serve; navigate to / in browser
```

The Go server must serve `index.html` for `GET /` and all non-`/api/` GETs (already implemented via the fallback handler).

### Final Vite config items

- `base: '/'` (default; no sub-path deployment assumed)
- `build.sourcemap: false` for production
- `preview.proxy` mirrors `server.proxy` for `npm run preview`

---

## Dependency Order Summary

```
Phase 0 (scaffold)
  └── Phase 1 (router + shell)
        └── Phase 2 (auth + login)
              └── Phase 3 (UI primitives)
                    ├── Phase 4 (parts)
                    ├── Phase 5 (inventory + storage)
                    ├── Phase 6 (SSE + JobProgress)  ← required before Phase 7
                    │     └── Phase 7 (projects + BOM)
                    │           └── Phase 8 (cart + orders)
                    │                 └── Phase 9 (builds + checkouts)
                    └── Phase 10 (settings)  ← independent; can run in parallel with 4–9
                          └── Phase 11 (polish + build)
```

---

## Critical Files (first to be written, referenced by everything)

| File | Why critical |
|---|---|
| `src/lib/api/client.ts` | Base of all API calls |
| `src/lib/router/index.ts` | Navigation primitive used in every page |
| `src/lib/stores/auth.ts` | Guards and TeamSwitcher depend on it |
| `src/lib/types/*.ts` | TypeScript contract for API shapes — write before implementing pages |
| `web/vite.config.ts` | API proxy + output path affect everything downstream |

---

## Progress Tracker

| Phase | Status |
|---|---|
| Phase 0 — Scaffold | ✅ Complete |
| Phase 1 — Router + App Shell | ✅ Complete |
| Phase 2 — Auth Store + Login | ✅ Complete |
| Phase 3 — UI Primitives | ⬜ Pending |
| Phase 4 — Parts Catalog | ⬜ Pending |
| Phase 5 — Inventory + Storage | ⬜ Pending |
| Phase 6 — SSE Module + JobProgress | ⬜ Pending |
| Phase 7 — Projects + BOM | ⬜ Pending |
| Phase 8 — Cart + Orders | ⬜ Pending |
| Phase 9 — Builds + Checkouts | ⬜ Pending |
| Phase 10 — Settings Pages | ⬜ Pending |
| Phase 11 — Polish + Build Integration | ⬜ Pending |
