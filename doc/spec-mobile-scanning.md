# Spec: Mobile Scanning — Phase 1 (Order Receiving)

## Overview

Distributor bags (DigiKey, Mouser) carry ISO 15434 / IPC-2D 2D data matrix barcodes encoding MPN, SKU, quantity, and lot. This feature lets a user pair their phone as a dedicated barcode scanner via a QR code shown on the desktop. Scanned bags are matched to order line items; the user confirms quantities on the phone; the desktop updates in real time via SSE.

**Scope of Phase 1:** Order receiving only.

---

## Design Decisions (Settled)

| Question | Decision |
|---|---|
| Context model | Desktop-driven — the desktop page and order set the scanning context |
| Phone role | Phone confirms (shows result + editable qty, taps Confirm/Skip before applying) |
| Line item match priority | Distributor SKU exact match first; MPN fallback |
| Quantity editability | Editable on phone — barcode qty pre-fills the field, user can change |
| Concurrent sessions | One active session per user |
| Token lifetime | "Taken" after phone connects — a second device opening the same URL gets a 409 |
| Session replacement | Warn before replacing an existing active session |
| Phone auth | `/scan` is a public (shellless, no-auth-redirect) route; phone authenticates only via Bearer scan token |
| QR modal on connect | Modal **auto-closes** when phone connects; a persistent status badge appears in the global AppShell topbar |
| Scanner badge | Lives in the global AppShell header (visible across all pages); opens a small popover with a Disconnect button on click |
| No-context phone state | Phone shows "Ready — open an order on the desktop to start scanning"; camera off |
| No-match behavior | "Not in this order" error; phone stays ready to scan next bag |
| Over-receive on phone | Warning banner shown, explicit confirm required |
| Already-fully-received line | Treated as over-receive (same warning flow) |
| Scan button placement | Always visible in OrderDetail topbar (not gated on receive mode) |
| Phone-connected signal | Extend SSE with `TargetUserID` routing; `scan_session_changed` delivered only to the session owner |
| Barcode formats | DigiKey and Mouser IPC-2D only; fail gracefully on others |
| Expiry UX on phone | "Session expired — scan the QR code again" screen |
| Clean disconnect | Immediate: DELETE session → phone's next poll returns 410 Gone |

---

## User Flows

### Desktop: Opening a Scan Session

1. User navigates to any order detail page.
2. A **"Scan"** button is always visible in the topbar `actions` area.
3. User clicks Scan → `ScanModal` opens.
4. Modal calls `POST /api/v1/teams/{teamId}/scan-sessions` with `{order_id}`:
   - If a previous session exists for this user → **backend returns 409**. Modal shows warning: *"You already have an active scanner session. Starting a new one will disconnect the previous phone."* User clicks **Replace** (which DELETEs old session then retries) or **Cancel**.
   - On success: returns `{id, token, expires_at}`.
5. Modal generates QR URL: `https://{host}/scan?token={token}` and renders the QR code SVG.
6. Modal status: **"Waiting for phone…"** — an animated waiting indicator.
7. Modal subscribes to `scan_session_changed` SSE (filtered by `session_id`).
8. When phone connects → SSE fires → **modal closes automatically**.
9. Global AppShell header shows the **scanner badge** (green dot + "Scanner connected").

### Desktop: While Scanner is Connected

- The scanner badge persists in the global AppShell header across all page navigation.
- Clicking the badge opens a small popover:
  ```
  ┌──────────────────────────────┐
  │ 🟢 Scanner connected         │
  │    Receiving Order #PO-1234  │
  │                              │
  │         [Disconnect]         │
  └──────────────────────────────┘
  ```
- Clicking **Disconnect** calls `DELETE /api/v1/teams/{teamId}/scan-sessions/{id}` and clears the badge.

### Desktop: Line Item Updates

No special handling needed. When the phone confirms a scan, the backend publishes the existing `order_changed` SSE event. The frontend's existing cache invalidation (`['order', orderId]`) triggers a re-fetch and the line items update automatically.

---

### Phone: Connecting

1. User scans QR code → browser opens `/scan?token=scan_XXXXX`.
2. Page reads token from query param; stores in memory (not `localStorage`).
3. Page calls `GET /api/v1/scan/session` with `Authorization: Bearer {token}`.
4. Backend marks `phone_connected = true`, publishes `scan_session_changed` (TargetUserID = session owner).
5. Token is now "taken" — a second device opening the same URL gets `409 Conflict`.
6. Phone shows context banner: **"Receiving Order #PO-1234"** (or "Ready — open an order on the desktop to start scanning" if context not set yet, camera off).

### Phone: Scanning a Bag

1. Phone polls `GET /api/v1/scan/session` every 2s.
2. When context is `receive_order`, camera activates (`BrowserMultiFormatReader` from `@zxing/browser`).
3. On barcode detected: camera pauses. Phone calls `POST /api/v1/scan/scans` with `{barcode: "<raw>"}`.
4. Backend parses and matches; returns one of three statuses:
   - **`matched`** → phone shows confirmation panel (see below)
   - **`no_match`** → phone shows *"Not in this order"* with a **Scan Next** button; camera resumes
   - **`parse_error`** → phone shows *"Unrecognized barcode format"*; camera resumes
5. **Confirmation panel:**
   ```
   ┌─────────────────────────────┐
   │ BC847 NPN Transistor        │
   │ MPN: BC847 · SKU: 296-1234  │
   │                             │
   │ ⚠️ Over-receive warning      │  ← only if barcode qty > remaining
   │ Bag has 100, only 50 left   │
   │                             │
   │ Quantity: [___100___]  pcs  │  ← editable input, pre-filled
   │                             │
   │ Line item 3 of 8            │
   │ Ordered: 50 · Received: 0   │
   │                             │
   │ [Confirm]  [Skip]           │
   └─────────────────────────────┘
   ```
6. User edits qty if needed, taps **Confirm** → `POST /api/v1/scan/confirm` with `{qty}`.
7. Backend applies receive, publishes `order_changed` SSE → desktop updates.
8. Phone shows brief green flash → camera resumes for next bag.
9. **Skip** → `POST /api/v1/scan/skip` → camera resumes immediately.

### Phone: Session Expiry / Disconnection

On any poll or action returning `401` or `410 Gone`:
- Camera stops.
- Full-screen message: **"Session expired"** + *"To continue scanning, ask the desktop user to share a new QR code."*

---

## Data Model

### New Table: `scan_sessions` (Migration 000029)

```sql
CREATE TABLE scan_sessions (
  id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  token_hash       TEXT        NOT NULL UNIQUE,
  user_id          UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  team_id          UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  phone_connected  BOOLEAN     NOT NULL DEFAULT FALSE,
  context_type     TEXT,                    -- NULL | 'receive_order'
  context_order_id UUID,                    -- references orders(id)
  context_order_ref TEXT,                   -- human-readable order reference for phone display
  pending_scan     JSONB,                   -- parsed result waiting for phone confirmation
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at       TIMESTAMPTZ NOT NULL     -- 2 hours from creation
);
```

**`pending_scan` JSONB shape:**
```json
{
  "line_item_id": "uuid",
  "part_name": "BC847 NPN Transistor",
  "mpn": "BC847",
  "sku": "296-1234-1-ND",
  "barcode_qty": 100,
  "quantity_ordered": 50,
  "already_received": 0,
  "over_receive": true
}
```

Down migration: `DROP TABLE scan_sessions;`

---

## API Specification

### Desktop Endpoints (Require `purchaser` role on `teamId`)

#### `POST /api/v1/teams/{teamId}/scan-sessions`

Creates a new scan session for the authenticated user.

**Request body:**
```json
{ "order_id": "uuid" }
```

**Response `201 Created`:**
```json
{
  "id": "uuid",
  "token": "scan_<64 hex chars>",
  "expires_at": "2026-04-01T14:00:00Z"
}
```

**Response `409 Conflict`** (existing active session):
```json
{
  "error": "active scan session already exists",
  "code": "SCAN_SESSION_EXISTS",
  "existing_session_id": "uuid"
}
```

The session is created with `context_type = 'receive_order'`, `context_order_id`, and `context_order_ref` (the order's purchase order number or ID prefix for phone display).

#### `DELETE /api/v1/teams/{teamId}/scan-sessions/{sessionId}`

Deletes the session immediately. Phone's next poll returns `410 Gone`.

**Response `204 No Content`**

---

### Phone Endpoints (Require scan token via Bearer auth; no team role check)

The scan token prefix `scan_` is detected in `lookupToken` and resolved via `GetScanSessionByTokenHash`. The `User` struct is populated from the session's `user_id`. These endpoints enforce team access directly via the session record (no `RequireTeamRole` middleware).

#### `GET /api/v1/scan/session`

Phone polls every 2 seconds.

**First call for this session:** sets `phone_connected = true`, publishes `scan_session_changed` SSE to session owner. If `phone_connected` was already `true`, returns `409 Conflict` with `{"error": "session already connected"}` (second device rejection).

**Subsequent calls:** no state change, returns current state.

**If session is expired or deleted:** `410 Gone`.

**Response `200 OK`:**
```json
{
  "session_id": "uuid",
  "context_type": "receive_order",
  "context_order_id": "uuid",
  "context_order_ref": "PO-2024-001",
  "pending_scan": null
}
```

`pending_scan` is `null` or the pending scan object. Non-null value transitions phone to confirming state (recovery path if `POST /scan/scans` response was lost).

#### `POST /api/v1/scan/scans`

Submit a raw barcode string.

**Request:** `{ "barcode": "<raw string>" }`

If session has no context: `409 Conflict` → `{"error": "no active context"}`.

**Response — match `200 OK`:**
```json
{
  "status": "matched",
  "line_item_id": "uuid",
  "part_name": "BC847 NPN Transistor",
  "mpn": "BC847",
  "sku": "296-1234-1-ND",
  "barcode_qty": 100,
  "quantity_ordered": 50,
  "already_received": 0,
  "over_receive": true
}
```

`over_receive` is `true` when `barcode_qty > (quantity_ordered - already_received)`.

Also stores result as `pending_scan` in DB.

**Response — no match `200 OK`:**
```json
{ "status": "no_match", "parsed_mpn": "BC847", "parsed_sku": "296-1234-1-ND" }
```

**Response — parse error `200 OK`:**
```json
{ "status": "parse_error", "raw": "<original string>" }
```

Note: scan outcomes always return `200` with a `status` field (not HTTP errors) for uniform phone-side handling.

#### `POST /api/v1/scan/confirm`

Applies `pending_scan`. Accepts optional qty override.

**Request:** `{ "qty": 95 }` (omit to use `barcode_qty` from pending_scan)

Internally calls the same business logic as `ReceiveOrderItem`. Over-receive is permitted (user already accepted the warning on phone). Clears `pending_scan`. Publishes `order_changed` SSE.

**Response `200 OK`:**
```json
{ "ok": true, "new_quantity_received": 95, "order_status": "partial" }
```

If no `pending_scan`: `409 Conflict`.

#### `POST /api/v1/scan/skip`

Clears `pending_scan` without acting. Camera resumes on phone.

**Response `200 OK`:** `{ "ok": true }`

---

## SSE Extension: TargetUserID

### Change to `worker.Event`

```go
type Event struct {
    Type         string
    Data         json.RawMessage
    JobID        string
    TeamID       string          // fan out to this team's subscribers
    UserID       string          // SKIP delivery to this user (existing)
    TargetUserID string          // deliver ONLY TO this user (new)
}
```

`UserID` and `TargetUserID` are mutually exclusive. `TargetUserID` takes precedence.

### Fan-out logic change

```go
// In the SSE hub publish loop:
for _, sub := range teamSubs {
    if event.TargetUserID != "" {
        if sub.userID != event.TargetUserID {
            continue  // skip everyone except the target
        }
    } else if event.UserID != "" && sub.userID == event.UserID {
        continue  // existing: skip the acting user
    }
    select {
    case sub.ch <- ev:
    default:
    }
}
```

### New event type: `scan_session_changed`

```go
worker.Event{
    Type:         "scan_session_changed",
    TeamID:       session.TeamID,
    TargetUserID: session.UserID,
    Data:         mustMarshal(map[string]any{
        "session_id":      session.ID,
        "phone_connected": true,
    }),
}
```

Frontend `ScanModal` subscribes to `scan_session_changed` events where `data.session_id` matches the current modal's session ID, then auto-closes.

---

## Frontend: Global Scanner State Store

New file: `web/src/lib/stores/scanSession.svelte.ts`

```typescript
type ScanSessionState = {
  id: string;
  teamId: string;
  orderId: string;
  orderRef: string;
  token: string;
  connected: boolean;
} | null;

export const scanSessionStore = /* $state<ScanSessionState> */ null;

export function setScanSession(s: ScanSessionState): void { ... }
export function clearScanSession(): void { ... }
export function markConnected(): void { ... }
```

The `ScanModal` writes to this store on session create and clears it on disconnect. The AppShell reads from it to show/hide the badge. The badge's popover uses `scanSessionStore.orderId`/`orderRef` for the label.

---

## Frontend Components

### `web/src/lib/components/layout/AppShell.svelte` Changes

Add a scanner badge area to the AppShell header (the topbar row common to all pages). When `scanSessionStore` is non-null and `connected = true`:

```
[Logo | Nav links ...]          [🟢 Scanner connected  ▾]
```

The badge is a controlled `Popover.Root` in the AppShell. Clicking opens:
```
┌─────────────────────────────────┐
│ 🟢 Scanner connected            │
│    Receiving PO-2024-001        │
│                                 │
│              [Disconnect]       │
└─────────────────────────────────┘
```

Disconnect button calls `DELETE /api/v1/teams/{teamId}/scan-sessions/{id}` and clears the store.

### `web/src/lib/components/scanner/ScanModal.svelte`

Desktop QR pairing dialog (bits-ui `Dialog.Root`).

**Props:** `{ teamId: string; orderId: string; open: boolean; onOpenChange: (v: boolean) => void; }`

**Behavior:**
1. On `open = true`: calls `POST /api/v1/teams/{teamId}/scan-sessions` with `{order_id: orderId}`.
   - On `409 SCAN_SESSION_EXISTS`: shows inline warning with **Replace** / **Cancel** buttons.
   - **Replace** path: `DELETE /api/v1/teams/{teamId}/scan-sessions/{existing_session_id}` → retry POST.
2. Generates QR URL: `${window.location.origin}/scan?token=${token}`.
3. Renders QR SVG using `qrcode` package: `QRCode.toString(url, { type: 'svg', errorCorrectionLevel: 'M' })`.
4. Status: **"Waiting for phone…"** with a subtle spinner.
5. Subscribes to SSE `scan_session_changed` events (filtered by `session_id`).
6. On phone connected:
   - Calls `setScanSession({id, teamId, orderId, orderRef, token, connected: true})`.
   - Calls `onOpenChange(false)` — modal closes.
7. On manual close (`onOpenChange(false)` without phone connecting): calls `DELETE` scan session, calls `clearScanSession()`.

### `web/src/routes/Scan.svelte` (Mobile scan page)

- **Route:** `/scan`
- **Auth:** No session required; token from `?token=` query param
- **Shell:** Shellless, full-screen, dark background, mobile-first

**States and transitions:**

| State | Camera | What's shown |
|---|---|---|
| `loading` | off | Spinner |
| `no_context` | off | Logo + "Ready — open an order on the desktop to start scanning" |
| `scanning` | active | Viewfinder + context banner at top |
| `confirming` | paused | Confirmation panel (part info, editable qty, Confirm/Skip) |
| `confirming_over_receive` | paused | Confirmation panel + orange warning banner |
| `error_no_match` | paused | "Not in this order" + [Scan Next] |
| `error_parse` | paused | "Unrecognized barcode format" + [Scan Next] |
| `expired` | off | "Session expired — scan the QR code again" |

**Camera:** Uses `@zxing/browser` `BrowserMultiFormatReader`, dynamically imported on mount. Scan hints: prioritize `DATA_MATRIX`, then `QR_CODE`, then `CODE_128`. Continuous decode from `<video autoplay playsinline muted>`. On decode: reset reader (stops scanning) → POST to backend. On confirm/skip: restart continuous decode.

**Polling:** `setInterval` at 2000ms. If `pending_scan` arrives in poll and phone missed the `POST /scan/scans` response, transitions to confirming state. Cleared on component destroy.

**Second-device rejection:** If `GET /api/v1/scan/session` returns 409 on the first call (session already connected), show: "This scanner session is already active on another device."

### `web/src/routes/team/OrderDetail.svelte` Changes

1. Import `ScanModal`.
2. Add `let scanOpen = $state(false)`.
3. Add **"Scan"** button in topbar `actions` snippet (always present, not gated on `receivingMode`).
4. Render `<ScanModal {teamId} orderId={order.id} open={scanOpen} onOpenChange={(v) => { scanOpen = v; }} />`.
5. No changes to line item receive logic — `order_changed` SSE handles desktop updates automatically.

### `web/src/App.svelte` Changes

```svelte
import Scan from './routes/Scan.svelte';

// ROUTES table (add before catch-all):
{ pattern: '/scan', component: Scan },

// isShellless:
let isShellless = $derived(
  ['/login', '/reset-password', '/scan'].includes(currentRoute().path)
);

// publicPaths (auth redirect bypass):
const publicPaths = ['/login', '/reset-password', '/scan'];
```

---

## Barcode Parser (`internal/scanner/ipc2d.go`)

### Supported Formats

**Format A — DigiKey (ISO 15434 Format 06)**
Header: `[)>\x1e06\x1d`  
Record separator: `\x1d` (GS)  
Group separator: `\x1e` (RS)

**Format B — Mouser (ISO 15434 Format 12 or 06)**
Same structure; accept both `06` and `12` format headers.

**Field prefixes** (after splitting on `\x1d`):
| Prefix | Field |
|---|---|
| `P` | Distributor SKU |
| `1P` | MPN |
| `Q` | Quantity (integer) |
| `1T` | Date code / lot |
| `4L` | Country of origin |
| `11K` | Customer order reference |

### API

```go
package scanner

type ParsedBarcode struct {
    SKU      string
    MPN      string
    Quantity int
    Lot      string
}

// Parse attempts to parse raw as IPC-2D (DigiKey or Mouser format).
// Returns ErrUnknownFormat if the header is not recognized.
// Returns ErrParseFailure if the header is recognized but fields are malformed.
func Parse(raw string) (*ParsedBarcode, error)

var (
    ErrUnknownFormat = errors.New("unrecognized barcode format")
    ErrParseFailure  = errors.New("barcode parse failure")
)
```

**Do NOT fall back to treating unknown formats as MPN strings.** Return `ErrUnknownFormat` cleanly; avoid false positives.

### Line Item Matching (in handler)

Given `ParsedBarcode` and `context_order_id`:

1. Fetch all line items for the order (including fully received ones).
2. **SKU match**: if `parsed.SKU != ""`, find line items whose preferred/associated listing has `sku = parsed.SKU`. If one match: use it.
3. **MPN fallback**: find line items where `part.mpn = parsed.MPN`. If one match: use it. If multiple: use the one with the largest remaining quantity.
4. **Fully received line items** are valid match candidates — produce over-receive result.
5. **No match at all**: return `{status: "no_match"}`.

---

## Auth Middleware Extension (`internal/middleware/auth.go`)

Add a third lookup path in `lookupToken` after the API token check:

```go
// Scan session token.
if strings.HasPrefix(raw, "scan_") {
    row, err := m.q.GetScanSessionByTokenHash(ctx, hash)
    // GetScanSessionByTokenHash JOINs to users and filters expires_at > NOW()
    if err == nil {
        user := &User{
            ID:          row.UserID.String(),
            DisplayName: row.DisplayName,
            Email:       row.Email,
            IsActive:    row.IsActive,
            IsAdmin:     row.IsAdmin,
            // no sessionID or apiTokenID set
        }
        return user, nil
    }
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, nil
    }
    return nil, err
}
```

No caching for scan session tokens (short-lived, low frequency). If caching is desired later, use the same pattern as API tokens with `ExpiresAt`.

---

## SQL Queries (`db/queries/scan_sessions.sql`)

| Query name | Operation |
|---|---|
| `CreateScanSession` | INSERT; returns id |
| `GetScanSessionByTokenHash` | SELECT JOIN users WHERE token_hash=$1 AND expires_at > NOW() |
| `GetScanSession` | SELECT WHERE id=$1 AND team_id=$2 |
| `GetActiveScanSessionForUser` | SELECT WHERE user_id=$1 AND expires_at > NOW() LIMIT 1 |
| `SetScanSessionPhoneConnected` | UPDATE SET phone_connected=true WHERE id=$1 |
| `SetScanSessionPendingScan` | UPDATE SET pending_scan=$2 WHERE id=$1 |
| `ClearScanSessionPendingScan` | UPDATE SET pending_scan=NULL WHERE id=$1 |
| `DeleteScanSession` | DELETE WHERE id=$1 AND team_id=$2 |
| `DeleteExpiredScanSessions` | DELETE WHERE expires_at < NOW() |

---

## Session Lifecycle

```
POST /scan-sessions (desktop)
  │  → token generated, session created with context
  │  → 409 if existing active session for user
  │
ScanModal shows QR code, polls/waits for scan_session_changed SSE
  │
GET /scan/session (phone, first call)
  │  → phone_connected = true
  │  → scan_session_changed SSE → desktop modal auto-closes
  │  → AppShell badge appears
  │  → second device: 409 "already connected"
  │
  ├── [Scanning loop]
  │     POST /scan/scans → pending_scan stored → response to phone
  │     POST /scan/confirm (with qty) → receive applied → order_changed SSE
  │     POST /scan/skip → pending_scan cleared
  │
DELETE /scan-sessions/{id} (desktop Disconnect)
  │  → Session deleted immediately
  │  → Phone next poll: 410 Gone → "Session expired" screen
  │  → AppShell badge clears
  │
OR expires_at reached
  └── lookupToken: no rows → 401 → "Session expired" screen
```

---

## New Dependencies

| Package | Where | Purpose |
|---|---|---|
| `@zxing/browser` | `Scan.svelte` (dynamic import) | Camera barcode/data matrix decoding |
| `qrcode` | `ScanModal.svelte` | QR code SVG generation |

---

## Files to Create / Modify

| File | Action |
|---|---|
| `db/migrations/000029_scan_sessions.up.sql` | Create |
| `db/migrations/000029_scan_sessions.down.sql` | Create |
| `db/queries/scan_sessions.sql` | Create |
| `internal/scanner/ipc2d.go` | Create |
| `internal/scanner/ipc2d_test.go` | Create (DigiKey + Mouser sample strings) |
| `internal/handler/scan.go` | Create |
| `internal/middleware/auth.go` | Modify (scan token lookup) |
| `internal/worker/queue.go` | Modify (add TargetUserID; update fan-out) |
| `internal/server/routes.go` | Modify (register scan routes) |
| `web/src/lib/stores/scanSession.svelte.ts` | Create |
| `web/src/routes/Scan.svelte` | Create |
| `web/src/lib/components/scanner/ScanModal.svelte` | Create |
| `web/src/lib/components/layout/AppShell.svelte` | Modify (scanner badge) |
| `web/src/App.svelte` | Modify (route + shellless + public path) |
| `web/src/routes/team/OrderDetail.svelte` | Modify (Scan button + ScanModal) |
| `doc/api.md` | Update (add scan session endpoints) |

---

## Verification Checklist

1. `go build ./...` — backend compiles
2. `sqlc generate && go build ./...` — generated types consistent
3. `npm run check` (in `web/`) — TypeScript clean
4. Migration applies: `scan_sessions` table created
5. **Desktop flow:**
   - Open any order → "Scan" button visible (always, not just in receive mode)
   - Click Scan → QR modal opens with QR code and "Waiting for phone…"
   - If existing active session: warning + Replace/Cancel shown
6. **Phone flow:**
   - Open QR URL on phone → `/scan` page loads without login redirect
   - First `GET /scan/session`: desktop modal closes, AppShell badge appears green
   - Second device opening same URL: "already connected" message
   - No-context state shown while no context set (shouldn't occur in Phase 1 since context is set at creation)
7. **Scan flow:**
   - Phone scans DigiKey data matrix → confirmation panel with correct part, MPN, qty
   - Mouser format also parsed correctly
   - Unknown barcode → "Unrecognized barcode format"
   - MPN not in order → "Not in this order"
   - Over-receive → orange warning banner before confirm
   - Quantity field is editable; user changes it; confirm uses edited value
   - Confirm → desktop line item updates within ~2s (via order_changed SSE)
   - Skip → camera resumes
8. **Disconnect flow:**
   - Click AppShell badge → popover with order ref + Disconnect button
   - Disconnect → badge clears, phone shows "Session expired"
   - Navigate to different page → badge persists
9. **Expiry:** Manually set `expires_at` to past in DB → phone next poll shows "Session expired"
