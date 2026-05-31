# partrack — Security Review Findings

Review date: 2026-05-31. Scope: cross-team authorization (IDOR/BOLA) and auth
hardening, per [threat-model.md](threat-model.md). Findings are observations;
remediation is discussed separately. All `file:line` refs verified against source.

Severity = exploitability × impact for a source-code-aware attacker who holds a
valid account (the realistic threat for a multi-team instance).

---

## A. Authorization — systemic cross-team IDOR (T1)

`RequireTeamRole` only proves the caller belongs to the `{teamId}` in the URL; it
does **not** check that nested resource IDs belong to that team. Many handlers
load/mutate by bare UUID, so a member of team A can target team B's resources by
calling `/teams/{A}/...` with B's IDs.

### A1. Project / Revision / BOM subtree — no team binding anywhere [HIGH]
Root cause: queries scope by `project_id` / `bom_revision_id` / `bom_line_item_id`
but never join back to `projects.team_id`. `GetRevision` is `WHERE br.id=$1 AND
br.project_id=$2` ([revisions.sql:25](../../db/queries/revisions.sql#L25)) — joins
`projects` but does not filter by team. Affected (member+ of any team can read or
mutate another team's data):

- Read: `ListRevisions`, `GetRevision` ([revisions.go](../../internal/handler/revisions.go)),
  `StockCheck` ([stockcheck.go:295](../../internal/handler/stockcheck.go#L295)),
  `ListSubstitutes` / `ListPreviousRevisionsWithSubstitutes`
  ([items.go](../../internal/handler/items.go), [substitutes.go](../../internal/handler/substitutes.go)).
  Leaks full BOM structure: designators, MPNs, manufacturers, per-board qty,
  resolution/enrichment data.
- Write: `UpdateRevision`, `ImportRevision` (inject revisions, flip `active_bom`,
  overwrite import config), `UpdateBOMItem`, `EnrichBOMItem`, `AddSubstitute`,
  `RemoveSubstitute`, `ReorderSubstitutes`, `ImportSubstitutes` (copies from an
  arbitrary body-supplied `source_revision_id`).

### A2. Global `/parts` routes — `Authenticate` only, no team/role [HIGH]
Routes at [routes.go:127-129](../../internal/server/routes.go#L127-L129) and
:122/:124 sit outside any team guard:
- `DeleteParameter` ([parts_parameters.go:116](../../internal/handler/parts_parameters.go#L116))
  = `DELETE ... WHERE id=$1 AND source_type='user'` — **not bound to `{partId}`**.
  Any authenticated user deletes any user parameter on any part.
- `CreateParameter` / `ListParameters` — any authenticated user adds/reads user
  parameters on **any** part, including global parts (affects all teams) and other
  teams' parts.
- `GetPart` / `ListParts` with `?team_id=` — `team_id` flows into
  `GetPartWithInventory`/`ListPartsWithInventory` with no membership check, leaking
  **any team's** stock levels, min/reorder levels, preferred listing, and storage
  locations to any authenticated user.

### A3. Listing / enrich mutations reach global & other-team parts [MED]
`AddListing`, `DeleteListing` (cascades `DeleteDistributorParameters`),
`RefreshListing`, `RefreshAllListings` ([parts_listings.go](../../internal/handler/parts_listings.go)),
`EnrichPart` ([parts_enrich.go:88](../../internal/handler/parts_enrich.go#L88)):
member+ of team A loads the part/listing by bare UUID, so can mutate listings /
trigger enrichment on a **global** part (visible to all teams) or team B's part.

### A4. Scan channel — request-supplied IDs not bound to the token's session [HIGH]
Scan routes authenticate by `scan_` token (a session bound to one user+team) but
several trust IDs from the request:
- `SubmitScan` ([scan.go:1053](../../internal/handler/scan.go#L1053)) — `order_id`
  from the body into `GetOrderLineItemsForScan` (`WHERE oli.order_id=$1`, no team).
  Any scan-token holder reads another team's order line items (MPN, SKU, qty
  ordered/received). (`ConfirmScan` re-checks team — so read-only disclosure.)
- Bulk scan `AddBulkScanItem` / `PatchBulkScanItem`
  ([scan_bulk.go:65](../../internal/handler/scan_bulk.go#L65),
  [:275](../../internal/handler/scan_bulk.go#L275)) — `{sessionId}` from URL never
  checked against the token's user/team; can inject/overwrite items in another
  user's/team's bulk session and trigger resolve jobs.
- `PushScanContext` ([scan.go:274](../../internal/handler/scan.go#L274)) —
  `{sessionId}` not validated to team before `ScanHub.Send` (low impact).

### A5. Core-domain cross-team writes/reads [MED]
- `CreateCheckout` ([checkouts.go:74](../../internal/handler/checkouts.go#L74)) —
  the `inventory_entry_id` branch uses `GetInventoryEntryForUpdate` (`WHERE id=$1`,
  no team), then deducts/reconciles stock. A member of team A can **deduct or
  reconcile team B's stock** (cross-team write). The `part_id` branch is safe.
- `ListAdjustments` ([inventory.go:330](../../internal/handler/inventory.go#L330)) —
  `entryId` not team-scoped; reads any team's full stock ledger.
- `CreateBuild` / `PreviewBuild` ([builds.go](../../internal/handler/builds.go)) —
  do not verify `project_id`/`bom_revision_id` belong to the team; allows referencing
  a foreign revision and reading its BOM line items (LOW; inventory stays in-team).

### Confirmed SAFE (correctly team-scoped)
Orders, Builds (by `buildId`), Cart, Storage, Inventory get/update/create/adjust,
Dashboard, Search, Checkout reads, `parts_resolve`, `distributors`,
`CreatePart`/`UpdatePart`/`DeletePart`, desktop bulk-scan & kitting, most scan
lookup/mutation handlers (derive team from token, re-check role).

---

## B. Authentication hardening (S1/S2)

### B1. No rate limiting / lockout anywhere [HIGH]
Middleware chain is `RequestID, RealIP, timing, Logger, Recoverer`
([server.go:31-36](../../internal/server/server.go#L31-L36)) — nothing throttles
`POST /auth/login`, `/auth/reset-password`, or `/tokens`. Enables online
brute-force / password spray, reset-token guessing, and **bcrypt-DoS** (each login
to a real account runs cost-10 bcrypt synchronously; no concurrency cap).

### B2. Login user enumeration + timing oracle [HIGH]
Distinct 401 codes ([auth.go:132-159](../../internal/handler/auth.go#L132-L159)):
`INVALID_CREDENTIALS` (unknown/bad-pw), `ACCOUNT_INACTIVE`, `NO_PASSWORD` — the
last two confirm account existence / SSO-only accounts. Early returns also skip
bcrypt, so a timing side channel reveals account existence even if codes are
unified (no dummy-hash equalization).

### B3. Scan token in URL, logged, long-lived, reusable [HIGH]
`GET /scan/stream?token=scan_...` ([scan.go:33](../../internal/handler/scan.go#L33),
[routes.go:32](../../internal/server/routes.go#L32)). `chimw.Logger` logs the full
URI → tokens written to logs in cleartext. TTL is **30 days**
([scan.go:25](../../internal/handler/scan.go#L25)), reusable across all `/scan/*`
endpoints, **not single-use**, revocable only via the purchaser/admin
`DeleteScanSession`. Token entropy (32B CSPRNG) and hashed-at-rest storage are
fine. (Reset token is likewise in the query string → logged — LOW.)

### B4. Cookies lack the `Secure` flag [HIGH]
`setSessionCookie` and the OIDC state cookie set `HttpOnly`+`SameSite=Lax` but no
`Secure` ([auth.go:451-460](../../internal/handler/auth.go#L451-L460)). TLS
terminates at the proxy (`r.TLS == nil`), so the app never sets it — the 30-day
session cookie can traverse a plaintext hop.

### B5. `session_secret` never validated [MED]
Loaded from YAML/env with no non-empty/min-length check
([config.go:88-90](../../internal/config/config.go#L88-L90)). It is the HMAC key
for OAuth `state` ([oauth.go](../../internal/handler/oauth.go)) and link tokens
([token.go](../../internal/handler/token.go)). If empty/weak, an attacker forges
OAuth state and writes distributor tokens under another team/user.

### B6. CSRF: cookie auth, `SameSite=Lax`, no token [HIGH residual]
No anti-CSRF token, Origin/Referer check, or Content-Type enforcement on mutating
handlers. `Lax` blocks cross-site POST/PATCH/DELETE (covers most JSON mutations)
but **allows cross-site top-level GET** — and side-effecting GETs exist:
`GET /oauth/{apiType}/initiate` ([routes.go:307](../../internal/server/routes.go#L307)),
`GET /orders/{orderId}/export`. No defense-in-depth.

### B7. Lower severity
- Session TTL 30 days, no idle timeout / rotation ([auth.go:27](../../internal/handler/auth.go#L27)) [LOW].
- API-token cache entry without `expires_at` never evicts until restart; a DB-side
  delete (not via API) stays valid in cache ([middleware/auth.go:175](../../internal/middleware/auth.go#L175)) [LOW].
- No global request-body size limit; compounds bcrypt-DoS [NEEDS-REVIEW].
- `/scan/stream` trusts `?token=` outside `Authenticate` (validated in-handler);
  ensure `trusted_proxies` is set in prod so `X-Forwarded-For`/audit IPs aren't
  spoofable [NEEDS-REVIEW].

---

## Confirmed-good controls
bcrypt passwords; 32-byte CSPRNG tokens, SHA-256-hashed at rest, raw shown once;
expired session/API/scan tokens filtered in SQL; revocation evicts the auth cache;
OIDC state CSRF + id_token audience verification; HMAC constant-time compares;
sqlc parameterized queries (low SQLi exposure); admin-only password-reset-link
generation with 24h single-use tokens.

---

## Root-cause themes (for the remediation discussion)
1. **Object-level authz is per-handler and inconsistent.** The projects/revisions/
   items/substitutes subtree has *no* team binding at all; global `/parts` routes
   have none by design but expose team data. A centralized team-ownership guard
   (or team-anchored queries) would close most of section A at once.
2. **Secrets/tokens travel in URLs and logs** (scan + reset tokens), and the scan
   token is long-lived — the S2 thread from the threat model, now concrete.
3. **No abuse-resistance layer** (rate limit / lockout / body cap) on
   unauthenticated endpoints.
</content>
