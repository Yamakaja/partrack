## Work Plan: Distributor Keyword / MPN / Manufacturer Search

This plan describes how to add a **parts search by MPN / manufacturer / keyword** capability to
the distributor API clients, covering both the DigiKey implementation and the in-repo test
client. The feature uses DigiKey ProductSearch v4 `/products/v4/search/keyword` as documented in
`doc/distributor/distributor-digikey.md`.

---

## 1. Goals and Scope

- Expose a **distributor-agnostic search API** that supports:
  - free-text keyword search (e.g. "10k 1% 0603 resistor"),
  - MPN-based search (e.g. "TPS7A4701RGWT"),
  - manufacturer + MPN search (e.g. "TI TPS7A4701").
- Implement this API for:
  - **DigiKey** using ProductSearch v4 `POST /products/v4/search/keyword` (real HTTP client),
  - **testclient** with a deterministic in-memory mock useful for handler tests.
- Keep the feature focused on **discovery** (finding candidate parts); full enrichment continues
  to use `EnrichPart` (DigiKey `productdetails`) and `RefreshListing` (`pricing`).

Out of scope for this iteration:

- New HTTP handlers or frontend UI for search (can be layered on top of the new distributor API).
- Advanced faceted search (deep use of `FilterOptionsRequest`, `FilterOptions`).
- Account-specific MyPricing integration (keyword search only returns StandardPricing).

---

## 2. API Design in `internal/distributor`

### 2.1 New search types

Add search-specific types next to `EnrichmentResult` / `Listing` in
`internal/distributor/client.go`:

- `SearchQuery` — high-level query description:
  - `Keywords string` — required; free-text query (MPN, manufacturer+MPN, generic text).
  - `Limit int32` — optional; max results per page (1–50 for DigiKey). 0 → use implementation default.
  - `Offset int32` — optional; starting index for pagination (0-based).

- `SearchProduct` — minimal product summary returned by search:
  - `MPN string` — manufacturer part number (from DigiKey `ManufacturerPartNumber` or equivalent).
  - `Manufacturer string`
  - `Description string`
  - `Category string`
  - `PrimarySKU string` — canonical distributor-specific SKU (e.g. DigiKey part number of the best variation).
  - `Package string` — human-readable package / case when available.
  - `UnitPrice string` — optional price preview (decimal string, may be empty).
  - `StockAvailable *int32` — optional stock preview.

- `SearchResult` — wrapper for a search page:
  - `Products []SearchProduct` — products for the current page.
  - `TotalCount int32` — total number of matches (DigiKey `ProductsCount`).
  - `ExactMatchIndices []int` — indices into `Products` that correspond to DigiKey `ExactMatches`
    when known; callers can prioritise these (e.g. auto-select on single exact match).

These types deliberately avoid binding to DigiKey-specific details so that future distributors
can implement the same interface.

### 2.2 Extend the `Client` interface

In `internal/distributor/client.go`, extend `Client` with a new method:

```go
SearchParts(ctx context.Context, q SearchQuery, credentials json.RawMessage) (*SearchResult, error)
```

Notes:

- Implementations **must not** mutate `SearchQuery`.
- Returning `(*SearchResult)(nil), nil` indicates "no results" (e.g. 0 matches or 404-equivalent).
- Implementations should cap `q.Limit` to their supported range (e.g. 1–50 for DigiKey); 0 means
  "use sensible default" (e.g. 20).

### 2.3 Registry and call sites

No immediate changes in `internal/distributor/registry.go` are required beyond fixing compile
errors once the interface gains `SearchParts`.

Future work (separate change) will:

- Add an HTTP handler endpoint (e.g. `GET /api/v1/teams/{teamId}/distributors/{id}/search`)
  that constructs a `SearchQuery` and calls `Client.SearchParts`.
- Wire the handler into the frontend parts search UI.

---

## 3. Test Client Implementation (`internal/distributor/testclient`)

### 3.1 Behaviour

Implement `SearchParts` on `testclient.Client` with deterministic behaviour suitable for tests:

- Accept any `SearchQuery`. Normalise `q.Keywords` to upper-case for matching.
- If `q.Keywords` has a prefix `"LM317"`:
  - Return a `SearchResult` containing one `SearchProduct` that mirrors the existing LM317
    enrichment data:
    - `MPN = "LM317TEST"` (or similar),
    - `Manufacturer = "Texas Instruments"`,
    - `PrimarySKU = testSKU`,
    - other fields matching the EnrichPart mock.
- If `q.Keywords` contains generic regulator terms (e.g. "REGULATOR") but not `LM317`:
  - Return 2–3 mock `SearchProduct` entries to exercise pagination and multiple results.
- For all other queries:
  - Return `SearchResult{Products: nil, TotalCount: 0}` (no matches).

This gives handler tests a stable, fast search backend without external dependencies.

### 3.2 Tests

Add tests under `internal/distributor/testclient`:

- Verify `SearchParts` returns LM317 product for `Keywords = "LM317"` and that the SKU matches
  the existing `EnrichPart` and `RefreshListing` behaviour.
- Verify non-LM317 queries either return 0 results or the expected mock set.
- Verify `Limit` / `Offset` behaviour (even if no-op now) so that later pagination logic can rely
  on testclient semantics.

---

## 4. DigiKey Implementation (`internal/distributor/digikey`)

### 4.1 Request/response models for keyword search

Add lightweight Go types in `internal/distributor/digikey/client.go` (or a new
`keyword.go` within the package) to model the subset of the swagger we need:

- `keywordRequest` mapping to `KeywordRequest`:
  - `Keywords string` `json:"Keywords"`
  - `Limit int32` `json:"Limit,omitempty"`
  - `Offset int32` `json:"Offset,omitempty"`
  - `FilterOptionsRequest *filterOptionsRequest` `json:"FilterOptionsRequest,omitempty"` (for future use).
  - `SortOptions *sortOptions` `json:"SortOptions,omitempty"` (for future use).

- `keywordResponse` mapping to `KeywordResponse`:
  - `Products []product` `json:"Products"`
  - `ProductsCount int32` `json:"ProductsCount"`
  - `ExactMatches []product` `json:"ExactMatches"`
  - `FilterOptions *filterOptions` `json:"FilterOptions,omitempty"`
  - `SearchLocaleUsed *isoSearchLocale` `json:"SearchLocaleUsed,omitempty"`
  - `AppliedParametricFiltersDto []parameterValue` `json:"AppliedParametricFiltersDto,omitempty"`

Where possible, reuse existing `product` / `parameterValue` structs from the `productdetails`
implementation to avoid duplication.

### 4.2 Shared POST helper

The existing client has `doGet` for GET requests with DigiKey-specific error handling
(401 retry with token refresh, 429 with `Retry-After`, etc.). For keyword search we need a
similar `doPostJSON` helper:

- `doPostJSON(ctx, rawURL string, body any, creds Credentials, out any) (found bool, err error)`
  - Serialise `body` as JSON.
  - Set `Content-Type: application/json`.
  - Apply `setHeaders` (auth + locale), obtain token via `getToken`.
  - Implement the same status-code handling as `doGet` for 200/400/401/404/429/500/503.
  - Return `found=false, nil` on 404-equivalent (if applicable); otherwise apply the same
    conventions as `doGet`.

This keeps keyword search consistent with existing enrichment/listing refresh behaviour.

### 4.3 Implement `SearchParts` for DigiKey

Implement `SearchParts` on `digikey.Client`:

1. **Decode credentials** as in `EnrichPart` / `RefreshListing`.
2. **Normalise and cap query parameters**:
   - Trim whitespace on `q.Keywords`; if empty, return `nil, nil` (no-op / invalid query).
   - If `q.Limit <= 0`, set to default (e.g. 20).
   - Clamp `q.Limit` into `[1, 50]` per DigiKey swagger.
   - Ensure `q.Offset >= 0`; negative values treated as 0.
3. **Build and send request**:
   - `endpoint := baseURL(creds) + "/products/v4/search/keyword"`.
   - `reqBody := keywordRequest{Keywords: q.Keywords, Limit: q.Limit, Offset: q.Offset}`.
   - Call `doPostJSON(ctx, endpoint, &reqBody, creds, &resp)`.
4. **Map `keywordResponse` to `SearchResult`**:
   - Iterate `resp.Products` and map to `SearchProduct`:
     - `MPN` from `ManufacturerPartNumber` (or equivalent) on `product`.
     - `Manufacturer` from `Manufacturer.Name`.
     - `Description` from `Description.ProductDescription` (fall back to `DetailedDescription`).
     - `Category` from `Category.Name`.
     - `PrimarySKU` from a best-effort selection of `ProductVariations[]` (e.g. first
       non-marketplace variation).
     - `Package` from `Parameters` using the same logic as `EnrichPart`
       ("Package / Case" or "Supplier Device Package").
     - Optional `UnitPrice` and `StockAvailable` from the chosen variation using
       `StandardPricing` only (since `MyPricing` is not present in keyword search).
   - Set `TotalCount = resp.ProductsCount`.
   - Derive `ExactMatchIndices` by matching `resp.ExactMatches` back to entries in
     `resp.Products` (e.g. by DigiKey part number + MPN); include indices for any products
     that exist in both arrays.
5. **Return `(*SearchResult)(nil), nil` when there are no products** (empty result set).

### 4.4 Error handling and retries

Ensure `SearchParts` inherits the same resilience as existing methods:

- Single automatic retry on 401 (token expiry) after invalidating the cached token.
- Single automatic retry on 429 honouring `Retry-After` when present.
- `400` treated as permanent error (invalid query) and surfaced to caller.
- `404` treated as "no results" where applicable.

---

## 5. Integration Surfaces (Follow-up Work)

Although not part of this implementation, keep the following integration points in mind when
designing the API shapes:

1. **HTTP handler for distributor search**
   - Likely under `internal/handler/distributors.go` or `internal/handler/parts.go`.
   - Accepts query params like `keywords`, `limit`, `offset`, and a `distributor_id` or
     `distributor_name`.
   - Resolves the distributor client via `registry`, builds a `SearchQuery`, and returns a JSON
     representation of `SearchResult` to the frontend.

2. **Frontend search UI**
   - A new API wrapper in `web/src/lib/api/distributors.ts` or `parts.ts` to call the handler.
   - A Svelte search component that presents results, emphasising exact matches and allowing the
     user to pick a product to enrich / add to BOM.

These integration steps can piggyback on the types introduced here without requiring any further
changes to the distributor interfaces.

---

## 6. Testing and Validation Strategy

### 6.1 Unit and integration tests

- **Distributor layer**:
  - Add tests for `testclient.SearchParts` covering LM317 and non-matching queries.
  - Add tests for `digikey.SearchParts` using a stubbed `http.Client` (e.g. via injecting a
    custom client into `digikey.Client` or by adding a `NewWithClient(*http.Client)` constructor).
  - Verify mapping from `keywordResponse` → `SearchResult`, including `ExactMatchIndices`.

- **Handler layer (when added)**:
  - Extend `internal/handler` tests to exercise the search endpoint end-to-end using the
    testclient implementation.

### 6.2 Manual verification (DigiKey sandbox)

Using a sandbox DigiKey account:

- Configure DigiKey credentials for a test team (with `sandbox: true`).
- Call the future search handler (or a temporary dev-only endpoint / script) with:
  - Pure MPN queries (expect `ExactMatches` populated).
  - Manufacturer + MPN queries.
  - Generic keyword queries to verify reasonable result ordering.

Cross-check a sample of results in the DigiKey web UI to ensure the mapping is sane.

---

## 7. Rollout

1. Implement and test the **testclient** changes first so handler/frontend work can proceed
   without dependence on DigiKey.
2. Implement `SearchParts` in the **DigiKey client**, guarded only by the presence of valid
   DigiKey credentials (no extra feature flags expected).
3. Once both implementations are stable, add the HTTP handler + frontend integration in a
   follow-up change.
4. Monitor logs for DigiKey search errors (400/401/429) after rollout and adjust retry/backoff
   behaviour if necessary.
