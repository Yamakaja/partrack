package handler_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestListOrders_Empty(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "ord-list@example.com", "pw", "Ord")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Ord Team")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/orders", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestGetOrder(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "ord-get@example.com", "pw", "Ord")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Ord Team")
	partID := testutil.CreatePart(t, testDB, "global", "ORD-PART-01", nil)
	distID := testutil.GetTestDistributorID(t, testDB)
	listingID := testutil.CreateListing(t, testDB, partID, distID, "ORD-SKU-01")
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	testutil.CreateOrderLineItem(t, testDB, orderID, listingID, 10)
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/orders/%s", teamID, orderID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["id"] != orderID {
		t.Errorf("expected id=%s, got %v", orderID, body["id"])
	}
	if body["status"] != "draft" {
		t.Errorf("expected status=draft, got %v", body["status"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 line item, got %d", len(items))
	}
}

func TestGetOrder_NotFound(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "ord-404@example.com", "pw", "Ord")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Ord Team")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/orders/00000000-0000-0000-0000-000000000000", teamID), nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
}

func TestUpdateOrder_Notes(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "ord-notes@example.com", "pw", "Ord")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Ord Team")
	distID := testutil.GetTestDistributorID(t, testDB)
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/orders/%s", teamID, orderID),
		map[string]any{"notes": "Rush order"}, token)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
}

func TestCancelOrder(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "ord-cancel@example.com", "pw", "Ord")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Ord Team")
	distID := testutil.GetTestDistributorID(t, testDB)
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/orders/%s", teamID, orderID), nil, token)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
}

func TestExportOrder_CSV(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "ord-export@example.com", "pw", "Ord")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Ord Team")
	partID := testutil.CreatePart(t, testDB, "global", "ORD-EXP-01", nil)
	distID := testutil.GetTestDistributorID(t, testDB)
	listingID := testutil.CreateListing(t, testDB, partID, distID, "ORD-EXP-SKU-01")
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	testutil.CreateOrderLineItem(t, testDB, orderID, listingID, 5)
	token := testutil.CreateSession(t, testDB, userID)

	// Make a raw request since response is CSV, not JSON.
	req, _ := http.NewRequest(http.MethodGet,
		testEnv.Server.URL+fmt.Sprintf("/api/v1/teams/%s/orders/%s/export", teamID, orderID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("expected Content-Type text/csv, got %s", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "SKU") {
		t.Errorf("expected CSV header with SKU, got: %s", string(body))
	}
}

func TestSubmitOrder_NotImplemented(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "ord-submit@example.com", "pw", "Ord")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Ord Team")
	distID := testutil.GetTestDistributorID(t, testDB)
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/orders/%s/submit", teamID, orderID), nil, token)
	if status != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", status)
	}
}

func TestLinkOrderReference(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "ord-ref@example.com", "pw", "Ord")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Ord Team")
	distID := testutil.GetTestDistributorID(t, testDB)
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/orders/%s/reference", teamID, orderID),
		map[string]any{"external_reference": "PO-12345"}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["status"] != "placed" {
		t.Errorf("expected status=placed, got %v", body["status"])
	}
	if body["external_reference"] != "PO-12345" {
		t.Errorf("expected external_reference=PO-12345, got %v", body["external_reference"])
	}
}

func TestReceiveOrderItem(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "ord-recv@example.com", "pw", "Ord")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Ord Team")
	partID := testutil.CreatePart(t, testDB, "global", "ORD-RECV-01", nil)
	distID := testutil.GetTestDistributorID(t, testDB)
	listingID := testutil.CreateListing(t, testDB, partID, distID, "ORD-RECV-SKU-01")
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	lineItemID := testutil.CreateOrderLineItem(t, testDB, orderID, listingID, 10)
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/orders/%s/items/%s/receive", teamID, orderID, lineItemID),
		map[string]any{"quantity_received": 10}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["order_status"] != "received" {
		t.Errorf("expected order_status=received, got %v", body["order_status"])
	}
	if body["new_quantity_on_hand"].(float64) != 10 {
		t.Errorf("expected new_quantity_on_hand=10, got %v", body["new_quantity_on_hand"])
	}

	// Verify stock via DB.
	var qty int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_on_hand FROM inventory_entries WHERE id = $1`, entryID,
	).Scan(&qty) //nolint:errcheck
	if qty != 10 {
		t.Errorf("expected DB quantity_on_hand=10, got %d", qty)
	}
}

// TestReceiveOrderItem_AllocatesToLinkedBuild verifies that receiving an order
// item updates quantity_received on linked build allocations and transitions
// a fully-covered build from ordered → ready (US-B7, US-B8).
func TestReceiveOrderItem_AllocatesToLinkedBuild(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "ord-recvbld@example.com", "pw", "Ord")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Ord Team")
	token := testutil.CreateSession(t, testDB, userID)

	partID := testutil.CreatePart(t, testDB, "global", "RECVBLD-PART-01", nil)
	distID := testutil.GetTestDistributorID(t, testDB)
	listingID := testutil.CreateListing(t, testDB, partID, distID, "RECVBLD-SKU-01")
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)

	// Create a placed order with 5 units of the part.
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	lineItemID := testutil.CreateOrderLineItem(t, testDB, orderID, listingID, 5)
	testutil.PlaceOrder(t, testDB, orderID)

	// Create a project/revision as required for a build.
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "RecvBld-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")

	// Insert an 'ordered' build waiting for all 5 units (none reserved or received).
	buildID := testutil.CreateBuild(t, testDB, projectID, revID, teamID, userID, 1, "ordered")
	testutil.CreateBuildPartAllocation(t, testDB, buildID, partID, 5, 0, 0)
	testutil.LinkBuildToOrder(t, testDB, buildID, orderID)

	// Receive 5 units.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/orders/%s/items/%s/receive", teamID, orderID, lineItemID),
		map[string]any{"quantity_received": 5}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["new_quantity_on_hand"].(float64) != 5 {
		t.Errorf("expected new_quantity_on_hand=5, got %v", body["new_quantity_on_hand"])
	}

	// Allocation_breakdown should mention our build.
	breakdown, _ := body["allocation_breakdown"].([]any)
	if len(breakdown) != 1 {
		t.Fatalf("expected 1 allocation_breakdown entry, got %d", len(breakdown))
	}
	entry := breakdown[0].(map[string]any)
	if entry["build_id"] != buildID {
		t.Errorf("expected build_id=%s in breakdown, got %v", buildID, entry["build_id"])
	}
	if entry["allocated"].(float64) != 5 {
		t.Errorf("expected allocated=5, got %v", entry["allocated"])
	}

	// Build should now be in 'ready' status.
	var buildStatus string
	testDB.QueryRow(context.Background(),
		`SELECT status FROM builds WHERE id = $1`, buildID,
	).Scan(&buildStatus) //nolint:errcheck
	if buildStatus != "ready" {
		t.Errorf("expected build status=ready after full receipt, got %s", buildStatus)
	}

	// Allocation quantity_received should be 5.
	var qtyReceived int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_received FROM build_part_allocations WHERE build_id = $1 AND part_id = $2`,
		buildID, partID,
	).Scan(&qtyReceived) //nolint:errcheck
	if qtyReceived != 5 {
		t.Errorf("expected allocation quantity_received=5, got %d", qtyReceived)
	}
	_ = entryID
}
