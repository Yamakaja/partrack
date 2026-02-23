# partrack — Stubs & Missing Implementations

Compiled from a full codebase survey of both the Go backend and Svelte frontend.
Ordered by priority: highest impact / most blocking first.

**Plans A, B, C, D, E are complete as of 2026-02-23.**

---

## P1 — Important Features Intentionally Deferred

### 1. Submit Order to Distributor API (`internal/handler/orders.go:344`)

**What it is:** `POST /api/v1/teams/{teamId}/orders/{orderId}/submit` — programmatic order submission to a distributor's ordering API.

**What currently happens:** Returns `501 Not Implemented`:
> "distributor API submission is not yet implemented; use export+reference flow instead"

**What it should do:** Call the distributor client's submit method (not yet defined on the `Client` interface), record the returned `distributor_order_ref`, and transition the order's `status` from `draft` → `placed`.

**Dependency:** Requires extending the `distributor.Client` interface (`internal/distributor/client.go`) with a `SubmitOrder` method and implementing it for each real distributor. For DigiKey this maps to either Ordering v3 (`POST /Ordering/v3/Orders`) or Quote v4 — both are documented in `doc/distributor/distributor-digikey.md`.

**Current workaround:** Users export a CSV and record the reference number manually.

---

## P2 — Features Present but Incomplete

### 2. User Profile Editing (Frontend + Backend)

**Frontend:** `web/src/routes/settings/Profile.svelte` — displays user info read-only with the message "Profile editing is not yet available." Fields for display name and email are `disabled`.

**Backend:** No `PATCH /api/v1/me` endpoint exists. The auth handler only exposes `GET /api/v1/me` (`GetMe`). No `UpdateUser` SQL query is defined.

**What's needed:**
- Backend: `PATCH /api/v1/me` handler + `UpdateUser` SQL query (`display_name`, optionally `email`)
- Frontend: enable the form fields and wire to the new endpoint

---

### 3. User Registration / Account Creation

**What it is:** There is no `POST /api/v1/auth/register` (or admin user-creation) HTTP endpoint. `CreateUser` exists as a SQL query and is called internally during some auth flows, but there is no user-facing way to create a new account.

**Current state:** The app is effectively invite-only — new users cannot self-register.

**Note:** May be intentional (closed beta / admin-seeded users). Clarify whether self-service registration is in scope before implementing.

---

## P3 — Polish / Minor Gaps

### 4. Unused `notImplemented()` Helper

**Location:** `internal/handler/handler.go:42`

```go
func (h *Handler) notImplemented(w http.ResponseWriter) { ... }
```

This helper was scaffolded but never called — handlers either fully implement or directly inline the 501 pattern. Use it in `SubmitOrder` or remove it.

---

### 5. Test Distributor Always Returns `stock_available: 0`

**Location:** `internal/distributor/testclient/client.go`

The test client hardcodes `StockAvailable = 0` for all LM317 variants. To test the "in-stock" display path without hitting the DigiKey sandbox, add a second MPN suffix convention (e.g. `LM317-INSTOCK`) that returns a nonzero stock value.

---

## Summary Table

| # | Area | Backend | Frontend | Priority | Plan |
|---|------|---------|----------|----------|------|
| 1 | BOM import pipeline | ✅ `handleBOMImport` implemented | ✅ SSE format matches | ~~P0~~ | ~~Plan A~~ |
| 2 | DigiKey API client | ✅ `internal/distributor/digikey/` complete | n/a | ~~P1~~ | ~~Plan B~~ |
| 3 | SubmitOrder | `internal/handler/orders.go:344` — 501 stub | button exists, disabled path | **P1** | — |
| 4 | BOM substitute parts | ✅ complete (`items.go`, SQL queries) | ✅ chips + inline search in `BOMLineItemRow` | ~~P2~~ | ~~Plan C~~ |
| 5 | Order export CSV | ✅ complete (`orders.go:271`) | ✅ Export/Re-export button in `OrderDetail.svelte` | ~~P2~~ | ~~Plan D~~ |
| 6 | Member user search | ✅ `SearchUsers` handler + route | ✅ typeahead in `Members.svelte` | ~~P3~~ | ~~Plan E~~ |
| 7 | Profile editing | no `PATCH /api/v1/me` | read-only display in `Profile.svelte` | **P2** | — |
| 8 | User registration | no `/auth/register` | n/a (may be intentional) | **P2** | — |
| 9 | Unused `notImplemented()` | `internal/handler/handler.go:42` | n/a | **P3** | — |
| 10 | Test distributor stock | `testclient` always returns 0 stock | n/a | **P3** | — |

## Remaining Open Work

| Priority | Item |
|----------|------|
| P1 | SubmitOrder — extend `distributor.Client` interface + implement for DigiKey (Ordering v3 or Quote v4) |
| P2 | User profile editing — `PATCH /api/v1/me` + `UpdateUser` SQL + enable `Profile.svelte` form |
| P2 | User registration — decide scope; implement if self-service is required |
| P3 | Remove or use `notImplemented()` in `handler.go` |
| P3 | Test distributor: add an in-stock MPN variant for UI testing |
