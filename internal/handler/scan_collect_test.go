package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// LCSC pick-bag barcodes: pc = SKU, pm = MPN, qty = piece count (optional).
const collectBarcodeQty10 = `{pbn:PICK1,on:GB1,pc:C555001,pm:COLLECT-A,qty:10,mc:X,cc:1,pdi:1,hp:0,wc:zh}`
const collectBarcodeNoQty = `{pbn:PICK2,on:GB2,pc:C555002,pm:COLLECT-B,mc:X,cc:1,pdi:2,hp:0,wc:zh}`
const collectBarcodeUnknown = `{pbn:PICK3,on:GB3,pc:C999999,pm:NOPE-1,qty:5,mc:X,cc:1,pdi:3,hp:0,wc:zh}`

// collectSetup: member user with team, scan token, and a collection created
// through the phone API. Returns (userID, teamID, scanToken, collectionID).
func collectSetup(t *testing.T, email string) (string, string, string, string) {
	t.Helper()
	userID := testutil.CreateUser(t, testDB, email, "pw", "Collector")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Collect Team")
	scanToken := testutil.CreateScanSession(t, testDB, userID, teamID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/scan/collections", map[string]any{"name": "Pile"}, scanToken)
	if status != http.StatusCreated {
		t.Fatalf("create collection via scan: expected 201, got %d: %v", status, body)
	}
	return userID, teamID, scanToken, body["id"].(string)
}

func TestCollectScan_QtyCaptureFlow(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, teamID, scanToken, collID := collectSetup(t, "collect-flow@example.com")

	partID := testutil.CreatePart(t, testDB, "global", "COLLECT-A", nil)
	distID := testutil.GetTestDistributorID(t, testDB)
	testutil.CreateListing(t, testDB, partID, distID, "C555001")

	// Toggle unset → treated as capture-off: bare membership despite qty:10.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/collect",
		map[string]any{"collection_id": collID, "barcode": collectBarcodeQty10}, scanToken)
	if status != http.StatusOK || body["status"] != "added" {
		t.Fatalf("expected added, got %d: %v", status, body)
	}
	if body["quantity_added"] != nil {
		t.Errorf("toggle unset: expected bare add, got quantity %v", body["quantity_added"])
	}

	// Turn capture on.
	_, status = testutil.Do(t, testEnv.Server, http.MethodPatch,
		"/api/v1/scan/collections/"+collID, map[string]any{"collect_capture_qty": true}, scanToken)
	if status != http.StatusOK {
		t.Fatalf("patch toggle: expected 200, got %d", status)
	}

	// Barcode qty is used and sums onto the (NULL) row.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/collect",
		map[string]any{"collection_id": collID, "barcode": collectBarcodeQty10}, scanToken)
	if body["status"] != "added" {
		t.Fatalf("expected added, got %v", body)
	}
	if q := body["quantity_added"].(float64); q != 10 {
		t.Errorf("expected quantity_added 10 from barcode, got %v", q)
	}
	if q := body["item_quantity"].(float64); q != 10 {
		t.Errorf("expected row quantity 10, got %v", q)
	}

	// Repeat scan sums.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/collect",
		map[string]any{"collection_id": collID, "barcode": collectBarcodeQty10}, scanToken)
	if q := body["item_quantity"].(float64); q != 20 {
		t.Errorf("expected summed row quantity 20, got %v", q)
	}
	tally := body["tally"].(map[string]any)
	if c := tally["item_count"].(float64); c != 1 {
		t.Errorf("expected 1 item in tally, got %v", c)
	}
	if q := tally["total_quantity"].(float64); q != 20 {
		t.Errorf("expected tally quantity 20, got %v", q)
	}

	// The resolve-only invariant: collecting never creates inventory entries.
	var invCount int
	testDB.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM inventory_entries WHERE team_id = $1 AND part_id = $2`,
		teamID, partID,
	).Scan(&invCount) //nolint:errcheck
	if invCount != 0 {
		t.Errorf("collect must not create inventory entries; found %d", invCount)
	}
}

func TestCollectScan_QuantityPromptFlow(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, _, scanToken, collID := collectSetup(t, "collect-prompt@example.com")
	partID := testutil.CreatePart(t, testDB, "global", "COLLECT-B", nil)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		"/api/v1/scan/collections/"+collID, map[string]any{"collect_capture_qty": true}, scanToken)
	if status != http.StatusOK {
		t.Fatalf("patch toggle: expected 200, got %d", status)
	}

	// Capture ON + barcode without qty → quantity_required, and NOTHING written.
	body, _ := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/collect",
		map[string]any{"collection_id": collID, "barcode": collectBarcodeNoQty}, scanToken)
	if body["status"] != "quantity_required" {
		t.Fatalf("expected quantity_required, got %v", body)
	}
	part := body["part"].(map[string]any)
	if part["id"].(string) != partID {
		t.Errorf("expected resolved part %s, got %v", partID, part["id"])
	}
	listBody, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		"/api/v1/scan/collections", nil, scanToken)
	colls := listBody["collections"].([]any)
	if c := colls[0].(map[string]any)["item_count"].(float64); c != 0 {
		t.Fatalf("quantity_required must not write; item_count=%v", c)
	}

	// Second phase with a typed count.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/collect",
		map[string]any{"collection_id": collID, "part_id": partID, "quantity": 25}, scanToken)
	if body["status"] != "added" || body["item_quantity"].(float64) != 25 {
		t.Fatalf("expected added with quantity 25, got %v", body)
	}

	// Skip path: part_id without quantity = bare merge, count untouched.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/collect",
		map[string]any{"collection_id": collID, "part_id": partID}, scanToken)
	if body["status"] != "added" {
		t.Fatalf("expected added, got %v", body)
	}
	if q := body["item_quantity"].(float64); q != 25 {
		t.Errorf("skip must not change the row quantity; got %v", q)
	}
}

func TestCollectScan_UnknownBarcodes(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, _, scanToken, collID := collectSetup(t, "collect-unknown@example.com")

	// Parses fine, but no such part in the catalog.
	body, _ := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/collect",
		map[string]any{"collection_id": collID, "barcode": collectBarcodeUnknown}, scanToken)
	if body["status"] != "unknown" || body["reason"] != "not_in_catalog" {
		t.Errorf("expected unknown/not_in_catalog, got %v", body)
	}

	// Unparseable garbage.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/collect",
		map[string]any{"collection_id": collID, "barcode": "garbage-not-a-barcode"}, scanToken)
	if body["status"] != "unknown" || body["reason"] != "unparsed" {
		t.Errorf("expected unknown/unparsed, got %v", body)
	}

	// Unknown scans never create list rows.
	listBody, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		"/api/v1/scan/collections", nil, scanToken)
	if c := listBody["collections"].([]any)[0].(map[string]any)["item_count"].(float64); c != 0 {
		t.Errorf("unknown scans must not create rows; item_count=%v", c)
	}
}

func TestCollectScan_ForeignCollection(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, teamID, _, collID := collectSetup(t, "collect-owner2@example.com")

	// A teammate's scan token must not reach the owner's list.
	otherID := testutil.CreateUser(t, testDB, "collect-intruder@example.com", "pw", "I")
	testutil.AddMember(t, testDB, teamID, otherID, "member")
	otherScan := testutil.CreateScanSession(t, testDB, otherID, teamID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/collect",
		map[string]any{"collection_id": collID, "barcode": collectBarcodeQty10}, otherScan)
	if status != http.StatusNotFound {
		t.Errorf("foreign collection via scan: expected 404, got %d", status)
	}

	body, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		"/api/v1/scan/collections", nil, otherScan)
	if got := len(body["collections"].([]any)); got != 0 {
		t.Errorf("teammate sees %d foreign collections via scan, want 0", got)
	}
}

func TestListScanCollectionItems(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	userID, teamID, scanToken, collID := collectSetup(t, "collect-view@example.com")

	partID := testutil.CreatePart(t, testDB, "global", "COLLECT-VIEW-A", nil)
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)
	testutil.SetStockQty(t, testDB, entryID, 42)

	desktopToken := testutil.CreateSession(t, testDB, userID)
	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s/items", teamID, collID),
		map[string]any{"part_id": partID, "quantity": 7, "note": "shelf pile"}, desktopToken)
	if status != http.StatusCreated {
		t.Fatalf("add item: expected 201, got %d", status)
	}

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		"/api/v1/scan/collections/"+collID+"/items", nil, scanToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["name"].(string) != "Pile" {
		t.Errorf("expected list name, got %v", body["name"])
	}
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	it := items[0].(map[string]any)
	if it["mpn"].(string) != "COLLECT-VIEW-A" {
		t.Errorf("expected mpn, got %v", it["mpn"])
	}
	if q := it["quantity"].(float64); q != 7 {
		t.Errorf("expected quantity 7, got %v", q)
	}
	if q := it["quantity_on_hand"].(float64); q != 42 {
		t.Errorf("expected on-hand 42, got %v", q)
	}
	if it["storage_location"] != nil {
		t.Errorf("expected nil storage_location, got %v", it["storage_location"])
	}
}

func TestPushScanContext_Collect(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	userID, teamID, _, collID := collectSetup(t, "collect-push@example.com")
	desktopToken := testutil.CreateSession(t, testDB, userID)

	// The owner's scan session id (needed for the desktop push URL).
	var sessionID string
	testDB.QueryRow(context.Background(),
		`SELECT id FROM scan_sessions WHERE user_id = $1`, userID,
	).Scan(&sessionID) //nolint:errcheck

	// Member (not purchaser) pushes a collect context to their own phone.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/scan-sessions/%s/context", teamID, sessionID),
		map[string]any{"collection_id": collID}, desktopToken)
	if status != http.StatusOK {
		t.Fatalf("collect context push as member: expected 200, got %d: %v", status, body)
	}
	if body["context_collection_name"].(string) != "Pile" {
		t.Errorf("expected pushed list name, got %v", body["context_collection_name"])
	}

	// Pushing a private list to ANOTHER user's session is refused.
	otherID := testutil.CreateUser(t, testDB, "collect-push-other@example.com", "pw", "O")
	testutil.AddMember(t, testDB, teamID, otherID, "member")
	testutil.CreateScanSession(t, testDB, otherID, teamID)
	var otherSessionID string
	testDB.QueryRow(context.Background(),
		`SELECT id FROM scan_sessions WHERE user_id = $1`, otherID,
	).Scan(&otherSessionID) //nolint:errcheck

	_, status = testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/scan-sessions/%s/context", teamID, otherSessionID),
		map[string]any{"collection_id": collID}, desktopToken)
	if status != http.StatusNotFound {
		t.Errorf("collect push to another user's session: expected 404, got %d", status)
	}
}
