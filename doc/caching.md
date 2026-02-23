# partrack — Caching Strategy

## Guiding Constraints

- **Single monolithic process:** all state mutations (login, logout, role changes, member removal, user deactivation) pass through the same Go binary. Cache invalidation can therefore be immediate and precise — there is no inter-process staleness problem and no need for short TTLs as a correctness hedge.
- **No distributed infrastructure:** caches are in-process only. No Redis, no Memcached.
- **Low complexity:** a cache is only added where the hot-path benefit is clear and the implementation is simple.
- **Distributor listing data and part parameters are out of scope** for this document. Listing data is already cached in the DB row via `last_refreshed_at` + `cache_duration_seconds`; parameter caching is deferred.

---

## What Should Be Cached

### 1. Session Token and API Token Validation

**Why it matters:** `GetUserBySessionToken` or `GetUserByAPIToken` runs on every authenticated request. These are the highest-frequency queries in the system — one DB round-trip per request just to confirm "this token is still valid."

**Cache design:**

| Cache | Key | Value | Eviction |
|---|---|---|---|
| Token cache | SHA-256(raw token) | `User` struct + `expiresAt` timestamp | Explicit on logout / token revocation; also dropped if `expiresAt` has passed |

On a cache hit, the handler checks `expiresAt > now()` before trusting the entry. This means time-based token expiry still works correctly without any TTL sweep; it just shifts the check from SQL (`expires_at > NOW()`) to an in-memory comparison.

**Invalidation sites:**
- `DELETE /api/v1/auth/logout` — evicts the calling user's token hash
- Admin "revoke all sessions for user" — evicts all token hashes belonging to that user (requires a reverse index: `userID → []tokenHash`, or a full scan of the small cache)
- API token deletion — evicts the deleted token's hash

**Write-behind for last-seen timestamps:** `UpdateSessionLastSeen` and `UpdateAPITokenLastUsed` must not block request handling. The cache entry stores a `lastSeenAt time.Time` that is updated in-memory on every cache hit. A single background goroutine flushes dirty entries to the DB on a fixed interval (e.g., every 5 minutes). On application crash, up to one interval's worth of `last_seen_at` / `last_used_at` precision is lost — this is explicitly acceptable. The values are audit data only; correctness of access control does not depend on them.

The flusher marks entries as clean after writing. Only entries dirtied since the last flush are written, so the DB write volume is bounded by the number of distinct active tokens, not the request rate.

---

### 2. Team Membership (Role Check)

**Why it matters:** `GetTeamMembership` runs on every team-scoped request — every inventory view, every BOM page, every cart action. It is the second-most-frequent query class after token validation.

**Cache design:**

| Cache | Key | Value | Eviction |
|---|---|---|---|
| Membership cache | `(teamID, userID)` | `role` string + `membershipID` | Explicit only |

Because all role and membership changes happen within this process, there is no need for a passive TTL. The cache entry is considered authoritative until explicitly evicted.

**Invalidation sites:**
- `PUT /api/v1/teams/{teamId}/members/{userId}` (role update) — evicts `(teamId, userId)`
- `DELETE /api/v1/teams/{teamId}/members/{userId}` (member removal) — evicts `(teamId, userId)`
- User deactivation — evicts all entries where `userID` matches (requires scanning the cache or a reverse index; the volume is small)

**Negative entries:** If `GetTeamMembership` returns `pgx.ErrNoRows`, the middleware currently returns 403 immediately. There is no benefit to caching the negative (the user is either not a member or was just removed; either way, 403 is instant from the DB and negative entries would just add eviction complexity).

---

## What Should Not Be Cached

| Data | Reason |
|---|---|
| **Inventory quantities** | Must be accurate. A stale count causes incorrect shortfall calculations and procurement decisions. |
| **Stock adjustments** | Immutable ledger, not a hot read path, no benefit. |
| **Shopping cart contents** | Mutated frequently; multiple team members may share a cart. Must be consistent. |
| **Order state** | Status transitions must be immediately visible to all users. |
| **BOM line items and resolutions** | Project-specific, not a shared hot path. |
| **Parts and parameters** | Deferred; revisit if profiling shows a need. |
| **Distributor listing data** | Already handled by the DB-row cache pattern (`last_refreshed_at`). Deferred. |

---

## HTTP Cache-Control Headers

Setting `Cache-Control` response headers lets the browser avoid redundant requests for stable read-only data. This is zero-cost backend work that directly reduces perceived latency in the Svelte frontend.

| Endpoint | Header | Rationale |
|---|---|---|
| `GET /api/v1/distributors` | `Cache-Control: private, max-age=3600` | Global, changes only on migration or admin action |
| `GET /api/v1/parts` (list) | `Cache-Control: private, max-age=30` | Stable between enrichments; short enough to reflect newly added parts |
| `GET /api/v1/parts/{id}` | `Cache-Control: private, max-age=60` | Enrichment completion is signalled via SSE; frontend can invalidate on that event |
| All mutating endpoints | `Cache-Control: no-store` | Never cache writes |
| SSE endpoints | N/A | Not cacheable |
| Auth endpoints | `Cache-Control: no-store` | Credentials must never be cached by a proxy or browser |

`private` is used throughout (rather than `public`) because responses may include team-scoped parts or team-contextual data. The distributor list could technically be `public`, but `private` is the safe uniform default.

---

## Implementation Notes

**Data structures:** Both caches can be a `sync.Map` (or a mutex-protected `map`) with struct values. The token cache value carries `{user User; expiresAt time.Time; lastSeenAt time.Time; dirty bool}`; the membership cache value carries `{role string; membershipID pgtype.UUID}`. No external library is required.

**Where they live:**
- Token cache and membership cache are constructed once in `server.New` and passed into the auth middleware constructors (`middleware.Authenticate`, `middleware.RequireTeamRole`).
- Handlers that perform invalidating writes (logout, role update, member removal, user deactivation) need access to the same cache instances. Pass them into `handler.Handler` alongside the DB pool.

**Background goroutine:** One goroutine serves two purposes: flushing dirty `lastSeenAt` values to the DB every 5 minutes, and lazily evicting expired token entries in the same pass. Membership entries have no TTL and are evicted only by explicit writes. A single goroutine is sufficient; it holds no locks longer than needed to snapshot the dirty set.

**Startup:** All caches start empty. The first request after a restart always hits the DB. No warm-up is required.