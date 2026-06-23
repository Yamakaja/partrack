package handler_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// Tier-1 correctness reproductions for the mobile-scanner review
// (doc/work/mobile-scanner-ui-review.md). These assert the SECURE/correct
// behavior and are EXPECTED TO FAIL against current code — each failure confirms
// a real data-integrity defect in the receive / stock-adjust ledger flow.

// A second LCSC barcode (distinct SKU/MPN) for multi-line-item tests.
// pc -> SKU, pm -> MPN, qty -> quantity.
const lcscBarcode2 = `{pbn:PICK2,on:GB2,pc:C1111111,pm:NE555,qty:3,mc:X,cc:1,pdi:2,hp:0,wc:zh}`

// Area 3 H3: ConfirmScan acts on the server's single pending-scan slot and ignores
// which line item the client actually confirmed. If the slot is overwritten by a
// later scan, a confirm the client targeted at line item A is silently applied to
// line item B. The confirm should carry + verify the line item identity and reject
// a stale mismatch (409), not blindly receive against the latest pending scan.
func TestRepro_ConfirmScan_IgnoresLineItemIdentity(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	user := testutil.CreateUser(t, testDB, "ci-h3@example.com", "pw", "User")
	teamA := testutil.CreateTeam(t, testDB, user, "member", "Team A")
	scan := testutil.CreateScanSession(t, testDB, user, teamA)
	distID := testutil.GetTestDistributorID(t, testDB)

	partA := testutil.CreatePart(t, testDB, "team", "CH340N", &teamA)
	partB := testutil.CreatePart(t, testDB, "team", "NE555", &teamA)
	listingA := testutil.CreateListing(t, testDB, partA, distID, "C2977777")
	listingB := testutil.CreateListing(t, testDB, partB, distID, "C1111111")
	order := testutil.CreateOrder(t, testDB, teamA, distID, user)
	lineA := testutil.CreateOrderLineItem(t, testDB, order, listingA, 10)
	lineB := testutil.CreateOrderLineItem(t, testDB, order, listingB, 10)
	testutil.PlaceOrder(t, testDB, order)
	testutil.CreateInventoryEntry(t, testDB, teamA, partA)
	testutil.CreateInventoryEntry(t, testDB, teamA, partB)

	// Scan A — phone now shows A's confirm screen; pending = A.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/scans",
		map[string]any{"order_id": order, "barcode": lcscBarcode}, scan)
	if status != http.StatusOK || body["status"] != "matched" {
		t.Fatalf("submit A: expected matched, got %d: %v", status, body)
	}
	// A later scan (e.g. second device / reconnect resume) overwrites pending = B.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/scans",
		map[string]any{"order_id": order, "barcode": lcscBarcode2}, scan)
	if status != http.StatusOK || body["status"] != "matched" {
		t.Fatalf("submit B: expected matched, got %d: %v", status, body)
	}

	// The client confirms what it last saw — line item A — sending its identity.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/confirm",
		map[string]any{"qty": 10, "line_item_id": lineA}, scan)

	// Secure expectation: the echoed line item (A) no longer matches the pending
	// scan (B), so the confirm must be rejected rather than mis-applied.
	if status != http.StatusConflict || body["code"] != "STALE_PENDING_SCAN" {
		t.Errorf("confirm targeting line item A while pending is B: expected 409 STALE_PENDING_SCAN, got %d (code=%v): %v", status, body["code"], body)
	}

	var recvA, recvB int32
	if err := testDB.QueryRow(ctx, `SELECT quantity_received FROM order_line_items WHERE id = $1`, lineA).Scan(&recvA); err != nil {
		t.Fatalf("read line A: %v", err)
	}
	if err := testDB.QueryRow(ctx, `SELECT quantity_received FROM order_line_items WHERE id = $1`, lineB).Scan(&recvB); err != nil {
		t.Fatalf("read line B: %v", err)
	}
	if recvA != 0 || recvB != 0 {
		t.Errorf("a stale confirm must not receive anything; got lineA.received=%d lineB.received=%d", recvA, recvB)
	}

	// The pending scan (B) must be preserved so the correct confirm still works.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/confirm",
		map[string]any{"qty": 3, "line_item_id": lineB}, scan)
	if status != http.StatusOK {
		t.Fatalf("confirming the actual pending line item B: expected 200, got %d: %v", status, body)
	}
	if err := testDB.QueryRow(ctx, `SELECT quantity_received FROM order_line_items WHERE id = $1`, lineB).Scan(&recvB); err != nil {
		t.Fatalf("read line B: %v", err)
	}
	if recvB != 3 {
		t.Errorf("expected line item B received 3 after a correct confirm, got %d", recvB)
	}
}

// Area 3 H2: ConfirmScan reads pending_scan outside the transaction and clears it
// non-transactionally AFTER commit, with no idempotency token. Concurrent confirms
// (double-tap / retry on a flaky connection) each read the still-present pending
// scan and each apply a receipt — double-counting an immutable ledger. Exactly one
// confirm should win; the rest should see "no pending scan".
func TestRepro_ConfirmScan_ConcurrentDoubleReceive(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	user := testutil.CreateUser(t, testDB, "ci-h2@example.com", "pw", "User")
	teamA := testutil.CreateTeam(t, testDB, user, "member", "Team A")
	scan := testutil.CreateScanSession(t, testDB, user, teamA)
	distID := testutil.GetTestDistributorID(t, testDB)

	partA := testutil.CreatePart(t, testDB, "team", "CH340N", &teamA)
	listingA := testutil.CreateListing(t, testDB, partA, distID, "C2977777")
	order := testutil.CreateOrder(t, testDB, teamA, distID, user)
	lineA := testutil.CreateOrderLineItem(t, testDB, order, listingA, 100) // ordered 100
	testutil.PlaceOrder(t, testDB, order)
	testutil.CreateInventoryEntry(t, testDB, teamA, partA)

	// One scan → one pending scan of qty 10.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/scans",
		map[string]any{"order_id": order, "barcode": lcscBarcode}, scan)
	if status != http.StatusOK || body["status"] != "matched" {
		t.Fatalf("submit scan: expected matched, got %d: %v", status, body)
	}

	// Fire N concurrent confirms of the same single pending scan.
	const n = 4
	var wg sync.WaitGroup
	start := make(chan struct{})
	statuses := make([]int, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req, _ := http.NewRequest(http.MethodPost, testEnv.Server.URL+"/api/v1/scan/confirm",
				strings.NewReader(`{"qty":10}`))
			req.Header.Set("Authorization", "Bearer "+scan)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				statuses[i] = resp.StatusCode
				resp.Body.Close()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	var received int32
	if err := testDB.QueryRow(ctx, `SELECT quantity_received FROM order_line_items WHERE id = $1`, lineA).Scan(&received); err != nil {
		t.Fatalf("read line item: %v", err)
	}
	t.Logf("concurrent confirm statuses=%v, final received=%d (single pending scan of qty 10)", statuses, received)
	if received != 10 {
		t.Errorf("a single pending scan (qty 10) was received as %d by %d concurrent confirms — ledger double-counted (no idempotency)", received, n)
	}
}

// Area 7 H2: the phone's "set to N" mode computes delta = N - snapshotOnHand on the
// client. If stock moved since the snapshot, applying that delta to the current
// on-hand silently produces the wrong absolute. With compare-and-set the client sends
// the expected on-hand; a mismatch must be reported (409 STALE_STOCK with the actual)
// rather than silently applied — and the user can recover with a fresh expectation.
func TestRepro_StockAdjustment_CompareAndSet(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	user := testutil.CreateUser(t, testDB, "ci-set@example.com", "pw", "User")
	teamA := testutil.CreateTeam(t, testDB, user, "member", "Team A")
	scan := testutil.CreateScanSession(t, testDB, user, teamA)

	partA := testutil.CreatePart(t, testDB, "team", "CH340N", &teamA)
	entry := testutil.CreateInventoryEntry(t, testDB, teamA, partA)
	testutil.SetStockQty(t, testDB, entry, 10) // phone reads on-hand = 10

	// Phone user chooses "set to 12" → client computes delta = 12 - 10 = 2, expects 10.
	const clientSnapshot = 10
	delta := 12 - clientSnapshot // = 2

	// Meanwhile stock actually changed (a concurrent receive/build) to 5.
	testutil.SetStockQty(t, testDB, entry, 5)

	// CAS: expected 10 != actual 5 → reject with the real current, do not apply.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/stock-adjustment",
		map[string]any{"inventory_entry_id": entry, "delta": delta, "expected_quantity": clientSnapshot}, scan)
	if status != http.StatusConflict || body["code"] != "STALE_STOCK" {
		t.Fatalf("stale set: expected 409 STALE_STOCK, got %d (code=%v): %v", status, body["code"], body)
	}
	if cur, _ := body["current_quantity"].(float64); int(cur) != 5 {
		t.Errorf("stale response should report the actual current (5), got %v", body["current_quantity"])
	}
	var onHand int32
	if err := testDB.QueryRow(ctx, `SELECT quantity_on_hand FROM inventory_entries WHERE id = $1`, entry).Scan(&onHand); err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if onHand != 5 {
		t.Errorf("a rejected CAS must not change stock; on-hand is %d, expected 5", onHand)
	}

	// Recovery: the user re-reads (5) and re-sets to 12 → delta 7, expects 5 → applies.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/stock-adjustment",
		map[string]any{"inventory_entry_id": entry, "delta": 12 - 5, "expected_quantity": 5}, scan)
	if status != http.StatusOK {
		t.Fatalf("retry with correct expectation: expected 200, got %d: %v", status, body)
	}
	if got, _ := body["new_quantity_on_hand"].(float64); int(got) != 12 {
		t.Errorf("after CAS retry, expected on-hand 12, got %v", body["new_quantity_on_hand"])
	}
}
