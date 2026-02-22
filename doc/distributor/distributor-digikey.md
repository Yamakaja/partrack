# DigiKey API Integration Plan

## Available APIs

| Swagger file | API name | Base path | Relevance |
|---|---|---|---|
| `ProductSearch.json` | ProductSearch v4 | `/products/v4` | **Core** — enrichment + listing refresh |
| `Ordering.json` | Ordering v3 | `/Ordering/v3` | **Future** — programmatic order placement |
| `OrderStatus.json` | OrderStatus v4 | `/orderstatus/v4` | **Future** — order tracking after placement |
| `Barcoding.json` | Barcoding v3 | `/Barcoding/v3` | **Future** — scan incoming shipment barcodes |
| `Quote-v4.json` | Quote v4 | `/quoting/v4` | **Future** — push cart to a DigiKey quote for review |
| `MyLists.json` | MyLists v1 | `/mylists/v1` | **Future** — push BOM/cart to a DigiKey saved list |
| `PackingList.json` | PackingList v1 | _(not in swagger)_ | Not needed — Barcoding covers receipt flow |
| `OrderManagement.json` | B2BApi OrderManagement v1 | _(not in swagger)_ | Not applicable — B2B schedule-date mutations only |

Hosts: production = `api.digikey.com`, sandbox = `sandbox-api.digikey.com`.

---

## Authentication

DigiKey uses **OAuth2 client credentials flow** (2-legged) for server-to-server integrations — no user login, no refresh tokens.

Token endpoint: `POST https://api.digikey.com/v1/oauth2/token`
- Body (form-urlencoded): `grant_type=client_credentials&client_id=...&client_secret=...`
- Response: `{"access_token": "...", "expires_in": 1800, "token_type": "Bearer"}`

Every API request requires two auth headers:
- `Authorization: Bearer {access_token}`
- `X-DIGIKEY-Client-Id: {client_id}`

Access tokens expire in ~30 minutes. The client implementation must cache the token in memory and proactively re-fetch it before it expires (e.g. refresh when less than 60 seconds remain). The token must not be stored in the DB — only `client_id` and `client_secret` are persisted.

---

## Credentials JSON (`api_credentials`)

The JSONB stored in `distributor_configs.api_credentials` for a DigiKey config:

```json
{
  "client_id":        "your-client-id",
  "client_secret":    "your-client-secret",
  "locale_site":      "US",
  "locale_currency":  "USD",
  "locale_language":  "en"
}
```

`locale_site`, `locale_currency`, and `locale_language` are optional and default to `US`, `USD`, `en`. They affect which regional pricing and product restrictions are returned. Valid values are documented in the ProductSearch swagger `X-DIGIKEY-Locale-*` header descriptions.

For teams using a DigiKey account-level pricing agreement, an optional `account_id` field can be added — this maps to `X-DIGIKEY-Account-Id` and enables `MyPricing` to be returned in ProductSearch responses. It is **required** for Quote v4 operations (quote creation fails without it).

---

## Locale Headers

Every ProductSearch request should include:

| Header | Value source | Default |
|---|---|---|
| `X-DIGIKEY-Client-Id` | `credentials.client_id` | required |
| `X-DIGIKEY-Locale-Site` | `credentials.locale_site` | `US` |
| `X-DIGIKEY-Locale-Currency` | `credentials.locale_currency` | `USD` |
| `X-DIGIKEY-Locale-Language` | `credentials.locale_language` | `en` |
| `X-DIGIKEY-Account-Id` | `credentials.account_id` | omit if absent |

---

## EnrichPart

### Endpoint

```
GET https://api.digikey.com/products/v4/search/{productNumber}/productdetails
```

`productNumber` = the part's MPN (or a DigiKey part number if available). The API performs a match — it is not a fuzzy search, but it does tolerate minor variations. A manufacturer part number that matches multiple manufacturers will return data for one of them; use the `manufacturerId` query parameter to disambiguate if needed (see "Ambiguous MPNs" below).

### Response mapping to `EnrichmentResult`

The response is `ProductDetails`:
- `.Product` — a `Product` object with all enrichment data
- `.AccountIdUsed`, `.CustomerIdUsed`, `.SearchLocaleUsed` — metadata, ignore

`Product` fields mapped to `EnrichmentResult`:

| DigiKey field | partrack field | Notes |
|---|---|---|
| `Manufacturer.Name` | `Manufacturer` | String |
| `Description.ProductDescription` | `Description` | Use `DetailedDescription` as fallback if empty |
| `Category.Name` | `Category` | Leaf category name; parent ignored |
| `DatasheetUrl` | `DatasheetURL` | Direct URL string |
| `Parameters[]` | `Parameters` | See parameter extraction below |
| `ProductVariations[]` | `Listings` | One `Listing` per variation; see below |
| `ProductStatus.Status` | — | Used for lifecycle status mapping; see below |

**The physical component package** (e.g. `SOT-23`, `0402`) is not a top-level field — it appears as a `Parameter` with `ParameterText = "Package / Case"` or `"Supplier Device Package"`. The `package` field on the partrack `Part` must be extracted from parameters during enrichment.

### Parameter extraction

Each entry in `Product.Parameters[]` has:
- `ParameterText` — parameter name (e.g. `"Capacitance"`, `"Operating Temperature"`)
- `ValueText` — combined value + unit string (e.g. `"100 nF"`, `"-55°C ~ 125°C"`, `"1.5 A"`)
- `ParameterType` — `String | Integer | Double | UnitOfMeasure | CoupledUnitOfMeasure | RangeUnitOfMeasure`

partrack stores `name`, `value`, `unit` separately. For `UnitOfMeasure` parameters, the `ValueText` is typically `"{number} {unit}"`. Strategy:

- For `UnitOfMeasure`: split on the last space; if the right token looks like a unit (non-numeric), store left as `value`, right as `unit`.
- For all other types (String, Integer, Double, range types): store the full `ValueText` as `value`, leave `unit` empty.
- Skip parameters where `ValueText` is empty or `"-"`.

This avoids building a unit parser. The stored values will be human-readable even for ranges and compound units.

### Lifecycle status mapping

`Product.ProductStatus.Status` → `parts.lifecycle_status`:

| DigiKey Status string | partrack status |
|---|---|
| `"Active"` | `active` |
| `"Not For New Designs"` | `nrnd` |
| `"Discontinued at Digi-Key"` | `nrnd` |
| `"End of Life"` | `nrnd` |
| `"Obsolete"` | `obsolete` |
| `"Last Time Buy"` | `nrnd` |
| anything else / absent | leave existing status unchanged |

Do not overwrite an existing status of `deprecated` or `no_longer_available` — only update when the incoming status is more specific.

### Listing extraction from ProductVariations

Each entry in `Product.ProductVariations[]` maps to one `Listing`:

| DigiKey field | partrack `Listing` field | Notes |
|---|---|---|
| `DigiKeyProductNumber` | `SKU` | Use as the canonical SKU |
| `PackageType.Name` | `PackagingType` | e.g. `"Tape & Reel (TR)"`, `"Cut Tape (CT)"`, `"Digi-Reel®"`, `"Tube"`, `"Tray"`, `"Bulk"` |
| `QuantityAvailableforPackageType` | `StockAvailable` | Cast `int32`; can be 0 |
| `MinimumOrderQuantity` | `Moq` | |
| `StandardPricing[0].UnitPrice` | `UnitPrice` | Price at MOQ quantity; format as decimal string e.g. `"0.5000"`. If `MyPricing` is non-empty, prefer that instead. |
| — | `FixedOrderCost` | Always `"0.0000"` — DigiKey does not expose a separate fixed order fee |

Skip variations where `MarketPlace = true` initially — Marketplace items ship from third parties and have different availability characteristics. This is a deferred decision; for now exclude them to avoid data quality issues.

If `StandardPricing` is empty, set `UnitPrice = ""` (unknown). The DB column is nullable.

### Ambiguous MPNs

Some MPNs (e.g. `"CR2032"`) match multiple manufacturers. In this case, `productdetails` returns one result, potentially the wrong manufacturer. Two mitigations:

1. **After enrichment**, if `Product.Manufacturer.Name` does not match the part's existing `manufacturer` field (when set), log a warning. Do not overwrite the manufacturer field if it was already set.
2. **If the part has a `manufacturer` set**, use `/search/keyword` with `{keywords: "{manufacturer} {mpn}"}` and `FilterOptionsRequest` to constrain on manufacturer — or call `productdetails` with the `manufacturerId` query parameter once the manufacturer's DigiKey ID is known. This is a deferred optimization; for the initial implementation, accept the ambiguity.

### Not found

If the API returns 404 or the response's `Product` is null, return `nil, nil` from `EnrichPart` — this signals "not found" to the worker, which will publish a `not_found` status event and continue to the next distributor.

---

## RefreshListing

### Endpoint

```
GET https://api.digikey.com/products/v4/search/{productNumber}/pricing
```

`productNumber` = the DigiKey part number stored as the listing's SKU (set during `EnrichPart`). This endpoint returns pricing and stock for all packaging variations of the base product. It is lighter than `productdetails` and sufficient for a stock + price refresh.

Query parameters:
- `limit=10` (max 10; default 5)
- `offset=0`
- `excludeMarketplace=true` (consistent with enrichment)

### Response

`ProductPricingResponse`:
- `.ProductPricings[]` — each is a `ProductPricing` with:
  - `.ManufacturerProductNumber`
  - `.ProductVariations[]` — each has:
    - `DigiKeyProductNumber` — the SKU
    - `QuantityAvailableforPackageType` — current stock
    - `MinimumOrderQuantity`
    - `StandardPricing[]` / `MyPricing[]` — price breaks
    - `PackageType.Name`

### Matching

Since `pricing` returns all variations of the product family, filter to find the variation where `DigiKeyProductNumber == sku` (the stored SKU). If no matching variation is found, return `nil, nil` ("unavailable").

Map matching variation to `Listing` using the same field mapping as `EnrichPart`.

---

## Future Capabilities

### Order Placement (Ordering v3 — `POST /Ordering/v3/Orders`)

Enables partrack to submit an order directly to DigiKey from the UI rather than exporting a CSV.

Request body (`OrderRequest`) requires:
- `PurchaseOrderNumber` — the partrack `orders.reference_number`
- `Currency` — from credentials locale
- `ShippingContact` — shipping address; must be configured per team (new field in `distributor_configs.api_credentials` or a separate team preference)
- `LineItems[]` — each with `DigiKeyPartNumber` (the listing SKU), `RequestedQuantity`, `UnitPrice` (from the snapshot)
- `ShipControl` — `"Immediate"` or `"Single"` (configurable)
- `ShipMethod` — configurable per team

Response: `OrderResponse` with `SalesOrderId` (integer). Store this in a new `external_order_id` column on `orders` for tracking.

Key constraint: `UnitPrice` is required on each line item as an expected price check. If DigiKey's actual price differs, the order may be held or rejected.

### Order Status Tracking (OrderStatus v4 — `GET /orderstatus/v4/salesorder/{salesOrderId}`)

Poll an order using the `SalesOrderId` returned at placement time.

`SalesOrder` response includes:
- `Status` — order-level status string
- `LineItems[]` with:
  - `DigiKeyProductNumber` — matches our listing SKU
  - `QuantityOrdered`, `QuantityShipped`, `QuantityBackOrder`, `QuantityReserved`
  - `UnitPrice`, `TotalPrice`
  - `ItemShipments[]` — tracking numbers and quantities per shipment

This enables partrack to show "shipped / backordered / pending" per line item and auto-populate `received_quantity` when `QuantityShipped` increases.

### Barcode Scanning for Receipt (Barcoding v3)

Two relevant endpoints:

**Pack list 2D barcode** — `GET /Barcoding/v3/PackList2DBarcodes/{barcode}`:
- Input: the 2D barcode string from the top-right corner of the DigiKey packing slip (URL-encoded)
- Response: `PackListBarcodeResponse` with `SalesorderId`, `InvoiceId`, `PackListDetails[]` (DigiKey part number + quantity per line)
- Use case: scan the packing slip to auto-mark an entire order as received

**Product label 2D barcode** — `GET /Barcoding/v3/Product2DBarcodes/{barcode}`:
- Input: the 2D barcode on an individual bag/reel label
- Response: `Product2DBarcodeResponse` with `DigiKeyPartNumber`, `ManufacturerPartNumber`, `Quantity`, `SalesorderId`, `InvoiceId`, `LotCode`, `DateCode`
- Use case: scan individual parts during receiving to record exact quantities

The barcode strings from packing slip and product labels contain special characters that must be URL-encoded before passing as path parameters.

### Create Quote (Quote v4)

Pushes a partrack order to DigiKey as a **quote** rather than placing it immediately. The user then reviews and checks out at digikey.com. This is the lower-commitment alternative to direct order placement — no shipping address is required from partrack.

**Flow — two API calls:**

**Step 1: Create the quote**

```
POST https://api.digikey.com/quoting/v4/quotes
```

Request body (`CreateQuoteRequest`):
- `QuoteName` — required, max 40 characters. Use the partrack order reference number or a generated name like `"partrack-{orderId}"`.

Required headers beyond the standard auth headers:
- `X-DIGIKEY-Account-Id` — **required** for quote creation (unlike ProductSearch where it is optional). Must be present in credentials.

Response (`QuoteResponse` wrapped in `QuoteInformation`):
- `.Quote.QuoteId` — integer, the DigiKey quote ID. Store in `orders.external_order_id`.
- `.Quote.QuoteName`
- `.Quote.ExpirationStatus` — `"Active"`, `"Error"`, or `"Expired"`
- `.Quote.Currency` — the currency DigiKey applied (may differ from requested if invalid)

**Step 2: Add products to the quote**

```
POST https://api.digikey.com/quoting/v4/quotes/{quoteId}/details
```

Request body — array of `AddProductToQuote`:
- `ProductNumber` — the listing SKU (DigiKey part number). MPN is also accepted but SKU is unambiguous.
- `CustomerReference` — optional; use the partrack order line item ID or the part's MPN for traceability.
- `Quantities` — array of `int32`. Pass a single element: the `quantity_ordered` from the cart/order line item. Multiple quantities can be passed to get tiered pricing back on the quote, but one is sufficient.

Response (`AddProductsToQuoteResponse`):
- `QuoteId` — echoed back
- `Errors[]` — per-product errors (e.g. unrecognised product number). A non-empty errors array means some products were not added; log and surface to the user.
- `Comments[]` — non-fatal warnings (e.g. substitute recommended)

**After both calls succeed**, the quote is visible in the user's DigiKey account at `digikey.com/en/quotes`. The `QuoteId` can be provided to the user as a direct reference.

**Retrieving quote details** (optional, for confirming what was accepted):

```
GET https://api.digikey.com/quoting/v4/quotes/{quoteId}/details
```

Returns `ProductsFromQuoteResponse` with `QuoteProducts[]`. Each `QuoteProduct` contains:
- `DigikeyProductNumber`, `ManufacturerProductNumber`, `Description`
- `QuantityAvailable` — current stock at time of quote
- `MinimumOrderQuantity`
- `Quantities[]` (`QuoteProductQuantity`) — one entry per requested quantity, each with `Quantity`, `UnitPrice`, `ExtendedPrice`
- `IsObsolete`, `IsDiscontinued`, `IsMfgQuoteRequired` — flags that indicate parts needing attention
- `ExpirationDate` — when this line's pricing expires

This read-back is useful for syncing quoted unit prices back into the partrack order's `unit_price_snapshot` fields, and for surfacing any lines that couldn't be quoted (`IsMfgQuoteRequired = true`).

**Account ID requirement:** `account_id` must be present in `api_credentials` for quote operations to succeed. The field should be made required in the DigiKey credentials validation logic if the team enables the Quote capability.

---

### Upload to MyList (MyLists v1)

Pushes a partrack cart or BOM revision to a **DigiKey MyList** — a saved, reusable parts list in the user's DigiKey account. Unlike a quote, a MyList has no expiry and no pricing locked in; it is closer to a cart template. It supports reference designators and alternate parts, making it well-suited for exporting a full BOM revision (not just a shortfall cart).

**Flow — two API calls:**

**Step 1: Create the list**

```
POST https://api.digikey.com/mylists/v1/lists
```

Request body (`CreateListRequest`):
- `ListName` — required. Use the partrack project + revision label (e.g. `"My Project — v1.0"`) for cart exports, or a freeform name.
- `Source` — set to `"b2b"` to identify programmatically created lists.
- `Tags` — optional; e.g. `["partrack", "{teamId}"]` for searchability.
- `ListSettings.Visibility` — `"Private"` (default; do not expose team's BOM to other account users).
- `ListSettings.PackagePreference` — `"EnteredPartNumber"` to preserve the exact DigiKey SKU that partrack selected.

Response: `ListData` with:
- `Id` — string UUID, the list identifier for subsequent calls. Store in `orders.external_order_id` (for cart-derived lists) or a new field if exporting a revision.
- `ListName`, `DateCreated`

**Step 2: Add parts to the list**

```
POST https://api.digikey.com/mylists/v1/lists/{listId}/parts
```

Request body — array of `RequestedPart`:
- `RequestedPartNumber` — the listing SKU (DigiKey part number). MPN is also accepted.
- `CustomerReference` — optional; use the partrack part's MPN or internal reference for traceability.
- `ReferenceDesignator` — the BOM line item's `reference_designators` string (e.g. `"R1,R2,R5"`). Only meaningful for BOM revision exports; leave empty for cart-derived lists.
- `AlternateParts[]` — array of part number strings. Populate from the BOM line item's `bom_line_item_substitutes` (substitute DigiKey SKUs). This is the key advantage of MyList over Quote for BOM exports: substitutes are preserved in the list.
- `Quantities[]` — array of `RequestedQuantity`:
  - `SelectedPackType` — the `PackageType.Name` from the listing (e.g. `"TR"`, `"CT"`)
  - `Quantity` — `quantity_ordered` from the cart line, or `bom_line_items.quantity` for BOM exports

The endpoint accepts a batch of parts in a single call. Send all line items together rather than one call per part to minimise API round-trips.

**Retrieving list contents** (optional):

```
GET https://api.digikey.com/mylists/v1/lists/{listId}/parts
```

Returns `PartsByListIdResponse` with `ListParts[]`. Each `ListPart` contains resolved `DigiKeyPartNumber`, `ManufacturerPartNumber`, `QuantityAvailable`, `Quantities[]` (with current pricing), `Substitutes[]`, and `AlternateParts[]`. This can be used to confirm all parts were accepted and to read back current pricing.

**Two export modes:**

| Mode | Source | `ReferenceDesignator` | `AlternateParts` | Use case |
|---|---|---|---|---|
| Cart export | `cart_items` + `order_line_items` | empty | empty | "Send my shortfall order to DigiKey for review" |
| BOM revision export | `bom_line_items` + `bom_line_item_substitutes` | from `reference_designators` | substitute SKUs | "Save this full BOM in DigiKey for repeat ordering" |

For BOM revision export, use the BOM line item `quantity × build_quantity` as the `RequestedQuantity`. For cart export, use `cart_items.quantity`.

---

## Rate Limits and Error Handling

DigiKey imposes rate limits per client application. The API returns `429 Too Many Requests` when exceeded. The client implementation must:

1. Honour `Retry-After` response header when present on 429 responses.
2. Implement exponential backoff with jitter for 429 and 503 responses.
3. Treat 401 as a token expiry signal: re-authenticate and retry once before failing.
4. Treat 404 as "not found" — return `nil, nil` to the caller.
5. Treat 400 as a permanent error (bad MPN or SKU) — return an error without retrying.

The worker's `publishProgress` and `publishError` helpers handle surfacing these errors as SSE events. A rate-limit retry should be transparent to the SSE stream (no event published until the eventual success or permanent failure).

---

## Implementation Notes

### Token caching

The DigiKey client struct holds a mutex-protected `cachedToken` and `tokenExpiry`. On each API call, check if `time.Now().Add(60*time.Second).Before(tokenExpiry)`. If not, re-authenticate. This is safe for the single-process deployment constraint.

### `api_type` value

The `distributors.api_type` column value for DigiKey should be `"digikey"`. The registry key used in `reg.Register("digikey", digikey.New())` must match.

### Sandbox vs production

A `sandbox` boolean field in credentials selects the host:
- `sandbox: true` → `sandbox-api.digikey.com`
- `sandbox: false` (default) → `api.digikey.com`

The sandbox has a separate client ID and secret. DigiKey's sandbox returns realistic but synthetic data and does not place real orders.

### Decimal price formatting

DigiKey returns prices as `float64`. Convert to a 4-decimal-place string for storage: `fmt.Sprintf("%.4f", price)`. This matches the `NUMERIC(14,4)` column precision.

### Physical package extraction

After processing all parameters, find the one with `ParameterText == "Package / Case"` and store its `ValueText` as `EnrichmentResult.Package`. Fall back to `"Supplier Device Package"` if `"Package / Case"` is absent. If neither is present, leave `Package` empty.
