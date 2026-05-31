# partrack — Security Remediation Plan

Companion to [findings.md](findings.md). Sequenced by risk and dependency.
Effort: S = hours, M = a day, L = multi-day.

> **Status (2026-05-31): Phases 1–4 implemented and tested on `master`.**
> - **P1** ownership guards + immutable resource-owner cache; **P2** scan-channel
>   binding; **P3** scope-based manual-edit authz; **P4** auth hardening:
>   rate limiting (go-chi/httprate, configurable, `Auth.RateLimitPerMinute`),
>   login enumeration/timing fix (unified error + dummy bcrypt), Secure cookies
>   (`App.SecureCookies`), startup `session_secret` validation, CSRF Origin-check
>   middleware + `oauth/initiate` GET→POST, 16 MiB body cap, and the **scan-ticket**
>   system (single-use pairing + stream tickets; backend + frontend pairing/QR-
>   refresh/reconnect). Durable scan token no longer appears in any URL.
> - **Deferred (4g, low priority):** session idle timeout, API-token cache eviction
>   on direct DB delete.
> - **Refinement vs. plan:** manual **listing** edits treat global parts as
>   shared-editable (catalog data, like enrichment) and only block *another team's*
>   team-scoped part; user **parameters** keep the strict global→admin gate (P3).
> - **Pre-existing, unrelated failing/flaky tests (not from this work):**
>   `internal/scanner` `TestParse_Mouser_RealWorld` (deterministic, untouched pkg)
>   and handler `TestPartialCheckout_AssembledQtyTooLarge` (async build race).

## Guiding decisions (from review discussion)
- **Distributor enrichment is global-safe.** Pulling trusted distributor data
  (`EnrichPart`, `RefreshListing`, `RefreshAllListings`, `ReEnrichAll`,
  `EnrichBOMItem`) onto a global/shared part is intended behaviour. These stay
  member+; they only ever write `source_type='distributor'` data and use the
  calling team's credentials. No authz change needed — **but** they must never
  touch the ledger or write `source_type='user'` data (verify in Phase 3).
- **Manual edits and the ledger are not global-safe.** Manual parameter/listing
  edits to a global part, and any stock-ledger append, must be authorised against
  the owning team (or instance admin for global parts).

---

## Phase 1 — Close the cross-team IDOR surface (A1, A5, A2-inventory) — L, highest priority

Root cause: object-level authz is per-handler and the projects/revisions/BOM
subtree has none. Fix **fail-closed at the data layer** so a foreign ID returns
zero rows (→ 404), which can't be forgotten the way a guard call can.

**1a. Team-anchor the BOM subtree queries.** Thread `team_id` into the leaf
queries and add a `JOIN projects p ON p.id = <rev/item>.… AND p.team_id = $teamId`
(or `WHERE … AND p.team_id = $teamId`). Queries to change (db/queries/):
- `revisions.sql`: `GetRevision`, `ListRevisions`, `UpdateRevision`,
  `CreateRevision`/`CreateBOMLineItem`/`SetProjectActiveBOM`/`SetProjectBOMImportConfig`
  (validate the project is in-team before insert), `ListBOMLineItems*`,
  `UpdateBOMLineItem`, `IgnoreBOMLineItem`.
- `substitutes.sql`: `AddSubstitute`, `RemoveSubstitute`, `ReorderSubstitutes`,
  `ImportSubstitutesFromRevision` (scope **both** target and `source_revision_id`
  to the team), `ListSubstitutesForLineItem`, `ListAllSubstitutesForRevision`.
- `stockcheck` path (`GetRevision` + `ListBOMLineItems`).
Handlers already hold the verified `teamID` from `RequireTeamRole`'s
`MembershipFromContext`; pass it in. `sqlc generate`, then `go build ./...`.

**Alternative / complement (readability):** add chi nested-route middleware
`RequireProjectInTeam` → `RequireRevisionInProject` that resolve+verify the chain
once and 404 on mismatch, mirroring the existing `RequireTeamRole` pattern. Useful
for path-param chains, but body-supplied IDs (1b) still need data-layer scoping, so
do 1a regardless.

**1b. Ledger & inventory cross-team writes (the "ledger isn't global" cases).**
- `CreateCheckout` `inventory_entry_id` branch ([checkouts.go:74](../../internal/handler/checkouts.go#L74)):
  add `AND team_id = $2` to `GetInventoryEntryForUpdate`
  ([inventory.sql:148](../../db/queries/inventory.sql#L148)) and pass the team — so
  another team's entry can't be deducted/reconciled.
- `ListAdjustments` ([inventory.go:330](../../internal/handler/inventory.go#L330)):
  verify the entry via team-scoped `GetInventoryEntry(entryID, teamID)` first (or
  join `inventory_entries.team_id` into `CountAdjustments`/`ListAdjustments`).
- `CreateBuild`/`PreviewBuild`: verify `project_id`/`bom_revision_id` are in-team
  (covered once 1a scopes the revision lookups they call).

**1c. Global `/parts` inventory read (A2).** `GetPart`/`ListParts` accept
`?team_id=` with no membership check. Gate it: if `team_id` is present, call
`h.checkTeamMembership(ctx, teamID, userID)` ([handler.go:134](../../internal/handler/handler.go#L134))
and 403 if not a member. (Longer term, consider moving inventory-augmented reads
under `/teams/{teamId}/parts/{partId}`.)

**1d. Regression tests.** Add cross-team negative tests (member of A → B's
project/revision/item/checkout/inventory entry → expect 403/404) to the existing
`*_test.go` harness so this can't regress.

---

## Phase 2 — Scan-channel object binding (A4) — M

Scan handlers authenticate by a `scan_` token bound to one (user, team); they must
derive **all** scoping from that session, never from request IDs.
- `SubmitScan` ([scan.go:1053](../../internal/handler/scan.go#L1053)): scope
  `GetOrderLineItemsForScan` by `sess.TeamID` (add `AND o.team_id = $2`) or verify
  the `order_id` belongs to the session's team before lookup.
- Bulk-scan phone handlers `AddBulkScanItem`/`PatchBulkScanItem`/`Close`/`Navigate`
  ([scan_bulk.go](../../internal/handler/scan_bulk.go)): resolve the session via a
  team+user-scoped `GetBulkScanSession(id, team_id)` (as the desktop path already
  does) and reject if it isn't the token's session; scope the item queries by that
  verified session.
- `PushScanContext` ([scan.go:274](../../internal/handler/scan.go#L274)): verify
  `{sessionId}` belongs to `{teamId}` before `ScanHub.Send` (low impact, cheap).

---

## Phase 3 — Global manual-edit authz (A2 params, A3 manual listings) — M

Keep enrichment global (above); restrict **manual** edits by part scope.
- Bind delete to its parent: `DeleteParameter` →
  `WHERE id=$1 AND part_id=$2 AND source_type='user'`
  ([parts.sql:239](../../db/queries/parts.sql#L239)); pass `{partId}`.
- Scope-based gate for `CreateParameter`/`DeleteParameter`/`ListParameters`
  ([parts_parameters.go](../../internal/handler/parts_parameters.go)) and manual
  `AddListing`/`DeleteListing` ([parts_listings.go](../../internal/handler/parts_listings.go)):
  load the part; if `part.Scope=="global"` require `IsAdmin`; if `"team"` require
  membership (member+) in that part's team via `checkTeamMembership`. The
  `/parts/{partId}/parameters` routes currently sit in the non-team auth group
  (routes.go:127-129) with no team context — this gate supplies it.
- Confirm `EnrichPart`/`RefreshListing`/`ReEnrichAll`/`EnrichBOMItem` only write
  `source_type='distributor'` and never append to `stock_adjustments` (spot-check;
  `EnrichBOMItem`'s inventory touch is the caller-team's zero entry, which is fine).

---

## Phase 4 — Auth hardening (B1–B6) — M/L

**4a. Abuse resistance (B1) — S/M.** Add an in-memory rate-limit middleware
(`go-chi/httprate`, fits the single-process model; or hand-rolled `x/time/rate`)
on `POST /auth/login`, `GET/POST /auth/reset-password`, `POST /tokens`, and the
`/scan/*` group — including the new unauthenticated `POST /scan/pair` (per-IP) and
the authenticated `POST /scan/stream-ticket` (per-session): per-IP and per-account,
with backoff/lockout. Add a global `http.MaxBytesReader` body cap in the middleware
chain (also blunts bcrypt-DoS).

**4b. Login enumeration & timing (B2) — S.** Collapse unknown-user / bad-password /
no-credential into one generic `401 INVALID_CREDENTIALS`
([auth.go:132-159](../../internal/handler/auth.go#L132-L159)); fold
`ACCOUNT_INACTIVE` into the same generic response. Run a dummy
`bcrypt.CompareHashAndPassword` against a fixed hash on the no-user/no-credential
path to equalise timing.

**4c. Scan token off the wire/logs (B3) — M.** Replace every URL-borne scan token
with **single-use, short-lived tickets**, so a ticket caught in any log / proxy /
browser-history / `Referer` is already spent or expired and therefore worthless.
**No schema change** — the durable `scan_sessions` record is unchanged; only token
*delivery* and *stream* auth change. Plan from
[threat-model.md](threat-model.md) open-question #4.

Ticket store (in-process; matches arch decision #12, mirrors `AuthCache`/`ScanHub`):
- `map[sha256(rawTicket)]{kind, scanSessionID, expiresAt}` guarded by a mutex.
- `Mint(kind, sessionID)` → 32-byte `crypto/rand` ticket, store the hash, per-kind
  TTL. `Consume(raw, kind)` does lookup+`delete` **under one lock** — atomic
  single-use; a racing second use finds nothing. Lazy/periodic sweep caps growth.
  Minting is only reachable from authenticated actions, so volume is bounded.

Pairing (replaces the durable token in the QR — `CreateScanSession`
[scan.go:168](../../internal/handler/scan.go#L168) currently returns raw `token`):
- `CreateScanSession` returns `pairing_ticket` (not `token`). Desktop renders it
  into the QR in the URL **fragment** (`…/scan#pt=…`) so it never hits the server
  on the phone's initial load.
- **Periodic refresh:** pairing ticket TTL **60s**, desktop re-mints every **30s**
  and updates the QR — overlapping windows mean no pairing gap. Add
  `POST /scan-sessions/{sessionId}/pairing-ticket` (authenticated, member+) to
  re-mint for the existing session, since `CreateScanSession` 409s when a session
  already exists.
- `POST /scan/pair` (unauthenticated — the ticket is the credential;
  **rate-limited**, see 4a) consumes the pairing ticket and returns
  `{ token, session }`. Phone holds the durable token in JS and uses
  `Authorization: Bearer` for all REST `/scan/*` calls thereafter.

Stream (replaces `?token=` on the SSE connect):
- `POST /scan/stream-ticket` (authenticated, Bearer scan token; **rate-limited**)
  mints a single-use **30s** stream ticket bound to the session → `{ ticket }`.
- `GET /scan/stream?ticket=…` ([StreamScanSession scan.go:333](../../internal/handler/scan.go#L333))
  atomically consumes the ticket; **delete the `?token=` branch** in
  `scanSessionFromScanToken` ([scan.go:32-34](../../internal/handler/scan.go#L32-L34)).
- **Reconnect contract (required for this to work):** on a spent/invalid/expired
  ticket the server returns `401` with code `STREAM_TICKET_INVALID` **before**
  opening the event stream. The client treats *any* stream error as "mint a fresh
  stream ticket and reconnect" (capped backoff) rather than letting native
  `EventSource` retry the same spent URL — adapt the existing reconnect logic
  (commit 4c3b282, "EventSource state tracking and reconnect invalidation").

Result: no scan token ever appears in a URL, so scan-log redaction is unnecessary.
(Optional further hardening: `POST /scan/pair` could set the token as an HttpOnly
cookie scoped to `/api/v1/scan` instead of returning it in the body, keeping it out
of JS too — larger change to the phone's auth model, deferred.)

The **reset-link** `GET /auth/reset-password?token=` ([auth.go:466](../../internal/handler/auth.go#L466))
is a link by nature; handle its log exposure with a request-logger that redacts
`token=` (it is already short-TTL + single-use), separate from the scan change.

Routes delta: `CreateScanSession` return `token`→`pairing_ticket`; add
`POST /scan/pair` and `GET /scan/stream` (now `?ticket=`) to the unauthenticated
`/scan` area; add `POST /scan/stream-ticket` and
`POST /scan-sessions/{sessionId}/pairing-ticket` to the Bearer-authenticated group;
wire a `ticketStore` into `Handler`.

**4d. Cookies `Secure` (B4) — S.** Set `Secure` on the session, logout-clear, and
OIDC-state cookies. Since TLS terminates at the proxy, drive it from config
(`app.secure_cookies`, default true) and/or honour `X-Forwarded-Proto=https` from
`trusted_proxies`.

**4e. `session_secret` validation (B5) — S.** At startup, require it to be
non-empty and ≥32 bytes; fail fast (`os.Exit`) otherwise. It is the HMAC key for
OAuth state and link tokens.

**4f. CSRF defense-in-depth (B6) — S/M.** Add an `Origin`/`Sec-Fetch-Site` check
middleware for cookie-authenticated unsafe methods (Bearer clients are exempt), and
convert the side-effecting `GET /oauth/{apiType}/initiate` to `POST`
([routes.go:307](../../internal/server/routes.go#L307)). `SameSite=Lax` stays as
the baseline.

**4g. Lower priority — S.** Session idle timeout using the tracked `last_seen_at`;
evict API-token cache entries on DB-side deletion (or accept restart-bounded
staleness).

---

## Suggested sequencing
1. **Phase 1** (IDOR + ledger) — the dominant risk; do first, with regression tests.
2. **Phase 4a + 4d + 4e** — cheap, broad guardrails (rate limit, Secure cookies,
   secret validation) that shrink blast radius immediately.
3. **Phase 2** (scan binding) and **Phase 3** (manual-edit authz).
4. **Phase 4b/4c/4f** — enumeration, scan-token lifecycle, CSRF.
5. **Phase 4g** — cleanups.
</content>
