package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

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

// Area 1 H1: the by-user phone endpoints (GetScanSession/SubmitScan/ConfirmScan/
// SkipScan) must resolve the session by the presented TOKEN, not by the (shared)
// authenticated user. With two active sessions for one user, by-user resolution
// (GetActiveScanSessionForUser: LIMIT 1, no ORDER BY) returns the same arbitrary
// session for every token, so a scan acts on the wrong session/team.
func TestVerify_ScanSession_TokenBound(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	// One user, two teams, two active scan sessions (the DB allows this — the create
	// guard is non-atomic, and testutil seeds the rows directly).
	user := testutil.CreateUser(t, testDB, "tokbind@example.com", "pw", "User")
	teamA := testutil.CreateTeam(t, testDB, user, "member", "Team A")
	teamB := testutil.CreateTeam(t, testDB, user, "member", "Team B")
	tokenA := testutil.CreateScanSession(t, testDB, user, teamA)
	tokenB := testutil.CreateScanSession(t, testDB, user, teamB)

	// Pre-connect both so GetScanSession performs no write — a write would reorder the
	// heap and let the by-user LIMIT-1 query return a different row per call by chance,
	// masking the bug. Read-only, the by-user pick is deterministic (same row each call).
	if _, err := testDB.Exec(ctx, `UPDATE scan_sessions SET phone_connected = true WHERE user_id = $1`, user); err != nil {
		t.Fatalf("pre-connect sessions: %v", err)
	}

	bodyA, statusA := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/scan/session", nil, tokenA)
	bodyB, statusB := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/scan/session", nil, tokenB)
	if statusA != http.StatusOK || statusB != http.StatusOK {
		t.Fatalf("get session: statuses A=%d B=%d", statusA, statusB)
	}

	idA, _ := bodyA["session_id"].(string)
	idB, _ := bodyB["session_id"].(string)
	if idA == "" || idB == "" {
		t.Fatalf("missing session_id: A=%q B=%q", idA, idB)
	}
	// Each token must resolve to its OWN session. By-user resolution returns the same
	// session for both tokens, so idA == idB until the endpoints are token-bound.
	if idA == idB {
		t.Errorf("both scan tokens resolved to the same session %s — endpoints resolve by user, not by the presented token", idA)
	}
}

// Area 1 H2/H3 (== Area 5 BLOCKER-1): the desktop "Send to Scanner" kitting push
// must reach the phone under the field the phone actually reads
// (context_build_ref) and carry a real project/revision ref — NOT the old
// "build_ref" key with a "Build × N" placeholder. We subscribe to the ScanHub
// channel and inspect the pushed SSE payload.
func TestVerify_StartKitting_PushesContextBuildRef(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	user := testutil.CreateUser(t, testDB, "ci-kit@example.com", "pw", "User")
	team := testutil.CreateTeam(t, testDB, user, "member", "Team A")
	apiTok := testutil.CreateAPIToken(t, testDB, user, "desktop")

	project := testutil.CreateProject(t, testDB, team, user, "KittingProj")
	rev := testutil.CreateRevision(t, testDB, project, user, "vR1")
	build := testutil.CreateBuild(t, testDB, project, rev, team, user, 5, "pending")

	// CreateScanSession returns the bearer token; the desktop route + ScanHub key
	// by the session UUID, so look it up.
	testutil.CreateScanSession(t, testDB, user, team)
	var scanID string
	if err := testDB.QueryRow(ctx,
		`SELECT id FROM scan_sessions WHERE user_id=$1 AND team_id=$2`, user, team).Scan(&scanID); err != nil {
		t.Fatalf("look up scan session id: %v", err)
	}

	// Subscribe to the phone's SSE channel before triggering the push.
	ch, cleanup := testEnv.Handler.ScanHub.Register(scanID, "", "")
	defer cleanup()

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/teams/"+team+"/scan-sessions/"+scanID+"/kitting",
		map[string]any{"build_id": build}, apiTok)
	if status != http.StatusCreated {
		t.Fatalf("StartKitting: expected 201, got %d", status)
	}

	var raw []byte
	select {
	case raw = <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no SSE context push received within 2s")
	}

	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal push: %v (raw=%s)", err, raw)
	}
	if msg["type"] != "scan_context_changed" || msg["context_type"] != "kitting" {
		t.Fatalf("unexpected push envelope: %v", msg)
	}
	// The phone reads context_build_ref; the legacy build_ref key would be ignored.
	if _, ok := msg["build_ref"]; ok {
		t.Errorf("push still uses the legacy build_ref key (phone reads context_build_ref): %v", msg)
	}
	ref, _ := msg["context_build_ref"].(string)
	if ref == "" {
		t.Fatalf("context_build_ref missing/empty: %v", msg)
	}
	// Must be the real project/revision ref, not the "Build × N" placeholder.
	if !strings.Contains(ref, "KittingProj") || !strings.Contains(ref, "vR1") {
		t.Errorf("context_build_ref should embed project + revision; got %q", ref)
	}
	if strings.HasPrefix(ref, "Build ") {
		t.Errorf("context_build_ref is still the placeholder: %q", ref)
	}
	if s, _ := msg["context_kitting_session_id"].(string); s == "" {
		t.Errorf("context_kitting_session_id missing: %v", msg)
	}
}
