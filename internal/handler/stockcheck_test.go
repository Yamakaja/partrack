package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestStockCheck_MissingBuildQty(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sc-nobuildqty@example.com", "pw", "SC")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "SC Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "SC-A")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/stock-check", teamID, projectID, revID), nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestStockCheck_RevisionNotFound(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sc-nobom@example.com", "pw", "SC")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "SC Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "SC-B")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/00000000-0000-0000-0000-000000000000/stock-check?build_quantity=1", teamID, projectID), nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
}

func TestStockCheck_OkAndShortfall(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sc-full@example.com", "pw", "SC")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "SC Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Set up project + revision with two items.
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "SC-Full")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	testutil.SetActiveBOM(t, testDB, projectID, revID)

	partA := testutil.CreatePart(t, testDB, "global", "SC-PART-A", nil) // sufficient stock
	partB := testutil.CreatePart(t, testDB, "global", "SC-PART-B", nil) // no stock (shortfall)

	itemA := testutil.CreateBOMLineItem(t, testDB, revID, "R1", 1)
	itemB := testutil.CreateBOMLineItem(t, testDB, revID, "C1", 1)
	testutil.ResolveBOMLineItem(t, testDB, itemA, partA)
	testutil.ResolveBOMLineItem(t, testDB, itemB, partB)

	// Part A: stock=10 (ok for build_qty=1, requires 1).
	entryA := testutil.CreateInventoryEntry(t, testDB, teamID, partA)
	testutil.SetStockQty(t, testDB, entryA, 10)
	// Part B: no inventory entry → shortfall.

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/stock-check?build_quantity=1", teamID, projectID, revID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}

	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// Index by reference_designators.
	statusByRef := make(map[string]string)
	for _, it := range items {
		m := it.(map[string]any)
		statusByRef[m["reference_designators"].(string)] = m["stock_status"].(string)
	}
	if statusByRef["R1"] != "in_stock" {
		t.Errorf("expected R1=in_stock, got %s", statusByRef["R1"])
	}
	if statusByRef["C1"] != "out_of_stock" {
		t.Errorf("expected C1=out_of_stock, got %s", statusByRef["C1"])
	}
}

func TestStockCheck_Unresolved(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sc-unresolved@example.com", "pw", "SC")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "SC Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "SC-Unresolved")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	testutil.SetActiveBOM(t, testDB, projectID, revID)
	testutil.CreateBOMLineItem(t, testDB, revID, "U1", 1) // not resolved

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/stock-check?build_quantity=1", teamID, projectID, revID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	it := items[0].(map[string]any)
	if it["stock_status"] != "out_of_stock" {
		t.Errorf("expected stock_status=out_of_stock, got %v", it["stock_status"])
	}
}

// TestStockCheck_ShowsReservedAndOnOrder verifies that the stock check response
// includes quantity_reserved, quantity_free, and quantity_on_order fields (US-B14).
// quantity_free = on_hand - reserved; quantity_on_order counts placed orders only.
func TestStockCheck_ShowsReservedAndOnOrder(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sc-resord@example.com", "pw", "SC")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "SC Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "SC-ResOrd")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	testutil.SetActiveBOM(t, testDB, projectID, revID)

	partID := testutil.CreatePart(t, testDB, "global", "SC-RESORD-01", nil)
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "R1", 1) // 1 per board
	testutil.ResolveBOMLineItem(t, testDB, itemID, partID)
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)
	testutil.SetStockQty(t, testDB, entryID, 10)

	// Another build in 'ready' status with 3 units reserved from the same stock pool.
	// (ready = all parts covered by stock; this is the realistic state when stock is sufficient)
	buildID := testutil.CreateBuild(t, testDB, projectID, revID, teamID, userID, 3, "ready")
	testutil.CreateBuildPartAllocation(t, testDB, buildID, partID, 3, 3, 0)

	// Create a placed order with 5 units on order for this part.
	distID := testutil.GetTestDistributorID(t, testDB)
	listingID := testutil.CreateListing(t, testDB, partID, distID, "SC-RESORD-SKU")
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	testutil.CreateOrderLineItem(t, testDB, orderID, listingID, 5)
	testutil.PlaceOrder(t, testDB, orderID)

	// Stock check for 1 board (requires 1 unit).
	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/stock-check?build_quantity=1",
			teamID, projectID, revID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}

	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	it := items[0].(map[string]any)

	// on_hand=10, reserved=3 (by the ordered build), free=7, on_order=5.
	if it["quantity_on_hand"].(float64) != 10 {
		t.Errorf("expected quantity_on_hand=10, got %v", it["quantity_on_hand"])
	}
	if it["quantity_reserved"].(float64) != 3 {
		t.Errorf("expected quantity_reserved=3, got %v", it["quantity_reserved"])
	}
	if it["quantity_free"].(float64) != 7 {
		t.Errorf("expected quantity_free=7 (=on_hand-reserved), got %v", it["quantity_free"])
	}
	if it["quantity_on_order"].(float64) != 5 {
		t.Errorf("expected quantity_on_order=5, got %v", it["quantity_on_order"])
	}
	// Free=7 >= required=1 → in_stock.
	if it["stock_status"].(string) != "in_stock" {
		t.Errorf("expected stock_status=in_stock (free=7 >= required=1), got %v", it["stock_status"])
	}
}
