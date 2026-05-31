package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// lcscBarcode parses to SKU C2977777 / MPN CH340N (see scanner.ParseLCSC tests).
const lcscBarcode = `{pbn:PICK2205300004,on:GB2512180610,pc:C2977777,pm:CH340N,qty:10,mc:TLAN62,cc:1,pdi:190633867,hp:0,wc:zh}`

// TestIDOR_SubmitScanCrossTeam verifies a scan token cannot read another team's
// order line items by passing a foreign order_id.
func TestIDOR_SubmitScanCrossTeam(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	attacker := testutil.CreateUser(t, testDB, "scan-attacker@example.com", "pw", "Attacker")
	teamA := testutil.CreateTeam(t, testDB, attacker, "member", "Team A")
	attackerScan := testutil.CreateScanSession(t, testDB, attacker, teamA)

	victim := testutil.CreateUser(t, testDB, "scan-victim@example.com", "pw", "Victim")
	teamB := testutil.CreateTeam(t, testDB, victim, "member", "Team B")
	distID := testutil.GetTestDistributorID(t, testDB)
	partB := testutil.CreatePart(t, testDB, "team", "CH340N", &teamB)
	listingB := testutil.CreateListing(t, testDB, partB, distID, "C2977777")
	orderB := testutil.CreateOrder(t, testDB, teamB, distID, victim)
	testutil.CreateOrderLineItem(t, testDB, orderB, listingB, 10)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/scan/scans",
		map[string]any{"order_id": orderB, "barcode": lcscBarcode}, attackerScan)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	// Team scoping must prevent a match against team B's order — no leak of the
	// line item's part data.
	if body["status"] != "no_match" {
		t.Errorf("expected no_match for cross-team order, got status=%v (full: %v)", body["status"], body)
	}
}

// TestIDOR_BulkScanCrossTeam verifies a scan token cannot add items to a bulk
// session it does not own.
func TestIDOR_BulkScanCrossTeam(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	attacker := testutil.CreateUser(t, testDB, "bulk-attacker@example.com", "pw", "Attacker")
	teamA := testutil.CreateTeam(t, testDB, attacker, "member", "Team A")
	attackerScan := testutil.CreateScanSession(t, testDB, attacker, teamA)

	victim := testutil.CreateUser(t, testDB, "bulk-victim@example.com", "pw", "Victim")
	teamB := testutil.CreateTeam(t, testDB, victim, "member", "Team B")
	victimBulk := testutil.CreateBulkScanSession(t, testDB, victim, teamB)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/scan/bulk/sessions/%s/items", victimBulk),
		map[string]any{"barcode": "anything"}, attackerScan)
	if status != http.StatusNotFound {
		t.Errorf("attacker AddBulkScanItem on foreign session: expected 404, got %d: %v", status, body)
	}
}
