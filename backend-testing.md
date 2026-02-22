# Backend Testing

## Philosophy

**Integration-first, real database, no mocks.**

The handler layer is the core of the application and its correctness depends entirely on real SQL — filters, constraints, pagination, transactions, upserts. Testing handlers against a mock database would validate almost nothing meaningful. The approach:

- Every handler test sends a real HTTP request through the full Chi router and asserts on the real HTTP response.
- The database is real PostgreSQL, provided via `PARTRACK_TEST_DB_URL`.
- External services (OIDC, real distributors) are excluded: password-based auth is used in tests, and the `testclient` distributor handles enrichment jobs.
- No mocking framework. Standard library `testing` + `net/http/httptest` + `encoding/json`.

The only unit tests (no DB, no HTTP) are for pure logic: the auth cache (`internal/middleware/`).

---

## Test Infrastructure: `internal/testutil`

A single shared package provides all test helpers. Nothing here is part of the production binary.

### `db.go`

```go
// MustDB opens a pool to PARTRACK_TEST_DB_URL, runs all migrations, truncates
// all application tables, and returns the pool. Calls os.Exit(1) if the env
// var is absent and t is nil; calls t.Skip if t is non-nil.
func MustDB(t *testing.T) *pgxpool.Pool

// Truncate deletes all rows from every application table in dependency order
// using TRUNCATE ... CASCADE. Does not touch `distributors` (seeded by migration)
// or golang-migrate's schema_migrations table.
func Truncate(t *testing.T, db *pgxpool.Pool)
```

### `seed.go`

Direct DB insertion helpers — faster and more focused than going through the HTTP stack for test setup:

| Function | Description |
|---|---|
| `CreateUser(t, db, email, password, displayName)` | Inserts user + bcrypt credential; returns user UUID |
| `CreateSession(t, db, userID)` | Generates raw token, stores SHA-256 hash; returns raw token |
| `CreateAPIToken(t, db, userID, name)` | Generates `ptk_` token, stores hash; returns raw token |
| `CreateTeam(t, db, userID, role, name)` | Inserts team and adds userID as given role; returns team UUID |
| `AddMember(t, db, teamID, userID, role)` | Inserts a `team_memberships` row |
| `CreatePart(t, db, scope, mpn, teamID)` | Inserts a part; returns UUID |
| `GetTestDistributorID(t, db)` | Returns UUID of the `api_type='test'` distributor (migration 000002) |
| `EnableDistributor(t, db, teamID, distributorID)` | Inserts a `distributor_configs` row (enabled=true, preferred=true) |
| `CreateProject(t, db, teamID, userID, name)` | Inserts a project; returns UUID |
| `CreateRevision(t, db, projectID, userID, label)` | Inserts a BOM revision; returns UUID |
| `SetActiveBOM(t, db, projectID, revisionID)` | Sets `active_bom_revision_id` on a project |
| `CreateBOMLineItem(t, db, revisionID, refDesig, qty)` | Inserts a BOM line item; returns UUID |
| `ResolveBOMLineItem(t, db, itemID, partID)` | Sets `resolved_part_id` on a line item |
| `CreateInventoryEntry(t, db, teamID, partID)` | Inserts inventory entry with qty=0; returns UUID |
| `SetStockQty(t, db, entryID, qty)` | Sets `quantity_on_hand` directly |
| `CreateListing(t, db, partID, distributorID, sku)` | Inserts listing with `unit_price=0`; returns UUID |
| `CreateOrder(t, db, teamID, distributorID, userID)` | Inserts draft order; returns UUID |
| `CreateOrderLineItem(t, db, orderID, listingID, qty)` | Inserts line item with `unit_price_snapshot=0`; returns UUID |

### `request.go`

```go
// Do sends a JSON request and returns the decoded response body and status code.
// token is sent as "Authorization: Bearer <token>"; pass "" for unauthenticated.
func Do(t *testing.T, srv *httptest.Server, method, path string, body any, token string) (map[string]any, int)
```

---

## Test Organization

```
internal/
  testutil/               ← shared helpers (importable by all test packages)
    db.go
    seed.go
    request.go
  middleware/
    cache_test.go         ← pure unit tests; no DB required
  worker/
    queue_test.go         ← worker integration tests
  handler/
    main_test.go          ← TestMain: shared DB pool + httptest.Server
    auth_test.go
    teams_test.go
    members_test.go
    tokens_test.go
    sessions_test.go
    distributors_test.go
    parts_test.go
    storage_test.go
    inventory_test.go
    preferences_test.go
    projects_test.go
    revisions_test.go
    items_test.go
    stockcheck_test.go
    cart_test.go
    orders_test.go
    builds_test.go
    checkouts_test.go
```

All `*_test.go` files in `internal/handler/` use `package handler_test` (black-box testing via HTTP).

### `main_test.go`

A single `TestMain` creates the shared DB pool and HTTP test server once for the entire `handler_test` package:

```go
func TestMain(m *testing.M) {
    testDB = testutil.MustDB(nil)
    cfg := &config.Config{Auth: config.AuthConfig{SessionSecret: "test-secret-for-testing-only-32b!"}}
    reg := distributor.NewRegistry()
    reg.Register("test", testclient.New())
    q := worker.New(1, testDB, reg)
    srv := server.New(cfg, testDB, q)
    ts := httptest.NewServer(srv.Handler)
    testEnv = &testEnvType{Server: ts, DB: testDB, Queue: q}
    code := m.Run()
    ts.Close()
    os.Exit(code)
}
```

---

## Isolation

Tests are **serial within a package** and share one DB pool per binary. Each test registers a cleanup:

```go
func TestFoo(t *testing.T) {
    t.Cleanup(func() { testutil.Truncate(t, testDB) })
    // ...
}
```

**Why not transaction rollback?** Many handlers open and commit their own transactions (cart convert, inventory adjustments, part enrichment). Wrapping the test in an outer transaction conflicts with this. Truncation is simple and fast enough.

**Parallelism:** `t.Parallel()` is not used at the handler level — tests share one DB. Multiple test packages must run with `-p 1` to prevent concurrent startup-truncations from corrupting each other's data (see [Running Tests](#running-tests)).

---

## Authentication in Tests

Tests do **not** go through `/auth/login`. Instead, `testutil.CreateSession` and `testutil.CreateAPIToken` insert records directly into the DB, then pass the raw token in `Authorization: Bearer`:

```go
userID := testutil.CreateUser(t, testDB, "alice@example.com", "pw", "Alice")
token  := testutil.CreateSession(t, testDB, userID)

body, status := testutil.Do(t, testEnv.Server, "GET", "/api/v1/auth/me", nil, token)
// assert status == 200
```

---

## Running Tests

```bash
# Pure unit tests — no DB required
go test ./internal/middleware/...

# Full DB-backed suite
PARTRACK_TEST_DB_URL="postgres://postgres:password@host/dbname" \
    go test -p 1 -count=1 ./internal/worker/... ./internal/handler/...

# Race detector (run periodically)
PARTRACK_TEST_DB_URL=... go test -p 1 -race -count=1 ./internal/worker/... ./internal/handler/...

# Single package verbosely
PARTRACK_TEST_DB_URL=... go test -v ./internal/handler/...

# Single test
PARTRACK_TEST_DB_URL=... go test -run TestConvertCart ./internal/handler/...
```

The `-p 1` flag is **required** when running multiple DB-dependent packages together. Without it, the test binaries start concurrently and their `Truncate` calls at startup race with each other's mid-test writes.

---

## What Is Not Tested

| Area | Reason |
|---|---|
| OIDC login flow | Requires an external OIDC provider; defer until a mock OIDC server is added. |
| SSE streaming | Full HTTP streaming is awkward with `httptest`. Worker event routing is verified in `queue_test.go` by subscribing directly to the queue channel. |
| BOM import (`JobBOMImport`) | Stub handler; add tests when implemented. |
| Frontend | Separate Vite project; not in scope. |
| Concurrent race conditions | Run `go test -race ./...` periodically; not a separate test category. |

---

## Constraints and Gotchas

**`:exec` queries never return `ErrNoRows`.** When a DELETE or UPDATE should return 404 on no match, the query must be declared `:one RETURNING id` (or similar) so pgx returns `pgx.ErrNoRows` on zero rows. A `:exec` query that affects 0 rows returns nil — the handler has no way to distinguish "deleted" from "not found".

**Manual stock adjustments require `reason`.** The DB enforces `CONSTRAINT manual_adjustment_requires_reason`: any `stock_adjustments` row where all source FKs (`order_line_item_id`, `build_id`, `general_checkout_id`) are NULL must have a non-empty `reason`. Tests that create manual adjustments must supply one.

**`CASE WHEN` for nullable boolean expressions in sqlc.** `(nullable_col = other_col)` returns NULL when `nullable_col` is NULL, which pgx cannot scan into a plain `bool`. Use `CASE WHEN col = other THEN TRUE ELSE FALSE END` to guarantee a non-null boolean. `COALESCE` works in SQL but causes sqlc to emit `interface{}`.

**Seed helpers set required NOT NULL fields.** `CreateListing` inserts `unit_price = 0` and `CreateOrderLineItem` inserts `unit_price_snapshot = 0` because those columns are NOT NULL with no DB default. Omitting them causes constraint violations when the values are later copied by `ConvertCart`.

**Worker tests use zero-worker queues.** `worker.New(0, db, reg)` creates a queue with no goroutines. Tests call `q.process(job)` directly for fully synchronous, deterministic execution. Handler tests use a 1-worker queue and subscribe to the event channel to wait for job completion.
