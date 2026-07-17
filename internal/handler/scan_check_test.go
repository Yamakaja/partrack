package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// LCSC barcodes for filter tests. pc (SKU) values have no listings unless a
// test creates one, so resolution falls through to the pm (MPN) path.
func checkBarcode(sku, mpn string) string {
	return fmt.Sprintf(`{pbn:P1,on:G1,pc:%s,pm:%s,qty:5,mc:X,cc:1,pdi:9,hp:0,wc:zh}`, sku, mpn)
}

func TestCheckScan_CollectionSource(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	userID := testutil.CreateUser(t, testDB, "check-list@example.com", "pw", "F")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "F Team")
	scanToken := testutil.CreateScanSession(t, testDB, userID, teamID)

	partA := testutil.CreatePart(t, testDB, "global", "CHECK-A", nil)
	testutil.CreatePart(t, testDB, "global", "CHECK-C", nil) // in catalog, not on the list

	// List with one quantified row.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/scan/collections", map[string]any{"name": "Bench"}, scanToken)
	if status != http.StatusCreated {
		t.Fatalf("create list: %d %v", status, body)
	}
	collID := body["id"].(string)
	desktopToken := testutil.CreateSession(t, testDB, userID)
	_, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s/items", teamID, collID),
		map[string]any{"part_id": partA, "quantity": 350, "note": "half reel"}, desktopToken)
	if status != http.StatusCreated {
		t.Fatalf("add item: %d", status)
	}

	source := map[string]any{"type": "collection", "collection_id": collID}

	// Target set.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/check/target",
		map[string]any{"source": source}, scanToken)
	if status != http.StatusOK {
		t.Fatalf("target: %d %v", status, body)
	}
	if body["ref"].(string) != "Bench" {
		t.Errorf("expected ref Bench, got %v", body["ref"])
	}
	if got := len(body["entries"].([]any)); got != 1 {
		t.Fatalf("expected 1 target entry, got %d", got)
	}

	// Match: on the list, with quantity + note in the entry.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/check",
		map[string]any{"barcode": checkBarcode("C888001", "CHECK-A"), "source": source,
			"progress": map[string]any{"done": 0, "total": 1}}, scanToken)
	if body["verdict"] != "match" {
		t.Fatalf("expected match, got %v", body)
	}
	entry := body["entry"].(map[string]any)
	if q := entry["qty_needed"].(float64); q != 350 {
		t.Errorf("expected qty_needed 350, got %v", q)
	}
	if entry["note"].(string) != "half reel" {
		t.Errorf("expected note in entry, got %v", entry["note"])
	}

	// Miss: resolved part not on the list.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/check",
		map[string]any{"barcode": checkBarcode("C888002", "CHECK-C"), "source": source}, scanToken)
	if body["verdict"] != "miss" {
		t.Errorf("expected miss, got %v", body["verdict"])
	}
	if body["part"] == nil {
		t.Error("miss should still identify the part")
	}

	// Unknown: garbage barcode.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/check",
		map[string]any{"barcode": "garbage", "source": source}, scanToken)
	if body["verdict"] != "unknown" {
		t.Errorf("expected unknown, got %v", body["verdict"])
	}

	// A teammate cannot filter against someone else's private list.
	otherID := testutil.CreateUser(t, testDB, "check-list-other@example.com", "pw", "O")
	testutil.AddMember(t, testDB, teamID, otherID, "member")
	otherScan := testutil.CreateScanSession(t, testDB, otherID, teamID)
	_, status = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/check",
		map[string]any{"barcode": "x", "source": source}, otherScan)
	if status != http.StatusNotFound {
		t.Errorf("foreign list filter: expected 404, got %d", status)
	}
}

func TestCheckScan_BuildSource(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	userID := testutil.CreateUser(t, testDB, "check-build@example.com", "pw", "F")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "F Team")
	scanToken := testutil.CreateScanSession(t, testDB, userID, teamID)

	partA := testutil.CreatePart(t, testDB, "global", "CHECK-BLD-A", nil)
	partSub := testutil.CreatePart(t, testDB, "global", "CHECK-BLD-SUB", nil)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Gizmo")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "r1")
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "R1, R2, R3", 3)
	testutil.ResolveBOMLineItem(t, testDB, itemID, partA)
	if _, err := testDB.Exec(context.Background(),
		`INSERT INTO bom_line_item_substitutes (bom_line_item_id, part_id, priority) VALUES ($1, $2, 0)`,
		itemID, partSub,
	); err != nil {
		t.Fatalf("seed substitute: %v", err)
	}
	buildID := testutil.CreateBuild(t, testDB, projectID, revID, teamID, userID, 5, "draft")

	source := map[string]any{"type": "build", "build_id": buildID}

	// Target: primary (3 per board × 5 = 15) + the approved substitute.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/check/target",
		map[string]any{"source": source}, scanToken)
	if status != http.StatusOK {
		t.Fatalf("target: %d %v", status, body)
	}
	entries := body["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 target entries (primary + substitute), got %d", len(entries))
	}

	// Primary part → match with demand + designators.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/check",
		map[string]any{"barcode": checkBarcode("C888003", "CHECK-BLD-A"), "source": source}, scanToken)
	if body["verdict"] != "match" {
		t.Fatalf("expected match, got %v", body)
	}
	entry := body["entry"].(map[string]any)
	if q := entry["qty_needed"].(float64); q != 15 {
		t.Errorf("expected qty_needed 15 (3 × build qty 5), got %v", q)
	}
	if entry["designators"].(string) != "R1, R2, R3" {
		t.Errorf("expected designators, got %v", entry["designators"])
	}

	// Approved substitute → substitute verdict crediting the primary.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/check",
		map[string]any{"barcode": checkBarcode("C888004", "CHECK-BLD-SUB"), "source": source}, scanToken)
	if body["verdict"] != "substitute" {
		t.Fatalf("expected substitute, got %v", body)
	}
	entry = body["entry"].(map[string]any)
	if entry["substitute_for_part_id"].(string) != partA {
		t.Errorf("expected substitute_for_part_id %s, got %v", partA, entry["substitute_for_part_id"])
	}
}

func TestCheckScan_OrderSource(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	userID := testutil.CreateUser(t, testDB, "check-order@example.com", "pw", "F")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "F Team")
	scanToken := testutil.CreateScanSession(t, testDB, userID, teamID)

	partA := testutil.CreatePart(t, testDB, "global", "CHECK-ORD-A", nil)
	distID := testutil.GetTestDistributorID(t, testDB)
	listingA := testutil.CreateListing(t, testDB, partA, distID, "C777001")
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	testutil.CreateOrderLineItem(t, testDB, orderID, listingA, 500)

	source := map[string]any{"type": "order", "order_ids": []string{orderID}}

	// Listing-level match: the barcode's SKU is the order line's SKU.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/check",
		map[string]any{"barcode": checkBarcode("C777001", "WHATEVER"), "source": source}, scanToken)
	if status != http.StatusOK || body["verdict"] != "match" {
		t.Fatalf("expected listing-level match, got %d %v", status, body)
	}
	entry := body["entry"].(map[string]any)
	if q := entry["qty_needed"].(float64); q != 500 {
		t.Errorf("expected qty_needed 500, got %v", q)
	}
	if entry["order_ref"] == nil || entry["order_ref"].(string) == "" {
		t.Errorf("expected order_ref, got %v", entry["order_ref"])
	}

	// Part-level fallback: unknown SKU but the MPN resolves to an ordered part.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/check",
		map[string]any{"barcode": checkBarcode("C000000", "CHECK-ORD-A"), "source": source}, scanToken)
	if body["verdict"] != "match" {
		t.Errorf("expected part-level match, got %v", body["verdict"])
	}

	// A part that wasn't ordered → miss.
	testutil.CreatePart(t, testDB, "global", "CHECK-ORD-MISS", nil)
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/check",
		map[string]any{"barcode": checkBarcode("C000001", "CHECK-ORD-MISS"), "source": source}, scanToken)
	if body["verdict"] != "miss" {
		t.Errorf("expected miss, got %v", body["verdict"])
	}
}
