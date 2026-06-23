package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// Reproduction tests for the mobile-scanner review (doc/work/mobile-scanner-ui-review.md).
//
// These assert the SECURE / correct behavior, so they are EXPECTED TO FAIL against
// the current code — each failure confirms a finding live against the test DB.
// They become regression tests once the underlying bugs are fixed.

// Area 7 BLOCKER-1: GetScanPartDetail has no scope guard. A scan token for team A
// must NOT be able to read team B's private (team-scoped) part dossier.
func TestVerify_GetScanPartDetail_CrossTeamLeak(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	attacker := testutil.CreateUser(t, testDB, "pd-attacker@example.com", "pw", "Attacker")
	teamA := testutil.CreateTeam(t, testDB, attacker, "member", "Team A")
	attackerScan := testutil.CreateScanSession(t, testDB, attacker, teamA)

	victim := testutil.CreateUser(t, testDB, "pd-victim@example.com", "pw", "Victim")
	teamB := testutil.CreateTeam(t, testDB, victim, "member", "Team B")
	partB := testutil.CreatePart(t, testDB, "team", "SECRET-MPN-001", &teamB)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/scan/parts/%s", partB), nil, attackerScan)

	if status != http.StatusNotFound {
		t.Errorf("IDOR: team-A scan token read team-B private part — expected 404, got %d (leaked mpn=%v)",
			status, body["mpn"])
	}
}

// Area 4 BLOCKER-1: PatchScanInventoryEntry scopes the entry to the team but takes
// storage_location.id verbatim. A scan token must NOT be able to assign its own
// inventory entry onto another team's storage location.
func TestVerify_PatchScanInventoryEntry_CrossTeamLocation(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	attacker := testutil.CreateUser(t, testDB, "inv-attacker@example.com", "pw", "Attacker")
	teamA := testutil.CreateTeam(t, testDB, attacker, "member", "Team A")
	attackerScan := testutil.CreateScanSession(t, testDB, attacker, teamA)
	partA := testutil.CreatePart(t, testDB, "team", "PART-A", &teamA)
	entryA := testutil.CreateInventoryEntry(t, testDB, teamA, partA)

	victim := testutil.CreateUser(t, testDB, "inv-victim@example.com", "pw", "Victim")
	teamB := testutil.CreateTeam(t, testDB, victim, "member", "Team B")
	var locB string
	if err := testDB.QueryRow(ctx,
		`INSERT INTO storage_locations (team_id, label) VALUES ($1, 'Victim Shelf') RETURNING id`,
		teamB).Scan(&locB); err != nil {
		t.Fatalf("seed victim location: %v", err)
	}

	body, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/scan/inventory-entries/%s", entryA),
		map[string]any{"storage_location": map[string]any{"id": locB}}, attackerScan)

	// Secure expectation: reject (the location is not in the session's team).
	if status >= 200 && status < 300 {
		t.Errorf("IDOR: assigned team-A entry to team-B location — expected 4xx, got %d: %v", status, body)
	}

	// Independently confirm whether the cross-team location actually persisted.
	var stored *string
	if err := testDB.QueryRow(ctx,
		`SELECT storage_location_id::text FROM inventory_entries WHERE id = $1`, entryA).Scan(&stored); err != nil {
		t.Fatalf("read back entry: %v", err)
	}
	if stored != nil && *stored == locB {
		t.Errorf("IDOR confirmed: inventory_entries.storage_location_id now points to team-B location %s", locB)
	}
}

// Area 6 BLOCKER-1: SetBulkScanItemStatusHandler validates the session's team but
// acts on an arbitrary itemId. A team-A member must NOT be able to dismiss an item
// belonging to a team-B session by passing their own session id + the foreign item id.
func TestVerify_SetBulkScanItemStatus_CrossSessionItem(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	attacker := testutil.CreateUser(t, testDB, "bsi-attacker@example.com", "pw", "Attacker")
	teamA := testutil.CreateTeam(t, testDB, attacker, "member", "Team A")
	attackerToken := testutil.CreateSession(t, testDB, attacker) // desktop session
	sessionA := testutil.CreateBulkScanSession(t, testDB, attacker, teamA)

	victim := testutil.CreateUser(t, testDB, "bsi-victim@example.com", "pw", "Victim")
	teamB := testutil.CreateTeam(t, testDB, victim, "member", "Team B")
	sessionB := testutil.CreateBulkScanSession(t, testDB, victim, teamB)
	var itemB string
	if err := testDB.QueryRow(ctx,
		`INSERT INTO bulk_scan_items (session_id, barcode, status) VALUES ($1, 'VICTIM-BC', 'pending_review') RETURNING id`,
		sessionB).Scan(&itemB); err != nil {
		t.Fatalf("seed victim bulk item: %v", err)
	}

	body, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/bulk-scan-sessions/%s/items/%s", teamA, sessionA, itemB),
		map[string]any{"status": "dismissed"}, attackerToken)

	if status >= 200 && status < 300 {
		t.Errorf("IDOR: dismissed a foreign-session item via own session — expected 404, got %d: %v", status, body)
	}

	var itemStatus string
	if err := testDB.QueryRow(ctx, `SELECT status FROM bulk_scan_items WHERE id = $1`, itemB).Scan(&itemStatus); err != nil {
		t.Fatalf("read back victim item: %v", err)
	}
	if itemStatus != "pending_review" {
		t.Errorf("IDOR confirmed: victim item status mutated to %q by a cross-session request", itemStatus)
	}
}

// Area 3 — REFUTES the round-2 "over-receive is a 500 dead-end" finding.
// Migration 000012_allow_over_receipt dropped the received<=ordered CHECK, so
// over-receipt is an intentionally-supported feature: confirming a qty greater than
// ordered succeeds (200), receives the full amount, and completes the order. This
// test pins that verified behavior (and would catch a regression that re-rejected it).
func TestVerify_OverReceive_IsAllowed(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	user := testutil.CreateUser(t, testDB, "recv@example.com", "pw", "Receiver")
	teamA := testutil.CreateTeam(t, testDB, user, "member", "Team A")
	scan := testutil.CreateScanSession(t, testDB, user, teamA)

	distID := testutil.GetTestDistributorID(t, testDB)
	partA := testutil.CreatePart(t, testDB, "team", "CH340N", &teamA)
	listingA := testutil.CreateListing(t, testDB, partA, distID, "C2977777")
	orderA := testutil.CreateOrder(t, testDB, teamA, distID, user)
	lineA := testutil.CreateOrderLineItem(t, testDB, orderA, listingA, 5) // ordered 5
	testutil.PlaceOrder(t, testDB, orderA)
	testutil.CreateInventoryEntry(t, testDB, teamA, partA)

	// Scan the qty-10 LCSC barcode → matched, over-receive (10 > 5 remaining).
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/scan/scans", map[string]any{"order_id": orderA, "barcode": lcscBarcode}, scan)
	if status != http.StatusOK || body["status"] != "matched" {
		t.Fatalf("submit scan: expected matched, got %d: %v", status, body)
	}
	if body["over_receive"] != true {
		t.Errorf("expected over_receive=true for qty 10 against 5 ordered, got %v", body["over_receive"])
	}

	// Confirm the full bag qty (10) — exceeds the 5 ordered. Expected: 200, allowed.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/scan/confirm", map[string]any{"qty": 10}, scan)
	if status != http.StatusOK {
		t.Fatalf("over-receive confirm: expected 200 (over-receipt is allowed), got %d: %v", status, body)
	}

	// The line item actually received 10 (> 5 ordered) and the order is complete.
	var received int32
	var orderStatus string
	if err := testDB.QueryRow(ctx,
		`SELECT li.quantity_received, o.status FROM order_line_items li JOIN orders o ON o.id = li.order_id WHERE li.id = $1`,
		lineA).Scan(&received, &orderStatus); err != nil {
		t.Fatalf("read back line item: %v", err)
	}
	if received != 10 {
		t.Errorf("expected quantity_received=10 after over-receipt, got %d", received)
	}
	if orderStatus != "received" {
		t.Errorf("expected order status 'received' after fully receiving, got %q", orderStatus)
	}
}
