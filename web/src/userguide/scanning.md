# Scanning

Distributor bags, reels, and boxes carry 2D barcodes (DataMatrix or QR codes) that encode the part number, quantity, lot, and order reference. partrack can decode these labels so you identify a part and its quantity without typing anything. You pair your phone as the camera — no dedicated hardware needed — and scans flow to your desktop in real time over a live connection.

## What scanning gives you

- **Identify parts instantly** by pointing your phone at a distributor bag.
- **Receive orders by scanning** each bag: partrack matches the label to the correct order line item and pre-fills the quantity.
- **Adjust stock on the fly**: after a bag scan, set or correct the quantity on hand directly from the phone.
- **Catalogue unknown parts** when a scanned bag is not yet in your parts catalog: import it from the distributor API in a few taps.
- **Batch-process many parts** (Bulk Scan mode) and commit them to inventory together after reviewing on the desktop.
- **Walk a storage location** by scanning its QR code to see what is stored there.
- **Kit a build** by having the phone guide you part-by-part through a pick list.

Scanning requires at least the **member** role for operations that modify stock. Any team member can look up parts; only members and above can apply stock adjustments or confirm order receipts. See [roles and permissions](roles-and-permissions.md) for details.

---

## Supported label formats

partrack recognises three barcode formats. The scanner will tell you if a label cannot be decoded.

| Format | Used by | Barcode type | Fields extracted |
|---|---|---|---|
| ISO 15434 / IPC-2D (format 06) | DigiKey | DataMatrix | SKU (`30P`/`P`), MPN (`1P`), quantity (`Q`), lot (`1T`), date code (`9D`), your PO number (`K`), distributor sales-order number (`1K`), country of origin (`4L`) |
| ISO 15434 / IPC-2D (format 12) | Mouser | DataMatrix | SKU (`P`), MPN (`1P`), quantity (`Q`), manufacturer (`1V`), lot (`1T`), invoice number (`11K`) |
| LCSC `{key:value}` | LCSC / JLCPCB | QR code | SKU (`pc`), MPN (`pm`), quantity (`qty`), lot (`pbn`), order number (`on`) |

The scanner also reads partrack's own part-level QR codes (which embed an explicit distributor name marker so they are identified unambiguously) and storage-location QR codes generated from the [Storage](storage.md) section.

Plain text barcodes (e.g. a single MPN string with no IPC-2D header) are **not** decoded as fallback. If a bag shows only a linear barcode without a 2D code, it will not scan. Most modern distributor bags include a DataMatrix or QR code; older or custom packaging may not.

**Some decoders substitute the ISO 15434 control characters** (RS `\x1E`, GS `\x1D`, EOT `\x04`) with their Unicode Control Picture equivalents (U+241E, U+241D, U+2404). partrack handles this automatically.

---

## Pairing your phone as a scanner

partrack's phone scanner runs entirely in your phone's mobile browser — no app to install.

### Starting a session

1. On the desktop, open any page in a team. At the bottom of the sidebar you will see a **Connect Phone** button.
2. Click **Connect Phone**. A dialog opens and displays a QR code.
3. On your phone, open your camera app and scan the QR code. Your browser opens the scanner page at `/scan`.
4. The dialog on the desktop closes automatically when the phone connects. The sidebar badge changes to **Scanner connected** (green dot).

You can also start a session from an order detail page: the "Scan" button in the topbar opens the same dialog with that order pre-loaded as the scanning context.

There is **one active session per user**. If you already have an active session, clicking Connect Phone checks whether the phone is live:

- If the phone is already connected, the dialog pushes the current order context (if any) to the phone and closes.
- If the session exists but the phone is not live (e.g. after a browser refresh on the phone), the old session is deleted and a new one is created.

### The QR code and security

The QR code encodes a short-lived, single-use **pairing ticket** (valid for 60 seconds) in the URL fragment (`#pt=…`). This fragment is never sent to the server in HTTP requests or proxy logs. The ticket is exchanged for a durable **scan token** (valid for 24 hours) when the phone opens the page. The scan token is stored in the phone browser's `localStorage` and sent only in `Authorization: Bearer` headers — it never appears in a URL.

For SSE streaming, the phone mints a separate single-use stream ticket (30-second lifetime) immediately before opening the stream. This keeps the durable token out of URLs. If the SSE connection drops, the phone automatically mints a fresh stream ticket and reconnects.

### Connection status

The sidebar shows one of three states:

| State | Meaning |
|---|---|
| **Connect Phone** (no badge) | No active session for your account |
| **Scanner connected** (green) | Session active and phone SSE stream is live |
| **Scanner disconnected** (grey) | Session active but phone is not currently streaming |

Click the badge to open a popover. From there you can **Disconnect** (terminates the session) or **Reconnect** (opens the pairing dialog with a fresh QR code).

### Disconnecting and reconnecting

- **Disconnect** deletes the scan session server-side. The phone receives a "session expired" screen on its next poll.
- If the phone browser tab is closed and reopened: the stored scan token is still valid (up to 24 hours). The phone loads, checks the token, and reconnects automatically.
- If the phone browser refreshes in the middle of a scan: state is recovered. The phone polls the session endpoint on load and resumes any active kitting session or bulk session.
- If the server restarts: all in-memory ScanHub connections and ticket store entries are lost. The durable scan session record persists in the database so the phone can reconnect with its stored token; the desktop badge will show "disconnected" until it does. Pairing tickets and stream tickets minted before the restart are gone and will be rejected — the phone will handle this by showing "Session expired" for stream errors, prompting you to re-pair.

---

## Phone scanner screens

After connecting, the phone shows a menu with four options. Navigate back to the menu at any time using the back arrow at the top of each screen.

### Menu

The main menu lists:

- **Scan** — look up a part by scanning its bag label or QR code, manage stock
- **Receive Order** — receive an order by scanning each bag
- **Find Part** — search the parts catalog by name or MPN
- **Storage** — assign a part to a storage location by scanning its storage QR code

Below the menu, the **Recent** list shows parts you have scanned in the current browser session, with their description and storage location label. Tap a recent part to jump directly to its detail screen.

If the desktop has pushed an order context to the phone, you will see it shown under the menu header (for example, "Order context: PO-1234"). The **Receive Order** description also reflects the active order.

### Scan (bag scan)

Tap **Scan** to enter the camera view. Point your phone at a bag label DataMatrix or LCSC QR code and hold the shutter button to scan. The scanner continuously decodes frames while the button is held and stops as soon as a code is found.

**If the part is found in the catalog:**

- If it is a new inventory entry (never tracked before), you land on the **Stock Adjustment** screen to set the initial quantity.
- If it already has an inventory entry, you land on the **Part Detail** screen showing the current quantity on hand and storage location.

**If the part is not in the catalog:**

You land on the "Not found" screen which shows the parsed MPN, SKU, and distributor extracted from the label. From here you can:

- **Import from distributor API** (if the distributor is configured for your team and a SKU was parsed from the label) — partrack fetches the part data from the distributor and creates the part. If there are multiple matching distributor listings, a listing picker lets you choose which one to import.
- **Search distributors** — manually search a distributor by MPN if no SKU was available.

When a "not found" scan is logged, partrack records it in the scan activity log. If the same part is later imported (by any team member), the activity log entry is resolved automatically.

**If the barcode cannot be decoded:** you see a "parse error" notification. Check that the 2D code (DataMatrix or QR) is in focus and well-lit, and try again. Plain-text or 1D barcodes will not decode.

**Location scanning:** if you scan a storage location QR code (generated from [Storage](storage.md)) instead of a part label, the phone navigates to the Location Inventory screen showing what is stored there.

**Bulk mode:** the Scan screen also doubles as the entry point for Bulk Scan mode (see [Bulk Scan](#bulk-scan)). When bulk mode is active, a counter badge appears and each decoded barcode is added to the batch rather than opening a detail screen.

### Receive Order

Use this screen to scan in the bags from a distributor delivery and credit them to an open order.

1. Tap **Receive Order**. If an order context was already pushed from the desktop (e.g. you clicked Scan from an order detail page), you go straight to scanning. Otherwise, an order list shows all receivable orders for the team. Tap the order you are receiving.
2. Point the phone at a bag label and tap the shutter button.
3. partrack matches the scanned label to an order line item — SKU match first, MPN fallback.
4. The confirmation screen shows:
   - Part name, MPN, and SKU
   - Quantity encoded in the barcode (editable — change it if the actual count differs)
   - Quantity ordered and quantity already received
5. If the barcode quantity would take the received total **above** the ordered quantity, a warning banner appears. You must explicitly confirm the over-receipt.
6. Tap **Confirm** to apply the receipt. The order and inventory update immediately and the desktop reflects the change.
7. After confirming, you can assign or change the part's storage location from the confirmation screen.
8. Tap **Skip** to discard the current scan without applying it (for example, if it matched the wrong line item).

When all line items are fully received, the order transitions to "Received" status and the phone shows an order-complete screen.

If a scanned label does not match any line item in the selected order, you see "Not in this order". Check that you have selected the correct order and try again.

### Find Part

Type a part name, MPN, or description to search the catalog. Results show the current quantity on hand and storage location. Tap a part to open the Part Detail screen.

The search is limited to 20 results. Use a more specific query if you do not see the part you expect.

### Part Detail

Shows a part's MPN, description, current quantity on hand, storage location, and distributor/SKU if known. From here you can:

- **Adjust stock** — opens the Stock Adjustment screen to add or subtract quantity, or set the quantity to a specific value.
- **Change storage location** — assign or move the part to a different storage location.

### Stock Adjustment

Two modes are available:

- **Set** (`set`) — enter the new absolute quantity. The delta applied is `new_quantity − current_quantity_on_hand`.
- **Adjust** (`adjust`) — enter a signed delta (positive to add, negative to remove).

Stock cannot go negative. If your adjustment would result in a negative quantity on hand, it is rejected.

An optional reason field records why the adjustment was made. If left blank, the reason defaults to "Manual scan adjustment" in the stock ledger.

After saving, the adjustment is added to the immutable stock ledger as a General Checkout or stock correction entry (see [parts and inventory](parts-inventory.md)).

### Storage

Tap **Storage** from the menu to assign parts to storage locations by scanning. Scan a storage location QR code to select it, then scan part bags — each part's inventory entry is updated to that location. Useful when sorting a delivery into bins or organising a shelf.

See [Storage](storage.md) for how to create storage locations and generate QR codes for them.

---

## Bulk Scan

Bulk Scan is a two-phase workflow: scan everything in a session on the phone, then review and commit the batch on the desktop.

### When to use Bulk Scan

Bulk Scan is suited for:

- Checking in a large order where you want to scan quickly and review before committing.
- Stocktaking a drawer or shelf.
- Processing a mixed delivery where some parts may be new to the catalog.

For receiving a specific known order scan by scan, the **Receive Order** flow is usually faster.

### Phase 1: Scanning (on the phone)

1. Open the **Scan** screen on the phone.
2. Enable **Bulk mode** using the toggle at the top of the screen. A counter badge appears.
3. Scan bags one by one. Each decoded barcode is added to the bulk session.
4. If a barcode quantity cannot be read automatically, a small overlay appears asking you to enter the quantity manually.
5. The phone detects duplicate barcodes within the same session and skips them rather than adding them twice.
6. When you are finished scanning, tap **Close session** to mark the session as complete and navigate to the review screen on the desktop. The phone sends a focus request to the desktop to open the review page.

**Resuming after disconnect:** if the phone disconnects or the browser is reloaded mid-session, it automatically resumes an active bulk session on reconnect. If the session was inactive for more than 15 minutes, you are offered a choice to resume or start fresh.

### Phase 2: Review (on the desktop)

Navigate to **Bulk Scan Review** in the sidebar. A badge count shows items awaiting review.

The review page shows all items in the session:

| Status indicator | Meaning |
|---|---|
| Yellow dot — processing | Part lookup or distributor resolution in progress |
| Blue dot — ready | Part identified and awaiting your action |
| Green dot — done | Reviewed (stock adjustment applied) |
| Dismissed | Skipped without any action |

**For each item** you can:

- **Apply stock adjustment** — confirm or edit the quantity and commit it to the inventory stock ledger.
- **Dismiss** — skip this item without making any change.
- **Retry** — if distributor resolution failed (e.g. an API timeout), retry the lookup.
- **Retry specific distributors** — retry only a subset of distributors if some failed.

If a part was not in the catalog when scanned, the review page shows the resolve status. Once the distributor lookup completes, the resolved part name and MPN appear and you can proceed to apply the adjustment.

Completed sessions (all items reviewed or dismissed) are hidden from the session list by default. Use "Show completed" to view them. You can delete a session once you are done with it.

### Linking to a storage location

Bulk Scan itself does not have a built-in storage location field for the whole batch. If you want parts landing in a specific location, either:

- Use the **Storage** screen on the phone before scanning to set the storage location on each inventory entry individually.
- After review on the desktop, update the storage location on the inventory entries from the [Inventory](parts-inventory.md) page.

See [Storage](storage.md) for details on locations and coordinates.

---

## Desktop integration

### Scanner badge (sidebar)

The scanner badge at the bottom of the sidebar is visible across all pages. It reflects whether your paired phone is currently live. Click it to disconnect or open the reconnect dialog.

### Pushing order context to the phone

From any **Order Detail** page, click the **Scan** button in the topbar. If a phone is already connected, this pushes the order to the phone immediately without opening the full pairing dialog. The phone auto-navigates to the Receive Order screen with that order selected.

### Build Kitting

From a build detail page (pending, ordered, or ready builds), you can start a **kitting session** from the desktop. This pushes the build's BOM as a guided pick list to the phone. The phone then walks you line by line through each part, letting you confirm the quantity pulled or flag a shortfall. Progress updates in real time on the desktop. See [Builds](builds.md) for the full kitting workflow.

### Scan-to-focus

From the phone, tapping "Open on Desktop" (available in the bulk session review flow) sends a focus request to the desktop. The desktop must have an active SSE connection; if it does, it navigates to the review page for that session automatically.

---

## Limitations and gotchas

- **One session per user.** Only one scan session can be active per user at a time. Starting a new session from the desktop removes any existing one.
- **One phone per session.** A single session can only have one phone connected. A second phone opening the same pairing URL will find the ticket already consumed.
- **In-memory state is per-process.** The ScanHub (live connections), ticket store, and SSE routing are all in-process. If the server restarts, live connections drop and all in-flight tickets are invalidated. The durable session record and scan token survive (they are in the database); the phone will show "Session expired" for the stream error, but can reconnect. pairing tickets in QR codes that were generated before the restart are gone and a new QR code is needed.
- **Single instance only.** Running multiple server replicas against the same database is not supported. In-memory state (ScanHub, ticket store) is not shared between instances.
- **Camera requires HTTPS.** The phone scanner uses the browser's `getUserMedia` API, which is blocked on plain HTTP (except `localhost`). The scanner page must be served over HTTPS.
- **Camera permission.** The first time you use the scanner, the browser will prompt for camera access. If you deny it, go to your browser's site settings to re-allow it.
- **Supported barcode types.** The scanner reads **DataMatrix** and **QR codes** only. 1D barcodes (EAN, UPC, Code 128, etc.) and plain text strings are not decoded.
- **Format 06 vs 12.** Both DigiKey and Mouser can use ISO 15434 format 06. The distributor is identified by heuristic signals in the barcode fields (e.g. the `30P` field is DigiKey-specific; format 12 or an `11K` invoice number without other DigiKey signals indicates Mouser). In rare cases, heuristics may mis-identify the distributor; the SKU is still extracted correctly so lookup will succeed if the part has a listing under that SKU.
- **Bulk session inactivity timeout.** A bulk session is considered timed out if no item has been added for more than 15 minutes. The phone will offer to resume or start fresh when you return.
- **Scan token lifetime.** The durable scan token is valid for 24 hours. After expiry, the phone shows "Session expired — scan a new QR code". Delete the old session from the desktop badge and open a fresh pairing dialog.
- **Stock cannot go negative.** Stock adjustment submissions that would result in negative quantity on hand are rejected.
- **Receive Order requires a placed or partially-received order.** Scanning against a draft, cancelled, or fully-received order will fail with "order is not in a receivable state".
